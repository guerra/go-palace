// Package main is the mempalace CLI entrypoint — a Cobra root that
// dispatches to status / init / mine / search subcommands. Phase B
// replaces the mine stub with a real miner call and adds init; search
// stays on the Phase A sentinel until Phase C.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"go-palace/internal/config"
	"go-palace/internal/convominer"
	"go-palace/internal/embed"
	"go-palace/internal/miner"
	"go-palace/internal/palace"
	"go-palace/internal/room"
	"go-palace/version"
)

// ErrNotImplementedPhaseA is returned by stub subcommands that only prove
// the CLI wiring works. Phase B replaces mine+init with real logic;
// search still returns this sentinel until Phase C.
var ErrNotImplementedPhaseA = errors.New("not implemented in Phase A: foundation only")

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
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newMineCmd())
	cmd.AddCommand(newSearchCmd())
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print runtime status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "mempalace %s — ok\n", version.Version)
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
			fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", rule)

			if !yes {
				fmt.Fprintf(cmd.OutOrStdout(), "\n  Press enter to accept: ")
				reader := bufio.NewReader(cmd.InOrStdin())
				_, _ = reader.ReadString('\n')
			}
			if err := room.SaveConfig(absDir, wing, rooms); err != nil {
				return fmt.Errorf("init: save: %w", err)
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
				emb := buildEmbedder()
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

			emb := buildEmbedder()
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

// newSearchCmd wires the search verb. Same stub pattern as Phase A —
// TODO(phase-c): replace with real searcher.Search.
func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Search the palace semantically (Phase A: stub)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return phaseAStub(cmd, "search")
		},
	}
}

// buildEmbedder returns the real Python-subprocess embedder when
// MEMPALACE_PY_DIR is set, falling back to FakeEmbedder(384) otherwise.
// The fallback path keeps `make audit` hermetic even without Python on
// the box — Phase B explicitly supports it.
func buildEmbedder() embed.Embedder {
	if os.Getenv("MEMPALACE_PY_DIR") == "" {
		return embed.NewFakeEmbedder(384)
	}
	e, err := embed.NewPythonSubprocessEmbedder(embed.PythonSubprocessOptions{
		MempalaceDir: os.Getenv("MEMPALACE_PY_DIR"),
	})
	if err != nil {
		slog.Warn("python embedder unavailable, using FakeEmbedder", "err", err)
		return embed.NewFakeEmbedder(384)
	}
	return e
}

// phaseAStub loads config, resolves the effective palace path, logs it, then
// returns the sentinel. It does NOT open a palace or construct an embedder.
func phaseAStub(cmd *cobra.Command, verb string) error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("phase-a %s: config load: %w", verb, err)
	}
	palacePath := cfg.PalacePath
	if flag := cmd.Flag("palace"); flag != nil && flag.Value.String() != "" {
		palacePath = flag.Value.String()
	}
	slog.Info("phase-a stub",
		"verb", verb,
		"palace_path", palacePath,
		"collection", cfg.CollectionName,
	)
	return ErrNotImplementedPhaseA
}

// projectNameFromDir mirrors Python's Path(dir).name.lower().replace(" ","_").replace("-","_").
func projectNameFromDir(absDir string) string {
	base := filepath.Base(absDir)
	base = strings.ToLower(base)
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "-", "_")
	return base
}
