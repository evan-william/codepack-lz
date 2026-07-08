package format

import (
	"bufio"
	"fmt"
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

	fmt.Fprintf(bw, `<repository root="%s" files="%d" skipped="%d" total-bytes="%d" tool="codepack-lz v%s"`,
		attrEscape(p.Root), len(p.Files), len(p.Skips), p.TotalBytes, version.Version)
	if p.TotalTokens != model.TokensUncounted {
		fmt.Fprintf(bw, ` %s="%d" tokenizer="%s"`, tokenXMLAttrName(p), p.TotalTokens, attrEscape(p.Tokenizer))
	}
	fmt.Fprintf(bw, " secret-scan=\"%s\">\n", attrEscape(p.SecretScan))
	if p.TotalTokens != model.TokensUncounted && !p.TokenEstimate {
		fmt.Fprintf(bw, "<notice>Token counts are exact provider counts for the configured model. Review for sensitive data before sharing; secret scanning cannot guarantee every secret is caught. File content below is raw text, not XML-escaped.</notice>\n")
	} else {
		fmt.Fprintf(bw, "<notice>Token counts are estimates, not exact. Review for sensitive data before sharing; secret scanning cannot guarantee every secret is caught. File content below is raw text, not XML-escaped.</notice>\n")
	}

	paths := make([]string, len(p.Files))
	for i := range p.Files {
		paths[i] = p.Files[i].Path
	}
	fmt.Fprintf(bw, "<file_tree>\n%s</file_tree>\n", renderTree(p.Root, paths))

	fmt.Fprintf(bw, "<files>\n")
	for i := range p.Files {
		f := &p.Files[i]
		if f.DupOf != "" {
			fmt.Fprintf(bw, "<file path=\"%s\" identical-to=\"%s\"/>\n", attrEscape(f.Path), attrEscape(f.DupOf))
			continue
		}
		fmt.Fprintf(bw, `<file path="%s" lang="%s" bytes="%d"`, attrEscape(f.Path), attrEscape(f.Lang), f.Size)
		if f.Tokens != model.TokensUncounted {
			fmt.Fprintf(bw, ` %s="%d"`, tokenXMLAttrName(p), f.Tokens)
		}
		fmt.Fprintf(bw, ">\n")
		bw.Write(f.Content)
		if len(f.Content) > 0 && f.Content[len(f.Content)-1] != '\n' {
			bw.WriteByte('\n')
		}
		fmt.Fprintf(bw, "</file>\n")
	}
	fmt.Fprintf(bw, "</files>\n")

	if len(p.Skips) > 0 {
		fmt.Fprintf(bw, "<skipped>\n")
		for _, s := range p.Skips {
			fmt.Fprintf(bw, "<skip path=\"%s\" reason=\"%s\" bytes=\"%d\"/>\n", attrEscape(s.Path), attrEscape(s.Reason), s.Size)
		}
		fmt.Fprintf(bw, "</skipped>\n")
	}

	if len(p.Redactions) > 0 {
		fmt.Fprintf(bw, "<redactions>\n")
		for _, r := range p.Redactions {
			fmt.Fprintf(bw, "<redaction path=\"%s\" rule=\"%s\" matches=\"%d\"/>\n", attrEscape(r.Path), attrEscape(r.Rule), r.Count)
		}
		fmt.Fprintf(bw, "</redactions>\n")
	}

	fmt.Fprintf(bw, "</repository>\n")
	return bw.Flush()
}

// attrEscape escapes the characters that would break a double-quoted
// attribute value.
func attrEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
