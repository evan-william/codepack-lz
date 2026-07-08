// Package format renders a model.Pack into one of the output formats:
//
//	md, xml, txt  - human- and LLM-readable text (the "paste into a chat" job)
//	codepack      - lossless base64 transport envelope (the round-trip job)
//
// Every renderer is deterministic: the same Pack produces byte-identical
// output (the envelope additionally requires a pinned Created time via
// SOURCE_DATE_EPOCH; see envelope.go).
package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/evan-william/codepack-lz/internal/model"
)

// Kinds accepted by --format.
const (
	KindMarkdown = "md"
	KindXML      = "xml"
	KindText     = "txt"
	KindEnvelope = "codepack"
)

// Kinds lists valid --format values for help text and validation.
func Kinds() []string {
	return []string{KindMarkdown, KindXML, KindText, KindEnvelope}
}

// Renderer writes a Pack to w.
type Renderer interface {
	Render(w io.Writer, p *model.Pack) error
}

// New returns the renderer for kind.
func New(kind string) (Renderer, error) {
	switch kind {
	case KindMarkdown:
		return &markdown{}, nil
	case KindXML:
		return &xmlRenderer{}, nil
	case KindText:
		return &text{}, nil
	case KindEnvelope:
		return NewEnvelope(CodecGzip)
	default:
		return nil, fmt.Errorf("unknown format %q (valid: %s)", kind, strings.Join(Kinds(), ", "))
	}
}

// tokenSummary renders the pack-level token line. Empty when counting was off.
func tokenSummary(p *model.Pack) string {
	if p.TotalTokens == model.TokensUncounted {
		return ""
	}
	if p.TokenEstimate {
		return fmt.Sprintf("~%d tokens (est., %s)", p.TotalTokens, p.Tokenizer)
	}
	return fmt.Sprintf("%d tokens (exact, %s)", p.TotalTokens, p.Tokenizer)
}

// fileTokens renders the per-file token fragment, or "" when not counted.
func fileTokens(p *model.Pack, f *model.File) string {
	if f.Tokens == model.TokensUncounted || f.DupOf != "" {
		return ""
	}
	if p.TokenEstimate {
		return fmt.Sprintf("~%d tokens (est.)", f.Tokens)
	}
	return fmt.Sprintf("%d tokens (exact)", f.Tokens)
}

func tokenXMLAttrName(p *model.Pack) string {
	if p.TokenEstimate {
		return "tokens-estimate"
	}
	return "tokens-exact"
}

// secretScanLabel is the human wording for Pack.SecretScan.
func secretScanLabel(p *model.Pack) string {
	switch p.SecretScan {
	case "redacted":
		noun := "files"
		if len(p.Redactions) == 1 {
			noun = "file"
		}
		return fmt.Sprintf("redacted (%d %s)", len(p.Redactions), noun)
	case "skipped":
		return "skipped (--no-secret-scan)"
	default:
		return "clean"
	}
}

// humanBytes formats a byte count for humans (KiB-style, power of 1024).
func humanBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

// codeFence returns a backtick fence longer than any run inside content, so
// embedded markdown fences cannot break out of the block. Minimum is three.
func codeFence(content []byte) string {
	longest, run := 0, 0
	for _, b := range content {
		if b == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := longest + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}
