package format

import (
	"bufio"
	"io"
	"strings"

	"github.com/evan-william/codepack-lz/internal/model"
	"github.com/evan-william/codepack-lz/internal/version"
)

// markdown renders the default readable format: summary, file tree, one
// fenced section per file, and a skipped-files table.
type markdown struct{}

func (markdown) Render(w io.Writer, p *model.Pack) error {
	bw := bufio.NewWriter(w)

	if err := writef(bw, "# Repository: %s\n\n", p.Root); err != nil {
		return err
	}
	if err := writef(bw, "- Files: %d packed, %d skipped\n", len(p.Files), len(p.Skips)); err != nil {
		return err
	}
	if err := writef(bw, "- Total size: %s\n", humanBytes(p.TotalBytes)); err != nil {
		return err
	}
	if ts := tokenSummary(p); ts != "" {
		if err := writef(bw, "- Tokens: %s\n", ts); err != nil {
			return err
		}
	}
	if err := writef(bw, "- Secret scan: %s\n", secretScanLabel(p)); err != nil {
		return err
	}
	if err := writef(bw, "- Packed by: codepack-lz v%s\n\n", version.Version); err != nil {
		return err
	}
	if err := writef(bw, "> **Warning:** Review this output for sensitive data before sharing. Secret scanning reduces risk but cannot guarantee every secret is caught.\n\n"); err != nil {
		return err
	}

	paths := make([]string, len(p.Files))
	for i := range p.Files {
		paths[i] = p.Files[i].Path
	}
	if err := writef(bw, "## File tree\n\n```\n%s```\n\n", renderTree(p.Root, paths)); err != nil {
		return err
	}

	if err := writef(bw, "## Files\n\n"); err != nil {
		return err
	}
	for i := range p.Files {
		f := &p.Files[i]
		if err := writef(bw, "### %s\n\n", f.Path); err != nil {
			return err
		}
		if f.DupOf != "" {
			if err := writef(bw, "Identical to `%s` (content not repeated).\n\n", f.DupOf); err != nil {
				return err
			}
			continue
		}
		meta := []string{f.Lang, humanBytes(f.Size)}
		if ft := fileTokens(p, f); ft != "" {
			meta = append(meta, ft)
		}
		if err := writef(bw, "`%s`\n\n", strings.Join(nonEmpty(meta), " - ")); err != nil {
			return err
		}
		fence := codeFence(f.Content)
		if err := writef(bw, "%s%s\n", fence, f.Lang); err != nil {
			return err
		}
		if _, err := bw.Write(f.Content); err != nil {
			return err
		}
		if len(f.Content) > 0 && f.Content[len(f.Content)-1] != '\n' {
			if err := bw.WriteByte('\n'); err != nil {
				return err
			}
		}
		if err := writef(bw, "%s\n\n", fence); err != nil {
			return err
		}
	}

	if len(p.Skips) > 0 {
		if err := writef(bw, "## Skipped\n\n| Path | Reason | Size |\n|---|---|---|\n"); err != nil {
			return err
		}
		for _, s := range p.Skips {
			size := ""
			if s.Size > 0 {
				size = humanBytes(s.Size)
			}
			if err := writef(bw, "| %s | %s | %s |\n", escapePipes(s.Path), s.Reason, size); err != nil {
				return err
			}
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}

	if len(p.Redactions) > 0 {
		if err := writef(bw, "## Redactions\n\n| Path | Rule | Matches |\n|---|---|---|\n"); err != nil {
			return err
		}
		for _, r := range p.Redactions {
			if err := writef(bw, "| %s | %s | %d |\n", escapePipes(r.Path), r.Rule, r.Count); err != nil {
				return err
			}
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}

	return bw.Flush()
}

func nonEmpty(items []string) []string {
	out := items[:0]
	for _, s := range items {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
