package format

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/evan-william/codepack-lz/internal/model"
)

var update = flag.Bool("update", false, "regenerate golden files")

// fixturePack is a hand-built Pack exercising every renderer feature:
// languages, a duplicate, a skip, a redaction, and token counts.
func fixturePack() *model.Pack {
	files := []model.File{
		{Path: "README.md", Size: 28, Lang: "markdown", Content: []byte("# Demo\n\nUnicode text works.\n"), Tokens: 9},
		{Path: "src/app.go", Size: 57, Lang: "go", Content: []byte("package app\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"), Tokens: 18},
		{Path: "src/copy.go", Size: 57, Lang: "go", DupOf: "src/app.go", Tokens: 0},
	}
	for i := range files {
		files[i].SHA256 = strings.Repeat("ab", 32) // stable placeholder digest
	}
	return &model.Pack{
		Root:          "demo",
		Files:         files,
		Skips:         []model.Skip{{Path: "big.bin", Reason: model.SkipBinary, Size: 4096}},
		Redactions:    []model.Redaction{{Path: "src/app.go", Rule: "generic-api-key-quoted", Count: 1}},
		TotalBytes:    140,
		TotalTokens:   27,
		Tokenizer:     "heuristic",
		TokenEstimate: true,
		SecretScan:    "redacted",
	}
}

func TestGoldenReadableFormats(t *testing.T) {
	for _, kind := range []string{KindMarkdown, KindXML, KindText} {
		t.Run(kind, func(t *testing.T) {
			r, err := New(kind)
			require.NoError(t, err)

			var buf bytes.Buffer
			require.NoError(t, r.Render(&buf, fixturePack()))

			golden := filepath.Join("testdata", "sample."+kind+".golden")
			if *update {
				require.NoError(t, os.MkdirAll("testdata", 0o755))
				require.NoError(t, os.WriteFile(golden, buf.Bytes(), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err, "run `go test ./internal/format -update` to generate goldens")
			require.Equal(t, string(want), buf.String())

			// Byte-identical on re-render.
			var again bytes.Buffer
			require.NoError(t, r.Render(&again, fixturePack()))
			require.Equal(t, buf.Bytes(), again.Bytes())
		})
	}
}

func TestMarkdownFenceCollision(t *testing.T) {
	p := &model.Pack{
		Root: "demo",
		Files: []model.File{{
			Path: "notes.md", Size: 30, Lang: "markdown",
			Content: []byte("text\n````\nnested fence\n````\n"),
			Tokens:  model.TokensUncounted,
		}},
		TotalTokens: model.TokensUncounted,
		SecretScan:  "clean",
	}
	var buf bytes.Buffer
	r, _ := New(KindMarkdown)
	require.NoError(t, r.Render(&buf, p))
	require.Contains(t, buf.String(), "`````markdown\n", "fence must exceed the longest run in content")
}

func TestXMLAttributeEscaping(t *testing.T) {
	p := &model.Pack{
		Root: `we"ird&<root>`,
		Files: []model.File{{
			Path: "a.txt", Size: 2, Lang: "text", Content: []byte("x\n"), Tokens: model.TokensUncounted,
		}},
		TotalTokens: model.TokensUncounted,
		SecretScan:  "clean",
	}
	var buf bytes.Buffer
	r, _ := New(KindXML)
	require.NoError(t, r.Render(&buf, p))
	require.Contains(t, buf.String(), `root="we&quot;ird&amp;&lt;root&gt;"`)
}

func TestCodeFence(t *testing.T) {
	require.Equal(t, "```", codeFence([]byte("no backticks")))
	require.Equal(t, "````", codeFence([]byte("uses ``` inside")))
	require.Equal(t, "``````", codeFence([]byte("````` five")))
}

func TestHumanBytes(t *testing.T) {
	require.Equal(t, "512 B", humanBytes(512))
	require.Equal(t, "1.0 KiB", humanBytes(1024))
	require.Equal(t, "1.5 MiB", humanBytes(1572864))
}

func TestNewUnknownKind(t *testing.T) {
	_, err := New("yaml")
	require.Error(t, err)
}
