package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"go-palace/version"
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mempalace",
		Short:   "MemPalace — local-first memory palace for AI agents",
		Version: version.Version,
	}
	cmd.AddCommand(newStatusCmd())
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print runtime status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "mempalace %s — ok\n", version.Version)
			return nil
		},
	}
}
