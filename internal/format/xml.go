package format

import (
	"bufio"
	"io"
	"strings"

	"github.com/evan-william/codepack-lz/internal/model"
	"github.com/evan-william/codepack-lz/internal/version"
)

// xmlRenderer emits an XML-styled bundle in the same spirit as Repomix's XML
// output: tags structure the document for the LLM, but file content is
// embedded raw (NOT entity-escaped) so the model reads real source instead
// of &lt;-soup and no tokens are wasted on escaping. This output is for LLM
// consumption, not for XML parsers; attribute values are escaped, text
// content is verbatim. Documented in docs/format-spec.md.
type xmlRenderer struct{}

func (xmlRenderer) Render(w io.Writer, p *model.Pack) error {
	bw := bufio.NewWriter(w)

	if err := writef(bw, `<repository root="%s" files="%d" skipped="%d" total-bytes="%d" tool="codepack-lz v%s"`,
		attrEscape(p.Root), len(p.Files), len(p.Skips), p.TotalBytes, version.Version); err != nil {
		return err
	}
	if p.TotalTokens != model.TokensUncounted {
		if err := writef(bw, ` %s="%d" tokenizer="%s"`, tokenXMLAttrName(p), p.TotalTokens, attrEscape(p.Tokenizer)); err != nil {
			return err
		}
	}
	if err := writef(bw, " secret-scan=\"%s\">\n", attrEscape(p.SecretScan)); err != nil {
		return err
	}
	if p.TotalTokens != model.TokensUncounted && !p.TokenEstimate {
		if err := writef(bw, "<notice>Token counts are exact provider counts for the configured model. Review for sensitive data before sharing; secret scanning cannot guarantee every secret is caught. File content below is raw text, not XML-escaped.</notice>\n"); err != nil {
			return err
		}
	} else {
		if err := writef(bw, "<notice>Token counts are estimates, not exact. Review for sensitive data before sharing; secret scanning cannot guarantee every secret is caught. File content below is raw text, not XML-escaped.</notice>\n"); err != nil {
			return err
		}
	}

	paths := make([]string, len(p.Files))
	for i := range p.Files {
		paths[i] = p.Files[i].Path
	}
	if err := writef(bw, "<file_tree>\n%s</file_tree>\n", renderTree(p.Root, paths)); err != nil {
		return err
	}

	if err := writef(bw, "<files>\n"); err != nil {
		return err
	}
	for i := range p.Files {
		f := &p.Files[i]
		if f.DupOf != "" {
			if err := writef(bw, "<file path=\"%s\" identical-to=\"%s\"/>\n", attrEscape(f.Path), attrEscape(f.DupOf)); err != nil {
				return err
			}
			continue
		}
		if err := writef(bw, `<file path="%s" lang="%s" bytes="%d"`, attrEscape(f.Path), attrEscape(f.Lang), f.Size); err != nil {
			return err
		}
		if f.Tokens != model.TokensUncounted {
			if err := writef(bw, ` %s="%d"`, tokenXMLAttrName(p), f.Tokens); err != nil {
				return err
			}
		}
		if err := writef(bw, ">\n"); err != nil {
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
		if err := writef(bw, "</file>\n"); err != nil {
			return err
		}
	}
	if err := writef(bw, "</files>\n"); err != nil {
		return err
	}

	if len(p.Skips) > 0 {
		if err := writef(bw, "<skipped>\n"); err != nil {
			return err
		}
		for _, s := range p.Skips {
			if err := writef(bw, "<skip path=\"%s\" reason=\"%s\" bytes=\"%d\"/>\n", attrEscape(s.Path), attrEscape(s.Reason), s.Size); err != nil {
				return err
			}
		}
		if err := writef(bw, "</skipped>\n"); err != nil {
			return err
		}
	}

	if len(p.Redactions) > 0 {
		if err := writef(bw, "<redactions>\n"); err != nil {
			return err
		}
		for _, r := range p.Redactions {
			if err := writef(bw, "<redaction path=\"%s\" rule=\"%s\" matches=\"%d\"/>\n", attrEscape(r.Path), attrEscape(r.Rule), r.Count); err != nil {
				return err
			}
		}
		if err := writef(bw, "</redactions>\n"); err != nil {
			return err
		}
	}

	if err := writef(bw, "</repository>\n"); err != nil {
		return err
	}
	return bw.Flush()
}

// attrEscape escapes the characters that would break a double-quoted
// attribute value.
func attrEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
