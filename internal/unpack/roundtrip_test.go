package unpack

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/evan-william/codepack-lz/internal/format"
	"github.com/evan-william/codepack-lz/internal/model"
	"github.com/evan-william/codepack-lz/internal/pack"
)

// trickyTree builds a source tree with every content edge case the envelope
// must survive byte-for-byte.
func trickyTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string][]byte{
		"crlf.txt":            []byte("one\r\ntwo\r\n"),
		"empty.txt":           {},
		"no-trailing-newline": []byte("abc"),
		"unicode/日本語.md":      []byte("# héllo ☃ ünïcode\n"),
		"deep/a/b/c/d.txt":    []byte("nested\n"),
		"fences.md":           []byte("```go\ncode\n```\n"),
		"dup/first.txt":       []byte("same bytes\n"),
		"dup/second.txt":      []byte("same bytes\n"),
		"tabs and spaces.txt": []byte("\tindent \n trailing \n"),
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, content, 0o644))
	}
	return root
}

func buildEnvelope(t *testing.T, root string) []byte {
	return buildEnvelopeWith(t, root, format.CodecGzip)
}

func buildEnvelopeWith(t *testing.T, root string, codec string, opts ...format.EnvelopeOption) []byte {
	t.Helper()
	p, err := pack.Build(root, pack.Options{SecretScan: true})
	require.NoError(t, err)
	env, err := format.NewEnvelope(codec, opts...)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, env.Render(&buf, p))
	return buf.Bytes()
}

func treeSnapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		require.NoError(t, err)
		content, err := os.ReadFile(p)
		require.NoError(t, err)
		snap[filepath.ToSlash(rel)] = content
		return nil
	})
	require.NoError(t, err)
	return snap
}

func TestRoundTripByteIdentical(t *testing.T) {
	src := trickyTree(t)
	envelope := buildEnvelope(t, src)

	out := filepath.Join(t.TempDir(), "restored")
	sum, err := Restore(bytes.NewReader(envelope), out, false)
	require.NoError(t, err)
	require.Equal(t, 9, sum.Files)
	require.Equal(t, sum.Files, sum.Verified, "every restored file is hash-verified, duplicates via their canonical copy on disk")

	require.Equal(t, treeSnapshot(t, src), treeSnapshot(t, out), "pack->unpack must reproduce the tree byte-for-byte")
}

func TestRoundTripZstd(t *testing.T) {
	src := trickyTree(t)
	envelope := buildEnvelopeWith(t, src, format.CodecZstd)

	out := filepath.Join(t.TempDir(), "restored")
	sum, err := Restore(bytes.NewReader(envelope), out, false)
	require.NoError(t, err)
	require.Equal(t, 9, sum.Files)
	require.Equal(t, treeSnapshot(t, src), treeSnapshot(t, out))
}

func TestEncryptedEnvelopeRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	src := trickyTree(t)
	envelope := buildEnvelopeWith(t, src, format.CodecZstd, format.WithEncryptionKey(key))

	h, err := ReadHeader(bufio.NewReader(bytes.NewReader(envelope)))
	require.NoError(t, err)
	require.Equal(t, format.CodecZstd, h.Codec)
	require.Equal(t, format.EncryptionAES256GCM, h.Encryption)
	require.Contains(t, h.Warning, "encrypted")

	_, err = Restore(bytes.NewReader(envelope), filepath.Join(t.TempDir(), "missing-key"), true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires a 32-byte key")

	out := filepath.Join(t.TempDir(), "restored")
	sum, err := RestoreWithOptions(bytes.NewReader(envelope), out, RestoreOptions{EncryptionKey: key})
	require.NoError(t, err)
	require.Equal(t, 9, sum.Files)
	require.Equal(t, treeSnapshot(t, src), treeSnapshot(t, out))
}

func TestEnvelopeDeterministic(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1783468800") // 2026-07-08T00:00:00Z
	src := trickyTree(t)
	first := buildEnvelope(t, src)
	second := buildEnvelope(t, src)
	require.Equal(t, first, second, "same input, same tool -> byte-identical envelope")
	require.Contains(t, string(first), "Created: 2026-07-08T00:00:00Z")
}

func TestTamperedHashNamesFile(t *testing.T) {
	src := trickyTree(t)
	p, err := pack.Build(src, pack.Options{SecretScan: true})
	require.NoError(t, err)
	// Simulate corruption: stored hash no longer matches stored bytes.
	p.Files[0].SHA256 = strings.Repeat("00", 32)

	env, err := format.NewEnvelope(format.CodecGzip)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, env.Render(&buf, p))

	_, err = Restore(&buf, t.TempDir(), true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "hash mismatch")
	require.Contains(t, err.Error(), p.Files[0].Path, "the failing file is named")
}

func TestCorruptedPayloadFails(t *testing.T) {
	envelope := buildEnvelope(t, trickyTree(t))

	// Flip one payload character in the middle of the compressed stream (the
	// gzip header near the start is not integrity-checked; the deflate data
	// is). Stay within the base64 alphabet so decoding proceeds.
	s := string(envelope)
	begin := strings.Index(s, format.BeginMarker) + len(format.BeginMarker)
	end := strings.Index(s, format.EndMarker)
	idx := begin + (end-begin)/2
	for s[idx] == '\n' || s[idx] == 'A' {
		idx++
	}
	corrupted := []byte(s[:idx] + "A" + s[idx+1:])

	out := filepath.Join(t.TempDir(), "restored")
	_, err := Restore(bytes.NewReader(corrupted), out, false)
	require.Error(t, err, "corruption must never restore silently")
}

func TestPathTraversalRejected(t *testing.T) {
	evil := []string{"../evil.txt", "/abs.txt", "C:/evil.txt", "a/../../evil.txt", "back\\slash.txt", "a//b.txt", "./dot.txt"}
	for _, path := range evil {
		p := &model.Pack{
			Root: "evil",
			Files: []model.File{{
				Path: path, Size: 2, SHA256: shaOf("x\n"), Content: []byte("x\n"), Tokens: model.TokensUncounted,
			}},
			TotalTokens: model.TokensUncounted,
			SecretScan:  "clean",
		}
		env, err := format.NewEnvelope(format.CodecGzip)
		require.NoError(t, err)
		var buf bytes.Buffer
		require.NoError(t, env.Render(&buf, p))

		_, err = Restore(&buf, t.TempDir(), false)
		require.Error(t, err, "path %q must be rejected", path)
		require.Contains(t, err.Error(), "unsafe path", "path %q", path)
	}
}

func TestRefuseOverwrite(t *testing.T) {
	envelope := buildEnvelope(t, trickyTree(t))
	out := filepath.Join(t.TempDir(), "restored")

	_, err := Restore(bytes.NewReader(envelope), out, false)
	require.NoError(t, err)
	_, err = Restore(bytes.NewReader(envelope), out, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to overwrite")
}

func TestDryRunWritesNothing(t *testing.T) {
	envelope := buildEnvelope(t, trickyTree(t))
	out := filepath.Join(t.TempDir(), "restored")

	sum, err := Restore(bytes.NewReader(envelope), out, true)
	require.NoError(t, err)
	require.True(t, sum.DryRun)
	require.Equal(t, 9, sum.Files)
	_, err = os.Stat(out)
	require.True(t, os.IsNotExist(err), "dry run must not create the output directory")
}

func TestLegacyPrototypeDetected(t *testing.T) {
	legacy := "CODEPACK-LZ v0.1\nencoding: gzip+base64\n\n---BEGIN CODEPACK-LZ---\nQUJD\n---END CODEPACK-LZ---\n"
	_, err := ReadHeader(bufio.NewReader(strings.NewReader(legacy)))
	require.ErrorIs(t, err, ErrLegacyFormat)
}

func TestReadHeaderStopsBeforePayload(t *testing.T) {
	envelope := buildEnvelope(t, trickyTree(t))
	// Truncate right after the BEGIN marker: the payload is unreadable, but
	// the header must still parse -- proof that nothing gets decoded.
	s := string(envelope)
	idx := strings.Index(s, format.BeginMarker) + len(format.BeginMarker) + 1
	h, err := ReadHeader(bufio.NewReader(strings.NewReader(s[:idx])))
	require.NoError(t, err)
	require.Equal(t, 9, h.Files)
	require.Equal(t, "gzip", h.Codec)
	require.Contains(t, h.Warning, "not encryption")
}

func TestNotAnEnvelope(t *testing.T) {
	_, err := ReadHeader(bufio.NewReader(strings.NewReader("# Repository: demo\n\nplain markdown\n")))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrLegacyFormat)
}

func shaOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
