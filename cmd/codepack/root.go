package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/evan-william/codepack-lz/internal/version"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "codepack",
		Short: "Pack a repository for LLMs -- readable bundle or lossless envelope",
		Long: `codepack packs a directory into a single file for large-language-model
workflows. Two kinds of output:

  readable  (--format md|xml|txt)  paste-into-chat context with token counts
  envelope  (--format codepack)    lossless, hash-verified transport artifact

Secret scanning is ON by default and fails the pack (exit 3) when potential
credentials are found; use --redact to mask them instead.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err}
	})
	root.AddCommand(newPackCmd(), newUnpackCmd(), newStatsCmd(), newMCPCmd(), newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the tool and envelope-format versions",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "codepack %s (envelope format v%d)\n", version.Version, version.FormatVersion)
		},
	}
}
