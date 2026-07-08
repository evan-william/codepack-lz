// Package pack orchestrates the pipeline that turns a directory into a
// deterministic model.Pack: walk -> read -> binary check -> secret scan ->
// (redact) -> (strip comments) -> (prune) -> hash -> dedup -> token count.
//
// The SHA-256 stored for each file is always the hash of the content that
// ends up in the pack, so unpack can verify what it restores (see
// model.File). Concurrency never affects output: files are processed in a
// bounded worker pool, then assembled in sorted order.
package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/evan-william/codepack-lz/internal/model"
	"github.com/evan-william/codepack-lz/internal/prune"
	"github.com/evan-william/codepack-lz/internal/secret"
	"github.com/evan-william/codepack-lz/internal/strip"
	"github.com/evan-william/codepack-lz/internal/tokens"
	"github.com/evan-william/codepack-lz/internal/walk"
)

// Options configures a build. The caller (cmd) validates flag combinations;
// Build trusts them.
type Options struct {
	Walk          walk.Options
	StripComments bool           // readable formats only; cmd rejects it for the envelope
	SecretScan    bool           // on by default
	Redact        bool           // replace findings instead of failing
	Counter       tokens.Counter // nil = token counting off
	Pruner        prune.Pruner   // nil = no structural compression
}

// SecretError aborts a build when secrets are found and redaction is off.
// The CLI maps it to exit code 3 so CI can gate on it.
type SecretError struct {
	Findings []secret.Finding
}

func (e *SecretError) Error() string {
	return fmt.Sprintf("%d potential secret(s) detected; rerun with --redact to mask them, add a %q comment to intended lines, or --no-secret-scan to skip scanning", len(e.Findings), secret.AllowMarker)
}

// Build runs the full pipeline over root.
func Build(root string, opts Options) (*model.Pack, error) {
	res, err := walk.Collect(root, opts.Walk)
	if err != nil {
		return nil, err
	}

	var scanner *secret.Scanner
	if opts.SecretScan {
		scanner, err = secret.Load()
		if err != nil {
			return nil, err
		}
	}

	type outcome struct {
		file      *model.File
		skip      *model.Skip
		findings  []secret.Finding
		redaction *model.Redaction
	}
	outcomes := make([]outcome, len(res.Entries))

	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error
	setErr := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	}

	for i, entry := range res.Entries {
		i, entry := i, entry
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			content, err := os.ReadFile(entry.AbsPath)
			if err != nil {
				setErr(fmt.Errorf("read %s: %w", entry.RelPath, err))
				return
			}
			if isBinary(content) {
				outcomes[i] = outcome{skip: &model.Skip{Path: entry.RelPath, Reason: model.SkipBinary, Size: int64(len(content))}}
				return
			}

			var o outcome
			if scanner != nil {
				o.findings = scanner.Scan(entry.RelPath, content)
				if len(o.findings) > 0 && opts.Redact {
					var n int
					content, n = secret.Redact(content, o.findings)
					o.redaction = &model.Redaction{Path: entry.RelPath, Rule: o.findings[0].RuleID, Count: n}
				}
			}
			if opts.StripComments {
				content = strip.Comments(content, extForPath(entry.RelPath))
			}
			lang := langForPath(entry.RelPath)
			if opts.Pruner != nil {
				content, err = opts.Pruner.Prune(content, lang)
				if err != nil {
					setErr(fmt.Errorf("compress %s: %w", entry.RelPath, err))
					return
				}
			}

			sum := sha256.Sum256(content)
			f := &model.File{
				Path:    entry.RelPath,
				Size:    int64(len(content)),
				SHA256:  hex.EncodeToString(sum[:]),
				Lang:    lang,
				Content: content,
				Tokens:  model.TokensUncounted,
			}
			if opts.Counter != nil {
				n, err := opts.Counter.Count(content)
				if err != nil {
					setErr(fmt.Errorf("count tokens in %s: %w", entry.RelPath, err))
					return
				}
				f.Tokens = n
			}
			o.file = f
			outcomes[i] = o
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	// Aggregate findings; abort after the full scan so the report is complete.
	var findings []secret.Finding
	for _, o := range outcomes {
		findings = append(findings, o.findings...)
	}
	if len(findings) > 0 && !opts.Redact {
		sort.Slice(findings, func(i, j int) bool {
			if findings[i].Path != findings[j].Path {
				return findings[i].Path < findings[j].Path
			}
			return findings[i].Start < findings[j].Start
		})
		return nil, &SecretError{Findings: findings}
	}

	p := &model.Pack{Root: res.Root, Skips: res.Skips}
	for _, o := range outcomes {
		switch {
		case o.file != nil:
			p.Files = append(p.Files, *o.file)
		case o.skip != nil:
			p.Skips = append(p.Skips, *o.skip)
		}
		if o.redaction != nil {
			p.Redactions = append(p.Redactions, *o.redaction)
		}
	}

	// Deterministic order regardless of worker completion order.
	sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
	sort.Slice(p.Skips, func(i, j int) bool { return p.Skips[i].Path < p.Skips[j].Path })
	sort.Slice(p.Redactions, func(i, j int) bool { return p.Redactions[i].Path < p.Redactions[j].Path })

	dedup(p)
	finalize(p, opts)
	return p, nil
}

// dedup elides content for files whose bytes are identical to an earlier
// (path-ascending) file. The canonical copy is always the first occurrence,
// so a DupOf reference always points at a lexicographically smaller path --
// unpack restores in order and the referenced file already exists.
func dedup(p *model.Pack) {
	firstByHash := make(map[string]int, len(p.Files))
	for i := range p.Files {
		f := &p.Files[i]
		if j, ok := firstByHash[f.SHA256]; ok && bytes.Equal(p.Files[j].Content, f.Content) {
			f.DupOf = p.Files[j].Path
			f.Content = nil
			f.Tokens = 0 // counted once, on the canonical copy
			continue
		}
		firstByHash[f.SHA256] = i
	}
}

func finalize(p *model.Pack, opts Options) {
	p.TotalTokens = model.TokensUncounted
	if opts.Counter != nil {
		p.TotalTokens = 0
		p.Tokenizer = opts.Counter.Name()
		p.TokenEstimate = opts.Counter.Estimate()
	}
	for i := range p.Files {
		p.TotalBytes += p.Files[i].Size
		if opts.Counter != nil && p.Files[i].DupOf == "" {
			p.TotalTokens += p.Files[i].Tokens
		}
	}
	switch {
	case !opts.SecretScan:
		p.SecretScan = "skipped"
	case len(p.Redactions) > 0:
		p.SecretScan = "redacted"
	default:
		p.SecretScan = "clean"
	}
}

// isBinary reports whether data looks like binary rather than text: any NUL
// byte, or invalid UTF-8. Valid UTF-8 with CRLF, tabs, or unicode is text.
func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	return !utf8.Valid(data)
}

// FormatFindings renders a secret report for stderr. It never prints full
// secret values, only previews.
func FormatFindings(findings []secret.Finding) string {
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "  %s:%d  %s  (%s)\n", f.Path, f.Line, f.RuleID, f.Preview)
	}
	return b.String()
}
