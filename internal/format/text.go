package format

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/evan-william/codepack-lz/internal/model"
	"github.com/evan-william/codepack-lz/internal/version"
)

// text renders a plain-text bundle with banner-separated file sections, for
// tools and prompts where markdown or XML markup is unwanted.
type text struct{}

const textRule = "================================================================"

func (text) Render(w io.Writer, p *model.Pack) error {
	bw := bufio.NewWriter(w)

	fmt.Fprintf(bw, "CodePack-LZ v%s - repository: %s\n", version.Version, p.Root)
	fmt.Fprintf(bw, "files: %d packed, %d skipped - total: %s", len(p.Files), len(p.Skips), humanBytes(p.TotalBytes))
	if ts := tokenSummary(p); ts != "" {
		fmt.Fprintf(bw, " - %s", ts)
	}
	fmt.Fprintf(bw, "\nsecret scan: %s\n", secretScanLabel(p))
	fmt.Fprintf(bw, "warning: review for sensitive data before sharing; scanning cannot guarantee every secret is caught.\n\n")

	paths := make([]string, len(p.Files))
	for i := range p.Files {
		paths[i] = p.Files[i].Path
	}
	fmt.Fprintf(bw, "FILE TREE\n---------\n%s\n", renderTree(p.Root, paths))

	for i := range p.Files {
		f := &p.Files[i]
		meta := []string{f.Lang, humanBytes(f.Size)}
		if ft := fileTokens(p, f); ft != "" {
			meta = append(meta, ft)
		}
		fmt.Fprintf(bw, "%s\nFILE: %s (%s)\n%s\n", textRule, f.Path, strings.Join(nonEmpty(meta), " - "), textRule)
		if f.DupOf != "" {
			fmt.Fprintf(bw, "identical to %s (content not repeated)\n\n", f.DupOf)
			continue
		}
		bw.Write(f.Content)
		if len(f.Content) > 0 && f.Content[len(f.Content)-1] != '\n' {
			bw.WriteByte('\n')
		}
		bw.WriteByte('\n')
	}

	if len(p.Skips) > 0 {
		fmt.Fprintf(bw, "SKIPPED\n-------\n")
		for _, s := range p.Skips {
			if s.Size > 0 {
				fmt.Fprintf(bw, "%s - %s (%s)\n", s.Path, s.Reason, humanBytes(s.Size))
			} else {
				fmt.Fprintf(bw, "%s - %s\n", s.Path, s.Reason)
			}
		}
		bw.WriteByte('\n')
	}

	if len(p.Redactions) > 0 {
		fmt.Fprintf(bw, "REDACTIONS\n----------\n")
		for _, r := range p.Redactions {
			fmt.Fprintf(bw, "%s - %s (%d matches)\n", r.Path, r.Rule, r.Count)
		}
		bw.WriteByte('\n')
	}

	return bw.Flush()
}
