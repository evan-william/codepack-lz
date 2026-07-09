package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/evan-william/codepack-lz/internal/format"
	"github.com/evan-william/codepack-lz/internal/pack"
	"github.com/evan-william/codepack-lz/internal/prune"
	"github.com/evan-william/codepack-lz/internal/tokens"
	"github.com/evan-william/codepack-lz/internal/walk"
)

type packFlags struct {
	format          string
	output          string
	codec           string
	include         []string
	exclude         []string
	noDefaultIgnore bool
	maxFileSize     string
	stripComments   bool
	compress        bool
	countTokens     string
	tokenModel      string
	tokenEndpoint   string
	noSecretScan    bool
	redact          bool
	copyOut         bool
	splitOutput     bool
	splitSize       string
	encrypt         bool
	keyEnv          string
	autoOutput      bool
}

const defaultOutputDir = "repack-result"

func newPackCmd() *cobra.Command {
	f := &packFlags{}
	cmd := &cobra.Command{
		Use:   "pack [path]",
		Short: "Pack a directory into a single output file",
		Long: `Pack walks a directory, filters noise, scans for secrets, and renders one
output file. Readable formats (md, xml, txt) are for pasting into an LLM;
the codepack envelope is a lossless, hash-verified transport artifact.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			return runPack(cmd, root, f)
		},
	}

	fl := cmd.Flags()
	fl.StringVar(&f.format, "format", format.KindMarkdown, "output format: "+strings.Join(format.Kinds(), "|"))
	fl.StringVarP(&f.output, "output", "o", "", "output file; empty = ./repack-result/<repo>-<timestamp>.<ext>, \"-\" = stdout")
	fl.StringVar(&f.codec, "codec", format.CodecGzip, "envelope codec: gzip|zstd")
	fl.StringArrayVar(&f.include, "include", nil, "glob(s); when set, only matching files are packed (repeatable)")
	fl.StringArrayVar(&f.exclude, "exclude", nil, "extra glob(s) to exclude (repeatable)")
	fl.BoolVar(&f.noDefaultIgnore, "no-default-ignore", false, "disable the built-in ignore list")
	fl.StringVar(&f.maxFileSize, "max-file-size", "1MiB", "skip files larger than this (e.g. 512KiB, 2MB, 1048576)")
	fl.BoolVar(&f.stripComments, "strip-comments", false, "remove comments (readable formats only)")
	fl.BoolVar(&f.compress, "compress", false, "structurally compress source files (readable formats only)")
	fl.StringVar(&f.countTokens, "count-tokens", "est", "token counting: est|off|api")
	fl.StringVar(&f.tokenModel, "token-model", "", "Anthropic model for --count-tokens=api (default: ANTHROPIC_MODEL or Claude Sonnet default)")
	fl.StringVar(&f.tokenEndpoint, "token-endpoint", "", "Anthropic count-tokens endpoint override")
	fl.BoolVar(&f.noSecretScan, "no-secret-scan", false, "disable secret scanning (dangerous)")
	fl.BoolVar(&f.redact, "redact", false, "replace detected secrets with [REDACTED:<rule>] instead of failing")
	fl.BoolVar(&f.copyOut, "copy", false, "also copy the output to the clipboard (best-effort)")
	fl.BoolVar(&f.splitOutput, "split-output", false, "write readable output as numbered part files")
	fl.StringVar(&f.splitSize, "split-size", "1MiB", "maximum bytes per split output part")
	fl.BoolVar(&f.encrypt, "encrypt", false, "encrypt a codepack envelope with AES-256-GCM")
	fl.StringVar(&f.keyEnv, "key-env", "CODEPACK_KEY_HEX", "environment variable holding a 32-byte hex key for encryption/decryption")
	_ = fl.MarkHidden("token-endpoint")
	return cmd
}

func runPack(cmd *cobra.Command, root string, f *packFlags) error {
	start := time.Now()
	if err := resolveDefaultOutput(root, f, start); err != nil {
		return err
	}
	opts, err := buildOptions(f)
	if err != nil {
		return err
	}
	if err := addSplitSelfExcludes(root, opts, f); err != nil {
		return err
	}
	addAutoOutputSelfExclude(opts, f)

	p, err := pack.Build(root, *opts)
	if err != nil {
		return err
	}

	renderer, err := newRenderer(f)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := renderer.Render(&buf, p); err != nil {
		return fmt.Errorf("render %s: %w", f.format, err)
	}

	dest := "stdout"
	if f.output == "-" {
		if _, err := cmd.OutOrStdout().Write(buf.Bytes()); err != nil {
			return err
		}
	} else if f.splitOutput {
		parts, err := writeSplitOutput(f.output, buf.Bytes(), f.splitSize)
		if err != nil {
			return err
		}
		dest = fmt.Sprintf("%s (%d part(s), %s max)", splitPattern(f.output), len(parts), f.splitSize)
	} else {
		if err := os.MkdirAll(filepath.Dir(f.output), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.output, buf.Bytes(), 0o644); err != nil {
			return err
		}
		dest = f.output
	}

	if f.copyOut {
		if err := copyToClipboard(buf.Bytes()); err != nil {
			warnf(cmd.ErrOrStderr(), "warning: clipboard copy failed: %v\n", err)
		} else {
			warnf(cmd.ErrOrStderr(), "copied %d bytes to clipboard\n", buf.Len())
		}
	}

	// Human summary goes to stderr; stdout carries only machine output.
	w := cmd.ErrOrStderr()
	warnf(w, "packed %d files (%d skipped) from %s into %s [%s, %d bytes] in %s\n",
		len(p.Files), len(p.Skips), p.Root, dest, f.format, buf.Len(), time.Since(start).Round(time.Millisecond))
	if p.TotalTokens >= 0 {
		if p.TokenEstimate {
			warnf(w, "tokens: ~%d (est., %s) - estimate, not exact\n", p.TotalTokens, p.Tokenizer)
		} else {
			warnf(w, "tokens: %d (exact, %s)\n", p.TotalTokens, p.Tokenizer)
		}
	}
	if len(p.Redactions) > 0 {
		warnf(w, "redacted secrets in %d file(s); details are recorded in the output manifest\n", len(p.Redactions))
	}
	if f.format == format.KindEnvelope {
		if f.encrypt {
			warnf(w, "note: %s\n", format.EncryptedWarning)
		} else {
			warnf(w, "note: %s\n", format.SecurityWarning)
		}
	}
	return nil
}

func warnf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func resolveDefaultOutput(root string, f *packFlags, now time.Time) error {
	if f.output != "" {
		return nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", root, err)
	}
	rootName := safeFilename(filepath.Base(absRoot))
	if rootName == "" {
		rootName = "codepack"
	}
	name := fmt.Sprintf("%s-%s%s", rootName, now.Format("20060102-150405"), outputExtension(f.format))
	f.output = filepath.Join(absRoot, defaultOutputDir, name)
	f.autoOutput = true
	return nil
}

func outputExtension(kind string) string {
	if kind == format.KindEnvelope {
		return ".codepack.txt"
	}
	return "." + kind
}

func safeFilename(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), ".-")
}

// buildOptions validates flag combinations and assembles pack.Options.
func buildOptions(f *packFlags) (*pack.Options, error) {
	if !slices.Contains(format.Kinds(), f.format) {
		return nil, usageError{fmt.Errorf("invalid --format %q (valid: %s)", f.format, strings.Join(format.Kinds(), ", "))}
	}
	switch f.codec {
	case format.CodecGzip, format.CodecZstd:
	default:
		return nil, usageError{fmt.Errorf("invalid --codec %q (valid: gzip, zstd)", f.codec)}
	}
	if f.format != format.KindEnvelope && f.codec != format.CodecGzip {
		return nil, usageError{fmt.Errorf("--codec only applies to --format codepack")}
	}
	if f.stripComments && f.format == format.KindEnvelope {
		return nil, usageError{fmt.Errorf("--strip-comments cannot be used with --format codepack: the envelope guarantees byte-identical restore and never transforms content (redaction is the only exception, and it is recorded in the manifest)")}
	}
	if f.compress && f.format == format.KindEnvelope {
		return nil, usageError{fmt.Errorf("--compress cannot be used with --format codepack: the envelope guarantees byte-identical restore and never transforms content")}
	}
	if f.redact && f.noSecretScan {
		return nil, usageError{fmt.Errorf("--redact and --no-secret-scan contradict each other")}
	}
	if f.splitOutput {
		if f.output == "-" {
			return nil, usageError{fmt.Errorf("--split-output requires --output")}
		}
		if f.format == format.KindEnvelope {
			return nil, usageError{fmt.Errorf("--split-output is for readable formats; split envelopes would not unpack as a single artifact")}
		}
		if _, err := parseSize(f.splitSize); err != nil {
			return nil, usageError{fmt.Errorf("invalid --split-size: %w", err)}
		}
	}
	if f.encrypt && f.format != format.KindEnvelope {
		return nil, usageError{fmt.Errorf("--encrypt only applies to --format codepack")}
	}

	maxSize, err := parseSize(f.maxFileSize)
	if err != nil {
		return nil, usageError{fmt.Errorf("invalid --max-file-size: %w", err)}
	}

	var counter tokens.Counter
	switch f.countTokens {
	case "off":
	case "api":
		if f.format != format.KindEnvelope {
			model := f.tokenModel
			if model == "" {
				model = os.Getenv("ANTHROPIC_MODEL")
			}
			endpoint := f.tokenEndpoint
			if endpoint == "" {
				endpoint = os.Getenv("ANTHROPIC_COUNT_TOKENS_URL")
			}
			counter, err = tokens.NewAnthropicCounter(os.Getenv("ANTHROPIC_API_KEY"), model, endpoint)
			if err != nil {
				return nil, usageError{err}
			}
		}
	case "est":
		// Token counts only appear in readable output; skip the cost otherwise.
		if f.format != format.KindEnvelope {
			counter, err = tokens.NewEstimator()
			if err != nil {
				return nil, err
			}
		}
	default:
		return nil, usageError{fmt.Errorf("invalid --count-tokens %q (valid: est, off, api)", f.countTokens)}
	}

	outputAbs := ""
	if f.output != "-" {
		if outputAbs, err = filepath.Abs(f.output); err != nil {
			return nil, fmt.Errorf("resolve output path: %w", err)
		}
	}

	return &pack.Options{
		Walk: walk.Options{
			Include:         f.include,
			Exclude:         f.exclude,
			NoDefaultIgnore: f.noDefaultIgnore,
			MaxFileSize:     maxSize,
			OutputPath:      outputAbs,
		},
		StripComments: f.stripComments,
		SecretScan:    !f.noSecretScan,
		Redact:        f.redact,
		Counter:       counter,
		Pruner:        prunerForFlags(f),
	}, nil
}

func newRenderer(f *packFlags) (format.Renderer, error) {
	if f.format == format.KindEnvelope {
		if f.encrypt {
			keyHex := os.Getenv(f.keyEnv)
			key, err := format.DecodeAES256KeyHex(keyHex)
			if err != nil {
				return nil, fmt.Errorf("--encrypt requires %s to contain a 64-character hex key: %w", f.keyEnv, err)
			}
			return format.NewEnvelope(f.codec, format.WithEncryptionKey(key))
		}
		return format.NewEnvelope(f.codec)
	}
	return format.New(f.format)
}

func prunerForFlags(f *packFlags) prune.Pruner {
	if !f.compress {
		return nil
	}
	return prune.NewHeuristic()
}

func writeSplitOutput(path string, data []byte, sizeText string) ([]string, error) {
	maxSize, err := parseSize(sizeText)
	if err != nil {
		return nil, usageError{fmt.Errorf("invalid --split-size: %w", err)}
	}
	if maxSize <= 0 {
		return nil, usageError{fmt.Errorf("--split-size must be greater than zero")}
	}
	parts := splitData(data, int(maxSize))
	names := make([]string, 0, len(parts))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	for i, part := range parts {
		name := splitName(path, i+1)
		if err := os.WriteFile(name, part, 0o644); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

func splitData(data []byte, max int) [][]byte {
	if len(data) == 0 {
		return [][]byte{{}}
	}
	parts := make([][]byte, 0, (len(data)/max)+1)
	for len(data) > max {
		end := max
		if idx := bytes.LastIndexByte(data[:max], '\n'); idx > 0 {
			end = idx + 1
		}
		parts = append(parts, data[:end])
		data = data[end:]
	}
	parts = append(parts, data)
	return parts
}

func splitPattern(path string) string {
	return splitName(path, 1) + " ..."
}

func splitName(path string, part int) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return fmt.Sprintf("%s.part%03d%s", base, part, ext)
}

func addSplitSelfExcludes(root string, opts *pack.Options, f *packFlags) error {
	if !f.splitOutput || f.output == "-" {
		return nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", root, err)
	}
	absOut, err := filepath.Abs(f.output)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	rel, err := filepath.Rel(absRoot, absOut)
	rel = filepath.ToSlash(rel)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil
	}
	ext := filepath.Ext(rel)
	base := strings.TrimSuffix(rel, ext)
	opts.Walk.Exclude = append(opts.Walk.Exclude, base+".part*"+ext)
	return nil
}

func addAutoOutputSelfExclude(opts *pack.Options, f *packFlags) {
	if f.autoOutput {
		opts.Walk.Exclude = append(opts.Walk.Exclude, defaultOutputDir+"/")
	}
}
