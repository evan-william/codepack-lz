package format

import (
	"bufio"
	"fmt"
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

	fmt.Fprintf(bw, "# Repository: %s\n\n", p.Root)
	fmt.Fprintf(bw, "- Files: %d packed, %d skipped\n", len(p.Files), len(p.Skips))
	fmt.Fprintf(bw, "- Total size: %s\n", humanBytes(p.TotalBytes))
	if ts := tokenSummary(p); ts != "" {
		fmt.Fprintf(bw, "- Tokens: %s\n", ts)
	}
	fmt.Fprintf(bw, "- Secret scan: %s\n", secretScanLabel(p))
	fmt.Fprintf(bw, "- Packed by: codepack-lz v%s\n\n", version.Version)
	fmt.Fprintf(bw, "> **Warning:** Review this output for sensitive data before sharing. Secret scanning reduces risk but cannot guarantee every secret is caught.\n\n")

	paths := make([]string, len(p.Files))
	for i := range p.Files {
		paths[i] = p.Files[i].Path
	}
	fmt.Fprintf(bw, "## File tree\n\n```\n%s```\n\n", renderTree(p.Root, paths))

	fmt.Fprintf(bw, "## Files\n\n")
	for i := range p.Files {
		f := &p.Files[i]
		fmt.Fprintf(bw, "### %s\n\n", f.Path)
		if f.DupOf != "" {
			fmt.Fprintf(bw, "Identical to `%s` (content not repeated).\n\n", f.DupOf)
			continue
		}
		meta := []string{f.Lang, humanBytes(f.Size)}
		if ft := fileTokens(p, f); ft != "" {
			meta = append(meta, ft)
		}
		fmt.Fprintf(bw, "`%s`\n\n", strings.Join(nonEmpty(meta), " - "))
		fence := codeFence(f.Content)
		fmt.Fprintf(bw, "%s%s\n", fence, f.Lang)
		bw.Write(f.Content)
		if len(f.Content) > 0 && f.Content[len(f.Content)-1] != '\n' {
			bw.WriteByte('\n')
		}
		fmt.Fprintf(bw, "%s\n\n", fence)
	}

	if len(p.Skips) > 0 {
		fmt.Fprintf(bw, "## Skipped\n\n| Path | Reason | Size |\n|---|---|---|\n")
		for _, s := range p.Skips {
			size := ""
			if s.Size > 0 {
				size = humanBytes(s.Size)
			}
			fmt.Fprintf(bw, "| %s | %s | %s |\n", escapePipes(s.Path), s.Reason, size)
		}
		bw.WriteByte('\n')
	}

	if len(p.Redactions) > 0 {
		fmt.Fprintf(bw, "## Redactions\n\n| Path | Rule | Matches |\n|---|---|---|\n")
		for _, r := range p.Redactions {
			fmt.Fprintf(bw, "| %s | %s | %d |\n", escapePipes(r.Path), r.Rule, r.Count)
		}
		bw.WriteByte('\n')
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
