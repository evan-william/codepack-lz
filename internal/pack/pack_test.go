package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/evan-william/codepack-lz/internal/model"
	"github.com/evan-william/codepack-lz/internal/prune"
)

const sampleRepo = "../../testdata/sample-repo"

func TestBuildSampleRepo(t *testing.T) {
	p, err := Build(sampleRepo, Options{SecretScan: true})
	require.NoError(t, err)

	require.Equal(t, "sample-repo", p.Root)
	wantPaths := []string{
		".codepackignore", "README.md", "crlf.txt", "docs/guide.md",
		"src/a.txt", "src/b.txt", "src/main.go",
	}
	gotPaths := make([]string, len(p.Files))
	for i, f := range p.Files {
		gotPaths[i] = f.Path
	}
	require.Equal(t, wantPaths, gotPaths, "sorted, deterministic file list")

	// Dedup: b.txt is byte-identical to a.txt and must reference it.
	var dup *model.File
	for i := range p.Files {
		if p.Files[i].Path == "src/b.txt" {
			dup = &p.Files[i]
		}
	}
	require.NotNil(t, dup)
	require.Equal(t, "src/a.txt", dup.DupOf)
	require.Nil(t, dup.Content)

	// CRLF is content, not noise: stored verbatim.
	for i := range p.Files {
		if p.Files[i].Path == "crlf.txt" {
			require.Equal(t, "line one\r\nline two\r\n", string(p.Files[i].Content))
		}
	}

	// Skips recorded with reasons; nothing silently dropped.
	reasons := map[string]string{}
	for _, s := range p.Skips {
		reasons[s.Path] = s.Reason
	}
	require.Equal(t, model.SkipDefaultIgnore, reasons["assets/logo.png"], "binary extension skipped by default list")
	require.Equal(t, model.SkipIgnoreFile, reasons["notes.generated.txt"], ".codepackignore honored")

	require.Equal(t, "clean", p.SecretScan)
	require.Equal(t, model.TokensUncounted, p.TotalTokens, "no counter configured")

	// Determinism: identical input -> identical Pack.
	again, err := Build(sampleRepo, Options{SecretScan: true})
	require.NoError(t, err)
	require.Equal(t, p, again)
}

func TestBuildBinarySniff(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "blob.dat"), []byte{1, 2, 0, 3}, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ok.txt"), []byte("text\n"), 0o644))

	p, err := Build(root, Options{SecretScan: true})
	require.NoError(t, err)
	require.Len(t, p.Files, 1)
	require.Equal(t, "ok.txt", p.Files[0].Path)
	require.Len(t, p.Skips, 1)
	require.Equal(t, model.SkipBinary, p.Skips[0].Reason)
	require.Equal(t, int64(4), p.Skips[0].Size)
}

func TestBuildSecretAborts(t *testing.T) {
	root := t.TempDir()
	key := "AKIA" + "QQQQQQQQQQQQQQQQ"
	require.NoError(t, os.WriteFile(filepath.Join(root, "cfg.go"), []byte("key := \""+key+"\"\n"), 0o644))

	_, err := Build(root, Options{SecretScan: true})
	var se *SecretError
	require.ErrorAs(t, err, &se)
	require.Len(t, se.Findings, 1)
	require.NotContains(t, FormatFindings(se.Findings), key, "report never prints the secret")
}

func TestBuildRedact(t *testing.T) {
	root := t.TempDir()
	key := "AKIA" + "QQQQQQQQQQQQQQQQ"
	require.NoError(t, os.WriteFile(filepath.Join(root, "cfg.go"), []byte("key := \""+key+"\"\n"), 0o644))

	p, err := Build(root, Options{SecretScan: true, Redact: true})
	require.NoError(t, err)
	require.Equal(t, "redacted", p.SecretScan)
	require.Len(t, p.Redactions, 1)
	require.Equal(t, "cfg.go", p.Redactions[0].Path)

	content := string(p.Files[0].Content)
	require.NotContains(t, content, key)
	require.Contains(t, content, "[REDACTED:aws-access-key-id]")
	// Invariant: stored hash is the hash of stored (redacted) content.
	requireHashMatchesContent(t, &p.Files[0])
}

func TestBuildStripCommentsHashInvariant(t *testing.T) {
	root := t.TempDir()
	src := "package x\n\n// comment to remove\nvar A = 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "x.go"), []byte(src), 0o644))

	p, err := Build(root, Options{SecretScan: true, StripComments: true})
	require.NoError(t, err)
	require.NotContains(t, string(p.Files[0].Content), "comment to remove")
	requireHashMatchesContent(t, &p.Files[0])
}

func TestBuildPruneHashInvariant(t *testing.T) {
	root := t.TempDir()
	src := "package x\n\nfunc Big() int {\n\tvalue := 41\n\treturn value + 1\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "x.go"), []byte(src), 0o644))

	p, err := Build(root, Options{SecretScan: true, Pruner: prune.NewHeuristic()})
	require.NoError(t, err)
	require.NotContains(t, string(p.Files[0].Content), "value := 41")
	requireHashMatchesContent(t, &p.Files[0])
}

type stubCounter struct{}

func (stubCounter) Name() string                { return "stub" }
func (stubCounter) Estimate() bool              { return true }
func (stubCounter) Count(b []byte) (int, error) { return len(b), nil }

func TestBuildTokenTotals(t *testing.T) {
	p, err := Build(sampleRepo, Options{SecretScan: true, Counter: stubCounter{}})
	require.NoError(t, err)
	require.Equal(t, "stub", p.Tokenizer)

	sum := 0
	for _, f := range p.Files {
		if f.DupOf == "" {
			require.Equal(t, int(f.Size), f.Tokens)
			sum += f.Tokens
		} else {
			require.Zero(t, f.Tokens, "duplicates are counted once, on the canonical copy")
		}
	}
	require.Equal(t, sum, p.TotalTokens)
}

// requireHashMatchesContent asserts the core invariant: SHA256 and Size
// always describe the stored content, even after redaction or stripping.
func requireHashMatchesContent(t *testing.T, f *model.File) {
	t.Helper()
	require.Equal(t, int64(len(f.Content)), f.Size)
	sum := sha256.Sum256(f.Content)
	require.Equal(t, hex.EncodeToString(sum[:]), f.SHA256)
}

func BenchmarkBuild(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 200; i++ {
		dir := filepath.Join(root, fmt.Sprintf("pkg%02d", i%20))
		require.NoError(b, os.MkdirAll(dir, 0o755))
		content := strings.Repeat(fmt.Sprintf("func f%d() int { return %d } // generated\n", i, i), 50)
		require.NoError(b, os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.go", i)), []byte(content), 0o644))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Build(root, Options{SecretScan: true}); err != nil {
			b.Fatal(err)
		}
	}
}
