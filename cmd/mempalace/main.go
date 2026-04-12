// Package main is the mempalace CLI entrypoint — a Cobra root that
// dispatches to status / init / mine / search / wake-up subcommands.
package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"go-palace/internal/config"
	"go-palace/internal/convominer"
	"go-palace/internal/dialect"
	"go-palace/internal/embed"
	"go-palace/internal/entity"
	"go-palace/internal/hooks"
	"go-palace/internal/instructions"
	"go-palace/internal/layers"
	"go-palace/internal/miner"
	"go-palace/internal/palace"
	"go-palace/internal/room"
	"go-palace/internal/searcher"
	"go-palace/internal/splitter"
	mcppkg "go-palace/mcp"
	"go-palace/version"
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		// Cobra prints the error itself (SilenceErrors=false would duplicate).
		// We only need a non-zero exit.
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mempalace",
		Short:   "MemPalace — local-first memory palace for AI agents",
		Version: version.Version,
		// SilenceUsage: a non-nil RunE return is NOT a usage error, so do
		// not dump help on every stub invocation.
		SilenceUsage: true,
	}
	cmd.PersistentFlags().String("palace", "", "override palace database path")
	cmd.PersistentFlags().StringVar(&modelFlag, "model", "", "path to local embedding model directory")
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newMineCmd())
	cmd.AddCommand(newSearchCmd())
	cmd.AddCommand(newWakeUpCmd())
	cmd.AddCommand(newSplitCmd())
	cmd.AddCommand(newHookCmd())
	cmd.AddCommand(newInstructionsCmd())
	cmd.AddCommand(newRepairCmd())
	cmd.AddCommand(newCompressCmd())
	cmd.AddCommand(newMCPCmd())
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print runtime status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "mempalace %s\n", version.Version)

			cfg, err := config.Load("")
			if err != nil {
				fmt.Fprintf(w, "  config: %v\n", err)
				return nil
			}
			palacePath := cfg.PalacePath
			if flag := cmd.Flag("palace"); flag != nil && flag.Value.String() != "" {
				palacePath = flag.Value.String()
			}

			emb, cleanup := buildEmbedder()
			defer cleanup()
			p, openErr := palace.Open(palacePath, emb)
			if openErr != nil {
				fmt.Fprintf(w, "  palace: not found at %s\n", palacePath)
				fmt.Fprintf(w, "  hint: mempalace init <dir> && mempalace mine <dir>\n")
				return nil
			}
			defer func() { _ = p.Close() }()

			total, _ := p.Count()
			fmt.Fprintf(w, "  palace: %s\n", palacePath)
			fmt.Fprintf(w, "  drawers: %d\n", total)

			wings := map[string]int{}
			rooms := map[string]int{}
			offset := 0
			for {
				drawers, err := p.Get(palace.GetOptions{Limit: 5000, Offset: offset})
				if err != nil || len(drawers) == 0 {
					break
				}
				for _, d := range drawers {
					if d.Wing != "" {
						wings[d.Wing]++
					}
					if d.Room != "" {
						rooms[d.Room]++
					}
				}
				offset += len(drawers)
				if len(drawers) < 5000 {
					break
				}
			}
			fmt.Fprintf(w, "  wings: %d\n", len(wings))
			fmt.Fprintf(w, "  rooms: %d\n", len(rooms))
			return nil
		},
	}
}

// newInitCmd wires the init verb: detect rooms from folder structure and
// write mempalace.yaml. The interactive approval flow is a minimal
// press-enter shim; `--yes` skips it entirely.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Initialize mempalace.yaml for a project",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("init: abs %s: %w", dir, err)
			}
			rooms, err := room.Detect(absDir)
			if err != nil {
				return fmt.Errorf("init: detect: %w", err)
			}
			wing := projectNameFromDir(absDir)
			yes, _ := cmd.Flags().GetBool("yes")

			bar := strings.Repeat("=", 55)
			rule := strings.Repeat("─", 55)
			fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n  MemPalace Init — Local setup\n%s\n", bar, bar)
			fmt.Fprintf(cmd.OutOrStdout(), "\n  WING: %s\n\n", wing)
			for _, r := range rooms {
				fmt.Fprintf(cmd.OutOrStdout(), "    ROOM: %s\n          %s\n", r.Name, r.Description)
			}
			// Entity detection
			files, scanErr := entity.ScanForDetection(absDir, 10)
			var confirmedPeople, confirmedProjects []string
			if scanErr == nil && len(files) > 0 {
				detected := entity.Detect(files, 10)
				if len(detected.People) > 0 || len(detected.Projects) > 0 || len(detected.Uncertain) > 0 {
					confirmedPeople, confirmedProjects = entity.Confirm(detected, yes, cmd.OutOrStdout(), cmd.InOrStdin())
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", rule)

			if !yes {
				fmt.Fprintf(cmd.OutOrStdout(), "\n  Press enter to accept: ")
				reader := bufio.NewReader(cmd.InOrStdin())
				_, _ = reader.ReadString('\n')
			}
			if err := room.SaveConfig(absDir, wing, rooms); err != nil {
				return fmt.Errorf("init: save: %w", err)
			}
			if len(confirmedPeople) > 0 || len(confirmedProjects) > 0 {
				if err := entity.SaveEntities(absDir, confirmedPeople, confirmedProjects); err != nil {
					return fmt.Errorf("init: save entities: %w", err)
				}
			} else {
				// Write empty entities.json so downstream tools know detection ran
				if err := entity.SaveEntities(absDir, nil, nil); err != nil {
					return fmt.Errorf("init: save entities: %w", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n  Config saved: %s\n",
				filepath.Join(absDir, "mempalace.yaml"))
			fmt.Fprintf(cmd.OutOrStdout(), "\n  Next step:\n    mempalace mine %s\n\n", dir)
			return nil
		},
	}
	cmd.Flags().Bool("yes", false, "auto-accept detected rooms")
	return cmd
}

// newMineCmd wires the real mine verb.
func newMineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mine [path]",
		Short: "Mine a project directory into the palace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			cfg, err := config.Load("")
			if err != nil {
				return fmt.Errorf("mine: config load: %w", err)
			}
			wing, _ := cmd.Flags().GetString("wing")
			agent, _ := cmd.Flags().GetString("agent")
			limit, _ := cmd.Flags().GetInt("limit")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			noGitignore, _ := cmd.Flags().GetBool("no-gitignore")
			includeIgnored, _ := cmd.Flags().GetStringSlice("include-ignored")
			mode, _ := cmd.Flags().GetString("mode")
			extractMode, _ := cmd.Flags().GetString("extract")

			palacePath := cfg.PalacePath
			if flag := cmd.Flag("palace"); flag != nil && flag.Value.String() != "" {
				palacePath = flag.Value.String()
			}

			// Dispatch to convominer when --mode=convos.
			if mode == "convos" {
				copts := convominer.ConvoMineOptions{
					ConvoDir:     dir,
					PalacePath:   palacePath,
					WingOverride: wing,
					Agent:        agent,
					Limit:        limit,
					DryRun:       dryRun,
					ExtractMode:  extractMode,
					Stdout:       cmd.OutOrStdout(),
				}
				if dryRun {
					return convominer.MineConvos(copts, nil)
				}
				emb, cleanup := buildEmbedder()
				defer cleanup()
				p, err := palace.Open(palacePath, emb)
				if err != nil {
					return fmt.Errorf("mine: open palace: %w", err)
				}
				defer func() { _ = p.Close() }()
				return convominer.MineConvos(copts, p)
			}

			opts := miner.MineOptions{
				ProjectDir:       dir,
				PalacePath:       palacePath,
				WingOverride:     wing,
				Agent:            agent,
				Limit:            limit,
				DryRun:           dryRun,
				RespectGitignore: !noGitignore,
				IncludeIgnored:   includeIgnored,
				Stdout:           cmd.OutOrStdout(),
			}

			// Dry-run short-circuit: no palace, no embedder. Verified by
			// TestMineDryRunFixture passing --palace /nonexistent.
			if dryRun {
				return miner.Mine(opts, nil)
			}

			emb, cleanup := buildEmbedder()
			defer cleanup()
			p, err := palace.Open(palacePath, emb)
			if err != nil {
				return fmt.Errorf("mine: open palace: %w", err)
			}
			defer func() { _ = p.Close() }()
			return miner.Mine(opts, p)
		},
	}
	cmd.Flags().String("wing", "", "override wing name")
	cmd.Flags().String("room", "", "force routing to a specific room (reserved)")
	cmd.Flags().String("agent", "mempalace", "agent name recorded on drawers")
	cmd.Flags().Int("limit", 0, "process at most N files")
	cmd.Flags().Bool("dry-run", false, "print what would be filed without touching the palace")
	cmd.Flags().Bool("no-gitignore", false, "ignore .gitignore files")
	cmd.Flags().StringSlice("include-ignored", nil, "force-include specific paths even if .gitignored")
	cmd.Flags().String("mode", "projects", "mining mode: projects or convos")
	cmd.Flags().String("extract", "exchange", "extraction mode for convos: exchange or general")
	return cmd
}

// newSearchCmd wires the real search verb.
func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search the palace semantically",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			cfg, err := config.Load("")
			if err != nil {
				return fmt.Errorf("search: config load: %w", err)
			}
			wing, _ := cmd.Flags().GetString("wing")
			room, _ := cmd.Flags().GetString("room")
			limit, _ := cmd.Flags().GetInt("limit")
			if results, _ := cmd.Flags().GetInt("results"); results > 0 {
				limit = results
			}

			palacePath := cfg.PalacePath
			if flag := cmd.Flag("palace"); flag != nil && flag.Value.String() != "" {
				palacePath = flag.Value.String()
			}

			emb, cleanup := buildEmbedder()
			defer cleanup()
			p, err := palace.Open(palacePath, emb)
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "\n  No palace found at %s\n  Run: mempalace init <dir> then mempalace mine <dir>\n", palacePath)
				return fmt.Errorf("search: open palace: %w", err)
			}
			defer func() { _ = p.Close() }()

			opts := searcher.SearchOptions{
				Query:      query,
				Wing:       wing,
				Room:       room,
				NResults:   limit,
				PalacePath: palacePath,
			}
			return searcher.Search(p, opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().String("wing", "", "filter by wing")
	cmd.Flags().String("room", "", "filter by room")
	cmd.Flags().Int("limit", 5, "max results to return")
	cmd.Flags().Int("results", 0, "alias for --limit (max results to return)")
	return cmd
}

// buildEmbedder returns the best available embedder and a cleanup function.
// Callers must defer cleanup() to release resources (e.g. hugot session).
//
//  1. HugotEmbedder (pure-Go, offline after first model download)
//  2. PythonSubprocessEmbedder (legacy, if MEMPALACE_PY_DIR is set)
//  3. FakeEmbedder(384) (deterministic hash vectors — tests / offline)
func buildEmbedder() (embed.Embedder, func()) {
	noop := func() {}

	// Try hugot first (unless explicitly disabled).
	if os.Getenv("MEMPALACE_NO_HUGOT") == "" {
		e, err := embed.NewHugotEmbedder(embed.HugotOptions{
			ModelPath: modelFlag,
		})
		if err != nil {
			slog.Warn("hugot embedder unavailable, trying fallbacks", "err", err)
		} else {
			return e, func() { _ = e.Close() }
		}
	}

	// Legacy Python subprocess path.
	if pyDir := os.Getenv("MEMPALACE_PY_DIR"); pyDir != "" {
		e, err := embed.NewPythonSubprocessEmbedder(embed.PythonSubprocessOptions{
			MempalaceDir: pyDir,
		})
		if err != nil {
			slog.Warn("python embedder unavailable, using FakeEmbedder", "err", err)
		} else {
			return e, func() { _ = e.Close() }
		}
	}

	return embed.NewFakeEmbedder(palace.DefaultEmbeddingDim), noop
}

// modelFlag holds the --model flag value for custom model path.
var modelFlag string

func newWakeUpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "wake-up",
		Aliases: []string{"wakeup"},
		Short:   "Show L0+L1 memory context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load("")
			if err != nil {
				return fmt.Errorf("wake-up: config load: %w", err)
			}
			wing, _ := cmd.Flags().GetString("wing")

			palacePath := cfg.PalacePath
			if flag := cmd.Flag("palace"); flag != nil && flag.Value.String() != "" {
				palacePath = flag.Value.String()
			}

			emb, cleanup := buildEmbedder()
			defer cleanup()
			p, err := palace.Open(palacePath, emb)
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "\n  No palace found at %s\n  Run: mempalace init <dir> then mempalace mine <dir>\n", palacePath)
				return fmt.Errorf("wake-up: open palace: %w", err)
			}
			defer func() { _ = p.Close() }()

			stack := layers.NewStack(p, "")
			var text string
			if wing != "" {
				text = stack.WakeUpWing(wing)
			} else {
				text = stack.WakeUp()
			}

			w := cmd.OutOrStdout()
			tokens := layers.TokenEstimate(text)
			fmt.Fprintf(w, "Wake-up text (~%d tokens):\n%s\n%s\n", tokens, strings.Repeat("=", 50), text)
			return nil
		},
	}
	cmd.Flags().String("wing", "", "filter L1 by wing")
	return cmd
}

func newSplitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "split [dir]",
		Short: "Split concatenated mega-files into per-session files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			outputDir, _ := cmd.Flags().GetString("output-dir")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			minSessions, _ := cmd.Flags().GetInt("min-sessions")

			results, err := splitter.Split(dir, splitter.SplitOptions{
				OutputDir:   outputDir,
				DryRun:      dryRun,
				MinSessions: minSessions,
			})
			if err != nil {
				return fmt.Errorf("split: %w", err)
			}
			w := cmd.OutOrStdout()
			if len(results) == 0 {
				fmt.Fprintf(w, "No mega-files found (min %d sessions).\n", minSessions)
				return nil
			}
			bar := strings.Repeat("=", 60)
			mode := "SPLITTING"
			if dryRun {
				mode = "DRY RUN"
			}
			fmt.Fprintf(w, "\n%s\n  Mega-file splitter — %s\n%s\n\n", bar, mode, bar)
			total := 0
			for _, r := range results {
				fmt.Fprintf(w, "  %s  (%d sessions)\n", filepath.Base(r.SourceFile), r.SessionsFound)
				total += r.FilesWritten
			}
			fmt.Fprintf(w, "\n%s\n", strings.Repeat("\u2500", 60))
			if dryRun {
				fmt.Fprintf(w, "  DRY RUN — would create %d files from %d mega-files\n", total, len(results))
			} else {
				fmt.Fprintf(w, "  Done — created %d files from %d mega-files\n", total, len(results))
			}
			return nil
		},
	}
	cmd.Flags().String("output-dir", "", "output directory (default: same as source)")
	cmd.Flags().Bool("dry-run", false, "show what would happen without writing files")
	cmd.Flags().Int("min-sessions", 2, "only split files with at least N sessions")
	return cmd
}

func newHookCmd() *cobra.Command {
	hookCmd := &cobra.Command{
		Use:   "hook",
		Short: "Run harness hook logic",
	}
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a hook handler",
		RunE: func(cmd *cobra.Command, _ []string) error {
			hookName, _ := cmd.Flags().GetString("hook")
			harness, _ := cmd.Flags().GetString("harness")
			if hookName == "" {
				return fmt.Errorf("hook run: --hook is required")
			}
			return hooks.RunHook(hookName, harness, os.Stdin, os.Stdout)
		},
	}
	runCmd.Flags().String("hook", "", "hook name: session-start, stop, precompact")
	runCmd.Flags().String("harness", "claude-code", "harness type: claude-code, codex")
	hookCmd.AddCommand(runCmd)
	return hookCmd
}

func newInstructionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "instructions [name]",
		Short: "Output skill instructions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := instructions.Get(args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), text)
			return nil
		},
	}
}

func newRepairCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "Rebuild palace vector index",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			cfg, err := config.Load("")
			if err != nil {
				return fmt.Errorf("repair: config load: %w", err)
			}
			palacePath := cfg.PalacePath
			if flag := cmd.Flag("palace"); flag != nil && flag.Value.String() != "" {
				palacePath = flag.Value.String()
			}

			emb, cleanup := buildEmbedder()
			defer cleanup()
			p, err := palace.Open(palacePath, emb)
			if err != nil {
				fmt.Fprintf(w, "\n  No palace found at %s\n", palacePath)
				return nil
			}
			defer func() { _ = p.Close() }()

			total, _ := p.Count()
			fmt.Fprintf(w, "\n%s\n  MemPalace Repair\n%s\n", strings.Repeat("=", 55), strings.Repeat("=", 55))
			fmt.Fprintf(w, "  Palace: %s\n", palacePath)
			fmt.Fprintf(w, "  Drawers: %d\n", total)

			if total == 0 {
				fmt.Fprintf(w, "  Nothing to repair.\n")
				return nil
			}

			// Re-embed all drawers by reading and upserting them in batches.
			fmt.Fprintf(w, "\n  Re-embedding all drawers...\n")
			offset := 0
			repaired := 0
			for {
				drawers, err := p.Get(palace.GetOptions{Limit: 100, Offset: offset})
				if err != nil || len(drawers) == 0 {
					break
				}
				if err := p.UpsertBatch(drawers); err != nil {
					fmt.Fprintf(w, "  Error at offset %d: %v\n", offset, err)
					break
				}
				repaired += len(drawers)
				offset += len(drawers)
				if len(drawers) < 100 {
					break
				}
			}
			fmt.Fprintf(w, "  Repaired %d drawers.\n\n", repaired)
			return nil
		},
	}
}

func newCompressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compress",
		Short: "Compress palace storage using AAAK dialect",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			cfg, err := config.Load("")
			if err != nil {
				return fmt.Errorf("compress: config load: %w", err)
			}
			palacePath := cfg.PalacePath
			if flag := cmd.Flag("palace"); flag != nil && flag.Value.String() != "" {
				palacePath = flag.Value.String()
			}
			wingFilter, _ := cmd.Flags().GetString("wing")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			emb, cleanup := buildEmbedder()
			defer cleanup()
			p, err := palace.Open(palacePath, emb)
			if err != nil {
				fmt.Fprintf(w, "\n  No palace found at %s\n", palacePath)
				return nil
			}
			defer func() { _ = p.Close() }()

			showStats, _ := cmd.Flags().GetBool("stats")
			d := dialect.New(nil, nil)

			where := map[string]string{}
			if wingFilter != "" {
				where["wing"] = wingFilter
			}

			offset := 0
			compressedCount := 0
			totalOrigTokens := 0
			totalCompTokens := 0
			var batch []palace.Drawer
			for {
				drawers, err := p.Get(palace.GetOptions{Where: where, Limit: 100, Offset: offset})
				if err != nil || len(drawers) == 0 {
					break
				}
				for _, dr := range drawers {
					meta := map[string]string{
						"wing":        dr.Wing,
						"room":        dr.Room,
						"source_file": dr.SourceFile,
					}
					result := d.Compress(dr.Document, meta)
					stats := d.CompressionStats(dr.Document, result)
					totalOrigTokens += stats.OriginalTokensEst
					totalCompTokens += stats.SummaryTokensEst

					if dryRun || showStats {
						fmt.Fprintf(w, "  %s: %d → %d bytes (~%d → ~%d tokens)\n",
							dr.ID, len(dr.Document), len(result), stats.OriginalTokensEst, stats.SummaryTokensEst)
					}
					if !dryRun {
						dr.Document = result
						batch = append(batch, dr)
					}
					compressedCount++
				}
				if !dryRun && len(batch) > 0 {
					if err := p.UpsertBatch(batch); err != nil {
						return fmt.Errorf("compress: upsert: %w", err)
					}
					batch = batch[:0]
				}
				offset += len(drawers)
				if len(drawers) < 100 {
					break
				}
			}
			if dryRun {
				fmt.Fprintf(w, "\n  DRY RUN — would compress %d drawers (~%d → ~%d tokens)\n",
					compressedCount, totalOrigTokens, totalCompTokens)
			} else {
				fmt.Fprintf(w, "  Compressed %d drawers (~%d → ~%d tokens)\n",
					compressedCount, totalOrigTokens, totalCompTokens)
			}
			return nil
		},
	}
	cmd.Flags().String("wing", "", "filter by wing")
	cmd.Flags().Bool("dry-run", false, "show what would happen without changing drawers")
	cmd.Flags().Bool("stats", false, "show per-drawer compression ratios")
	return cmd
}

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server for AI agents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serve, _ := cmd.Flags().GetBool("serve")
			if !serve {
				fmt.Fprintf(cmd.OutOrStdout(), "To start the MCP server:\n")
				fmt.Fprintf(cmd.OutOrStdout(), "  mempalace mcp --serve\n\n")
				fmt.Fprintf(cmd.OutOrStdout(), "To register with Claude Code:\n")
				fmt.Fprintf(cmd.OutOrStdout(), "  claude mcp add mempalace -- mempalace mcp --serve\n")
				return nil
			}

			cfg, err := config.Load("")
			if err != nil {
				return fmt.Errorf("mcp: config load: %w", err)
			}
			palacePath := cfg.PalacePath
			if flag := cmd.Flag("palace"); flag != nil && flag.Value.String() != "" {
				palacePath = flag.Value.String()
			}

			emb, cleanup := buildEmbedder()
			defer cleanup()
			p, err := palace.Open(palacePath, emb)
			if err != nil {
				return fmt.Errorf("mcp: open palace: %w", err)
			}
			defer func() { _ = p.Close() }()

			// Open KG alongside palace.
			kgPath := palacePath + "_kg.sqlite3"
			kgDB, kgErr := mcppkg.OpenKGForMCP(kgPath)
			if kgErr != nil {
				slog.Warn("mcp: kg unavailable", "error", kgErr)
			}
			if kgDB != nil {
				defer func() { _ = kgDB.Close() }()
			}

			srv := mcppkg.NewServer(palacePath, p, kgDB, cfg)
			return srv.Serve(os.Stdin, os.Stdout)
		},
	}
	cmd.Flags().Bool("serve", false, "launch MCP server on stdin/stdout")
	return cmd
}

// projectNameFromDir mirrors Python's Path(dir).name.lower().replace(" ","_").replace("-","_").
func projectNameFromDir(absDir string) string {
	base := filepath.Base(absDir)
	base = strings.ToLower(base)
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "-", "_")
	return base
}
