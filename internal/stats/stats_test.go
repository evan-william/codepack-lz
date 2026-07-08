package stats

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/evan-william/codepack-lz/internal/format"
	"github.com/evan-william/codepack-lz/internal/pack"
)

func writeEnvelope(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello\n"), 0o644))

	p, err := pack.Build(src, pack.Options{SecretScan: true})
	require.NoError(t, err)
	env, err := format.NewEnvelope(format.CodecGzip)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, env.Render(&buf, p))

	out := filepath.Join(t.TempDir(), "pack.codepack.txt")
	require.NoError(t, os.WriteFile(out, buf.Bytes(), 0o644))
	return out
}

func TestPrintEnvelopeHeaderOnly(t *testing.T) {
	path := writeEnvelope(t)

	// Truncate everything after the BEGIN marker: if stats decoded the
	// payload this would fail; reading the header alone must succeed.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	idx := strings.Index(string(raw), format.BeginMarker) + len(format.BeginMarker) + 1
	truncated := filepath.Join(t.TempDir(), "truncated.codepack.txt")
	require.NoError(t, os.WriteFile(truncated, raw[:idx], 0o644))

	var out bytes.Buffer
	require.NoError(t, Print(&out, truncated))
	require.Contains(t, out.String(), "codepack envelope (v1)")
	require.Contains(t, out.String(), "files:        1")
	require.Contains(t, out.String(), "not encryption")
	require.Contains(t, out.String(), "without decoding")
}

func TestPrintReadableSniff(t *testing.T) {
	md := filepath.Join(t.TempDir(), "out.md")
	require.NoError(t, os.WriteFile(md, []byte("# Repository: demo\n\nstuff\n"), 0o644))

	var out bytes.Buffer
	require.NoError(t, Print(&out, md))
	require.Contains(t, out.String(), "codepack markdown output")
}

func TestPrintUnknown(t *testing.T) {
	f := filepath.Join(t.TempDir(), "random.bin")
	require.NoError(t, os.WriteFile(f, []byte("garbage"), 0o644))

	var out bytes.Buffer
	require.NoError(t, Print(&out, f))
	require.Contains(t, out.String(), "unknown")
}
