package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"go-palace/internal/config"
	"go-palace/version"
)

// ErrNotImplementedPhaseA is returned by stub subcommands that only prove
// the CLI wiring works. Phase B replaces these with real mine/search logic.
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

// newMineCmd wires the mine verb. Phase A ships the stub: it proves config
// loading + palace-path resolution work without triggering any embedder or
// opening a palace (that would download an 80MB ONNX model on first run).
func newMineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mine [path]",
		Short: "Mine a project directory into the palace (Phase A: stub)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return phaseAStub(cmd, "mine")
		},
	}
}

// newSearchCmd wires the search verb. Same stub pattern as mine.
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
