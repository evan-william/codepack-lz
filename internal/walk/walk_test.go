package walk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/evan-william/codepack-lz/internal/model"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func relPaths(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.RelPath
	}
	return out
}

func skipByPath(skips []model.Skip, path string) *model.Skip {
	for i := range skips {
		if skips[i].Path == path {
			return &skips[i]
		}
	}
	return nil
}

func TestCollectDefaultIgnores(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, "node_modules/lib/index.js", "x\n")
	writeFile(t, root, ".git/config", "x\n")
	writeFile(t, root, "app.log", "x\n")
	writeFile(t, root, ".env", "SECRET=1\n")
	writeFile(t, root, ".env.example", "SECRET=\n")
	writeFile(t, root, "package-lock.json", "{}\n")
	writeFile(t, root, ".github/workflows/ci.yml", "on: push\n")

	res, err := Collect(root, Options{})
	require.NoError(t, err)

	require.Equal(t, []string{".env.example", ".github/workflows/ci.yml", "main.go"}, relPaths(res.Entries),
		"dotfiles stay unless explicitly ignored; .env.example is re-included by negation")

	require.NotNil(t, skipByPath(res.Skips, "node_modules/"), "dir pruned and recorded once")
	require.NotNil(t, skipByPath(res.Skips, ".git/"))
	require.Equal(t, model.SkipDefaultIgnore, skipByPath(res.Skips, ".env").Reason)
	require.Equal(t, model.SkipDefaultIgnore, skipByPath(res.Skips, "app.log").Reason)
	require.Equal(t, model.SkipDefaultIgnore, skipByPath(res.Skips, "package-lock.json").Reason)
}

func TestCollectNoDefaultIgnore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app.log", "x\n")

	res, err := Collect(root, Options{NoDefaultIgnore: true})
	require.NoError(t, err)
	require.Equal(t, []string{"app.log"}, relPaths(res.Entries))
}

func TestCollectIgnoreFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, IgnoreFileName, "*.generated.txt\ndocs/\n!keep.generated.txt\n")
	writeFile(t, root, "a.generated.txt", "x\n")
	writeFile(t, root, "keep.generated.txt", "x\n")
	writeFile(t, root, "docs/guide.md", "x\n")
	writeFile(t, root, "main.go", "x\n")

	res, err := Collect(root, Options{})
	require.NoError(t, err)

	require.Equal(t, []string{IgnoreFileName, "keep.generated.txt", "main.go"}, relPaths(res.Entries),
		"negation re-includes; last match wins")
	require.Equal(t, model.SkipIgnoreFile, skipByPath(res.Skips, "a.generated.txt").Reason)
	require.Equal(t, model.SkipIgnoreFile, skipByPath(res.Skips, "docs/").Reason)
}

func TestCollectIncludeExclude(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "x\n")
	writeFile(t, root, "b.md", "x\n")
	writeFile(t, root, "sub/c.go", "x\n")

	res, err := Collect(root, Options{Include: []string{"*.go"}})
	require.NoError(t, err)
	require.Equal(t, []string{"a.go", "sub/c.go"}, relPaths(res.Entries), "no-slash include matches at any depth")
	require.Equal(t, model.SkipNotIncluded, skipByPath(res.Skips, "b.md").Reason)

	res, err = Collect(root, Options{Exclude: []string{"sub"}})
	require.NoError(t, err)
	require.Equal(t, []string{"a.go", "b.md"}, relPaths(res.Entries))
	require.Equal(t, model.SkipExcludeFlag, skipByPath(res.Skips, "sub/").Reason)
}

func TestCollectMaxFileSize(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "small.txt", "ok\n")
	writeFile(t, root, "big.txt", strings.Repeat("x", 100))

	res, err := Collect(root, Options{MaxFileSize: 10})
	require.NoError(t, err)
	require.Equal(t, []string{"small.txt"}, relPaths(res.Entries))
	sk := skipByPath(res.Skips, "big.txt")
	require.Equal(t, model.SkipMaxFileSize, sk.Reason)
	require.Equal(t, int64(100), sk.Size, "skip records the size")
}

func TestCollectSelfOutput(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "out.md", "old output\n")
	writeFile(t, root, "main.go", "x\n")

	res, err := Collect(root, Options{OutputPath: filepath.Join(root, "out.md"), NoDefaultIgnore: true})
	require.NoError(t, err)
	require.Equal(t, []string{"main.go"}, relPaths(res.Entries))
	require.Equal(t, model.SkipSelfOutput, skipByPath(res.Skips, "out.md").Reason)
}

func TestCollectSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "real.txt", "x\n")
	linkPath := filepath.Join(root, "link.txt")
	if err := os.Symlink(filepath.Join(root, "real.txt"), linkPath); err != nil {
		t.Skipf("symlinks unavailable on this system: %v", err) // Windows without dev mode
	}

	res, err := Collect(root, Options{})
	require.NoError(t, err)
	require.Equal(t, []string{"real.txt"}, relPaths(res.Entries))
	require.Equal(t, model.SkipSymlink, skipByPath(res.Skips, "link.txt").Reason)
}

func TestCollectDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z.txt", "a.txt", "m/x.txt", "m/a.txt", "b.txt"} {
		writeFile(t, root, name, "x\n")
	}

	first, err := Collect(root, Options{})
	require.NoError(t, err)
	second, err := Collect(root, Options{})
	require.NoError(t, err)

	require.Equal(t, []string{"a.txt", "b.txt", "m/a.txt", "m/x.txt", "z.txt"}, relPaths(first.Entries))
	require.Equal(t, relPaths(first.Entries), relPaths(second.Entries))
}

func TestRulesetSemantics(t *testing.T) {
	rs, err := ParseRules(strings.NewReader(`
# comment
*.log
build/
!important.log
src/*.tmp
`), "test")
	require.NoError(t, err)

	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"app.log", false, true},
		{"deep/nested/app.log", false, true},
		{"important.log", false, false},      // negated
		{"deep/important.log", false, false}, // negation matches at any depth too
		{"build", true, true},                // dirOnly matches dirs
		{"build", false, false},              // ...but not files named build
		{"src/x.tmp", false, true},           // anchored pattern
		{"other/src/x.tmp", false, false},    // anchored: not at root -> no match
		{"src/nested/x.tmp", false, false},   // * does not cross /
	}
	for _, c := range cases {
		require.Equal(t, c.want, rs.Match(c.path, c.isDir), "path=%s isDir=%v", c.path, c.isDir)
	}
}
