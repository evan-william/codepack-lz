// Package stats inspects codepack output files. For envelopes it prints the
// plaintext header WITHOUT decoding the payload; for readable formats it
// reports what it can sniff cheaply.
package stats

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/evan-william/codepack-lz/internal/unpack"
)

// Print writes a human summary of the file at path to w.
func Print(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	br := bufio.NewReader(f)
	h, err := unpack.ReadHeader(br)
	switch {
	case err == nil:
		printStats(w, "format:       codepack envelope (v%d)\n", h.FormatVersion)
		printStats(w, "tool:         codepack-lz v%s\n", h.ToolVersion)
		printStats(w, "created:      %s\n", h.Created)
		printStats(w, "root:         %s\n", h.Root)
		printStats(w, "files:        %d (%d skipped)\n", h.Files, h.Skipped)
		printStats(w, "bytes raw:    %d\n", h.BytesRaw)
		printStats(w, "bytes packed: %d (%s, before base64)\n", h.BytesPacked, h.Codec)
		printStats(w, "encryption:   %s\n", h.Encryption)
		if h.BytesRaw > 0 {
			printStats(w, "ratio:        %.1f%%\n", float64(h.BytesPacked)/float64(h.BytesRaw)*100)
		}
		printStats(w, "secret scan:  %s\n", h.SecretScan)
		printStats(w, "warning:      %s\n", h.Warning)
		printStats(w, "\nheader read without decoding the payload; run `codepack unpack --dry-run %s` to verify integrity\n", path)
		return nil
	case errors.Is(err, unpack.ErrLegacyFormat):
		return err
	}

	// Not an envelope: sniff the readable formats.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	head := make([]byte, 256)
	n, _ := io.ReadFull(f, head)
	kind := sniffKind(string(head[:n]))
	printStats(w, "format: %s\n", kind)
	printStats(w, "size:   %d bytes\n", info.Size())
	if kind == "unknown" {
		printStats(w, "note:   not a codepack envelope and not a recognized readable output\n")
	}
	return nil
}

func printStats(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func sniffKind(head string) string {
	switch {
	case strings.HasPrefix(head, "# Repository: "):
		return "codepack markdown output"
	case strings.HasPrefix(head, "<repository "):
		return "codepack xml output"
	case strings.HasPrefix(head, "CodePack-LZ v"):
		return "codepack plain-text output"
	default:
		return "unknown"
	}
}
