package format

import (
	"bufio"
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

	if err := writef(bw, "CodePack-LZ v%s - repository: %s\n", version.Version, p.Root); err != nil {
		return err
	}
	if err := writef(bw, "files: %d packed, %d skipped - total: %s", len(p.Files), len(p.Skips), humanBytes(p.TotalBytes)); err != nil {
		return err
	}
	if ts := tokenSummary(p); ts != "" {
		if err := writef(bw, " - %s", ts); err != nil {
			return err
		}
	}
	if err := writef(bw, "\nsecret scan: %s\n", secretScanLabel(p)); err != nil {
		return err
	}
	if err := writef(bw, "warning: review for sensitive data before sharing; scanning cannot guarantee every secret is caught.\n\n"); err != nil {
		return err
	}

	paths := make([]string, len(p.Files))
	for i := range p.Files {
		paths[i] = p.Files[i].Path
	}
	if err := writef(bw, "FILE TREE\n---------\n%s\n", renderTree(p.Root, paths)); err != nil {
		return err
	}

	for i := range p.Files {
		f := &p.Files[i]
		meta := []string{f.Lang, humanBytes(f.Size)}
		if ft := fileTokens(p, f); ft != "" {
			meta = append(meta, ft)
		}
		if err := writef(bw, "%s\nFILE: %s (%s)\n%s\n", textRule, f.Path, strings.Join(nonEmpty(meta), " - "), textRule); err != nil {
			return err
		}
		if f.DupOf != "" {
			if err := writef(bw, "identical to %s (content not repeated)\n\n", f.DupOf); err != nil {
				return err
			}
			continue
		}
		if _, err := bw.Write(f.Content); err != nil {
			return err
		}
		if len(f.Content) > 0 && f.Content[len(f.Content)-1] != '\n' {
			if err := bw.WriteByte('\n'); err != nil {
				return err
			}
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}

	if len(p.Skips) > 0 {
		if err := writef(bw, "SKIPPED\n-------\n"); err != nil {
			return err
		}
		for _, s := range p.Skips {
			if s.Size > 0 {
				if err := writef(bw, "%s - %s (%s)\n", s.Path, s.Reason, humanBytes(s.Size)); err != nil {
					return err
				}
			} else {
				if err := writef(bw, "%s - %s\n", s.Path, s.Reason); err != nil {
					return err
				}
			}
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}

	if len(p.Redactions) > 0 {
		if err := writef(bw, "REDACTIONS\n----------\n"); err != nil {
			return err
		}
		for _, r := range p.Redactions {
			if err := writef(bw, "%s - %s (%d matches)\n", r.Path, r.Rule, r.Count); err != nil {
				return err
			}
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}

	return bw.Flush()
}
