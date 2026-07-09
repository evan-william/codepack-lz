package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/evan-william/codepack-lz/internal/pack"
)

func execute(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func demoRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Demo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	return dir
}

func TestCLIPackMarkdown(t *testing.T) {
	repo := demoRepo(t)
	out := filepath.Join(t.TempDir(), "ctx.md")

	_, stderr, err := execute(t, "pack", repo, "--format", "md", "-o", out)
	require.NoError(t, err)
	require.Contains(t, stderr, "packed 2 files")
	require.Contains(t, stderr, "tokens: ~")

	content, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(content), "# Repository:")
	require.Contains(t, string(content), "### src/main.go")
}

func TestCLIPackStdout(t *testing.T) {
	repo := demoRepo(t)
	stdout, _, err := execute(t, "pack", repo, "--format", "txt", "--count-tokens", "off", "-o", "-")
	require.NoError(t, err)
	require.Contains(t, stdout, "FILE: src/main.go")
}

func TestCLIPackDefaultOutput(t *testing.T) {
	repo := demoRepo(t)

	stdout, stderr, err := execute(t, "pack", repo, "--format", "md", "--count-tokens", "off")
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Contains(t, stderr, "repack-result")

	outDir := filepath.Join(repo, defaultOutputDir)
	require.DirExists(t, outDir)
	matches, err := filepath.Glob(filepath.Join(outDir, filepath.Base(repo)+"-????????-??????.md"))
	require.NoError(t, err)
	require.Len(t, matches, 1)

	content, err := os.ReadFile(matches[0])
	require.NoError(t, err)
	require.Contains(t, string(content), "### src/main.go")
}

func TestCLIEnvelopeRoundTrip(t *testing.T) {
	repo := demoRepo(t)
	envPath := filepath.Join(t.TempDir(), "repo.codepack.txt")

	_, stderr, err := execute(t, "pack", repo, "--format", "codepack", "-o", envPath)
	require.NoError(t, err)
	require.Contains(t, stderr, "not encryption", "envelope pack must warn about base64")

	outDir := filepath.Join(t.TempDir(), "restored")
	_, stderr, err = execute(t, "unpack", envPath, "--out", outDir)
	require.NoError(t, err)
	require.Contains(t, stderr, "hashes verified")

	restored, err := os.ReadFile(filepath.Join(outDir, "src", "main.go"))
	require.NoError(t, err)
	require.Equal(t, "package main\n\nfunc main() {}\n", string(restored))
}

func TestCLIEnvelopeRoundTripZstd(t *testing.T) {
	repo := demoRepo(t)
	envPath := filepath.Join(t.TempDir(), "repo.codepack.txt")

	_, stderr, err := execute(t, "pack", repo, "--format", "codepack", "--codec", "zstd", "-o", envPath)
	require.NoError(t, err)
	require.Contains(t, stderr, "not encryption")

	outDir := filepath.Join(t.TempDir(), "restored")
	_, stderr, err = execute(t, "unpack", envPath, "--out", outDir)
	require.NoError(t, err)
	require.Contains(t, stderr, "hashes verified")
	require.FileExists(t, filepath.Join(outDir, "src", "main.go"))
}

func TestCLIEncryptedEnvelopeRoundTrip(t *testing.T) {
	t.Setenv("CODEPACK_KEY_HEX", "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	repo := demoRepo(t)
	envPath := filepath.Join(t.TempDir(), "repo.codepack.txt")

	_, stderr, err := execute(t, "pack", repo, "--format", "codepack", "--codec", "zstd", "--encrypt", "-o", envPath)
	require.NoError(t, err)
	require.Contains(t, stderr, "AES-256-GCM encrypted")

	outDir := filepath.Join(t.TempDir(), "restored")
	_, stderr, err = execute(t, "unpack", envPath, "--out", outDir)
	require.NoError(t, err)
	require.Contains(t, stderr, "hashes verified")
}

func TestCLISplitOutput(t *testing.T) {
	repo := demoRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "ctx.part999.md"), []byte("old split should not be packed\n"), 0o644))
	out := filepath.Join(repo, "ctx.md")

	_, stderr, err := execute(t, "pack", repo, "--format", "md", "-o", out, "--split-output", "--split-size", "200B")
	require.NoError(t, err)
	require.Contains(t, stderr, "part(s)")
	require.NoFileExists(t, out)
	partOne := filepath.Join(filepath.Dir(out), "ctx.part001.md")
	partTwo := filepath.Join(filepath.Dir(out), "ctx.part002.md")
	require.FileExists(t, partOne)
	require.FileExists(t, partTwo)
	first, err := os.ReadFile(partOne)
	require.NoError(t, err)
	second, err := os.ReadFile(partTwo)
	require.NoError(t, err)
	combined := string(first) + string(second)
	require.NotContains(t, combined, "old split should not be packed")
}

func TestCLICompressReadableOutput(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "src"), 0o755))
	src := "package main\n\nfunc main() {\n\tsecret := 41\n\tprintln(secret + 1)\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(repo, "src", "main.go"), []byte(src), 0o644))
	out := filepath.Join(t.TempDir(), "ctx.md")

	_, _, err := execute(t, "pack", repo, "--format", "md", "--count-tokens", "off", "--compress", "-o", out)
	require.NoError(t, err)
	content, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(content), "func main()")
	require.Contains(t, string(content), "...")
	require.NotContains(t, string(content), "println(secret")
}

func TestCLICountTokensAPI(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	var mu sync.Mutex
	calls := 0
	var problems []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			problems = append(problems, "wrong x-api-key: "+got)
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problems = append(problems, err.Error())
		}
		if body.Model != "claude-test" {
			problems = append(problems, "wrong model: "+body.Model)
		}
		_, _ = w.Write([]byte(`{"input_tokens":5}`))
	}))
	defer srv.Close()

	repo := demoRepo(t)
	out := filepath.Join(t.TempDir(), "ctx.md")
	_, stderr, err := execute(t, "pack", repo, "--format", "md", "--count-tokens", "api", "--token-model", "claude-test", "--token-endpoint", srv.URL, "-o", out)
	require.NoError(t, err)
	require.Empty(t, problems)
	require.Equal(t, 2, calls)
	require.Contains(t, stderr, "tokens: 10 (exact, anthropic:claude-test)")
	content, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(content), "10 tokens (exact, anthropic:claude-test)")
}

func TestCLIUnpackDefaultDirFromRoot(t *testing.T) {
	repo := demoRepo(t)
	envPath := filepath.Join(t.TempDir(), "repo.codepack.txt")
	_, _, err := execute(t, "pack", repo, "--format", "codepack", "-o", envPath)
	require.NoError(t, err)

	work := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(work))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	_, _, err = execute(t, "unpack", envPath)
	require.NoError(t, err)
	require.DirExists(t, filepath.Join(work, filepath.Base(repo)))
}

func TestCLIStats(t *testing.T) {
	repo := demoRepo(t)
	envPath := filepath.Join(t.TempDir(), "repo.codepack.txt")
	_, _, err := execute(t, "pack", repo, "--format", "codepack", "-o", envPath)
	require.NoError(t, err)

	stdout, _, err := execute(t, "stats", envPath)
	require.NoError(t, err)
	require.Contains(t, stdout, "codepack envelope (v1)")
	require.Contains(t, stdout, "files:        2")
}

func TestCLISecretDetectionExitPath(t *testing.T) {
	repo := t.TempDir()
	key := "AKIA" + "QQQQQQQQQQQQQQQQ"
	require.NoError(t, os.WriteFile(filepath.Join(repo, "cfg.go"), []byte("key := \""+key+"\"\n"), 0o644))

	_, _, err := execute(t, "pack", repo, "--format", "md", "-o", filepath.Join(t.TempDir(), "x.md"))
	var se *pack.SecretError
	require.ErrorAs(t, err, &se, "secret findings surface as SecretError (exit 3)")

	// --redact converts the failure into a masked pack.
	out := filepath.Join(t.TempDir(), "y.md")
	_, stderr, err := execute(t, "pack", repo, "--format", "md", "-o", out, "--redact")
	require.NoError(t, err)
	require.Contains(t, stderr, "redacted secrets in 1 file(s)")
	content, _ := os.ReadFile(out)
	require.NotContains(t, string(content), key)
	require.Contains(t, string(content), "[REDACTED:aws-access-key-id]")
}

func TestCLIUsageErrors(t *testing.T) {
	repo := demoRepo(t)
	cases := [][]string{
		{"pack", repo, "--format", "docx"},
		{"pack", repo, "--format", "codepack", "--strip-comments"},
		{"pack", repo, "--codec", "zstd"},
		{"pack", repo, "--count-tokens", "api"},
		{"pack", repo, "--format", "codepack", "--compress"},
		{"pack", repo, "--format", "codepack", "--split-output", "-o", filepath.Join(t.TempDir(), "x.codepack.txt")},
		{"pack", repo, "--format", "md", "--encrypt"},
		{"pack", repo, "--format", "md", "--split-output", "-o", "-"},
		{"pack", repo, "--redact", "--no-secret-scan"},
		{"pack", repo, "--max-file-size", "banana"},
		{"pack", repo, "--bogus-flag"},
	}
	for _, args := range cases {
		_, _, err := execute(t, args...)
		var ue usageError
		require.ErrorAs(t, err, &ue, "args %v must be a usage error (exit 2)", args)
	}
}

func TestCLIVersion(t *testing.T) {
	stdout, _, err := execute(t, "version")
	require.NoError(t, err)
	require.Contains(t, stdout, "codepack 0.3.1 (envelope format v1)")
}

func TestExitCodeMapping(t *testing.T) {
	require.Equal(t, exitSecrets, exitCodeFor(&pack.SecretError{}))
	require.Equal(t, exitUsage, exitCodeFor(usageError{errors.New("bad flag")}))
	require.Equal(t, exitError, exitCodeFor(errors.New("io failure")))
}
