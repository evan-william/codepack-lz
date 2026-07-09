package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/evan-william/codepack-lz/internal/format"
	"github.com/evan-william/codepack-lz/internal/unpack"
)

func newUnpackCmd() *cobra.Command {
	var outDir string
	var dryRun bool
	var keyEnv string

	cmd := &cobra.Command{
		Use:   "unpack <file.codepack.txt>",
		Short: "Restore a codepack envelope and verify every file hash",
		Long: `Unpack decodes a codepack envelope, restores the directory tree, and
verifies each file against its stored SHA-256. It never overwrites existing
files. Without --out, the tree is restored into ./<root-name-from-pack>.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() { _ = in.Close() }()

			h, err := unpack.ReadHeader(bufio.NewReader(in))
			if err != nil {
				return err
			}

			target := outDir
			if target == "" {
				target = h.Root
				if target == "" {
					return fmt.Errorf("pack has no root name; pass --out")
				}
			}
			if _, err := in.Seek(0, 0); err != nil {
				return err
			}

			var key []byte
			if h.Encryption == format.EncryptionAES256GCM {
				key, err = format.DecodeAES256KeyHex(os.Getenv(keyEnv))
				if err != nil {
					return fmt.Errorf("encrypted envelope requires %s to contain a 64-character hex key: %w", keyEnv, err)
				}
			}

			sum, err := unpack.RestoreWithOptions(in, target, unpack.RestoreOptions{DryRun: dryRun, EncryptionKey: key})
			if err != nil {
				return err
			}

			w := cmd.ErrOrStderr()
			abs, _ := filepath.Abs(target)
			if dryRun {
				warnf(w, "dry run: %d files (%d bytes) verified ok; nothing written (target would be %s)\n", sum.Files, sum.Bytes, abs)
				return nil
			}
			warnf(w, "unpacked %d files (%d bytes) into %s; all %d hashes verified\n", sum.Files, sum.Bytes, abs, sum.Verified)
			return nil
		},
	}

	cmd.Flags().StringVar(&outDir, "out", "", "output directory (default: ./<pack root name>)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "decode and verify without writing anything")
	cmd.Flags().StringVar(&keyEnv, "key-env", "CODEPACK_KEY_HEX", "environment variable holding a 32-byte hex key for encrypted envelopes")
	return cmd
}
