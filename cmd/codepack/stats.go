package main

import (
	"github.com/spf13/cobra"

	"github.com/evan-william/codepack-lz/internal/stats"
)

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats <file>",
		Short: "Inspect a codepack output file without decoding its payload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return stats.Print(cmd.OutOrStdout(), args[0])
		},
	}
}
