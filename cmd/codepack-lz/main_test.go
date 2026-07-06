package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPackReadUnpackRoundTrip(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "README.md"), "# Demo\n\nhello\n")
	mustWrite(t, filepath.Join(root, "src", "main.go"), "package main\n\nfunc main() {}\n")
	mustWrite(t, filepath.Join(root, "node_modules", "leftpad", "index.js"), "module.exports = 1\n")
	mustWrite(t, filepath.Join(root, "old.codepack.txt"), "do not include me\n")

	pkg, envelope, err := packDirectory(root, PackOptions{
		Output:      filepath.Join(root, "pack.codepack.txt"),
		MaxFileSize: 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("packDirectory failed: %v", err)
	}
	if pkg.Stats.FileCount != 2 {
		t.Fatalf("expected 2 packed files, got %d", pkg.Stats.FileCount)
	}

	paths := []string{pkg.Files[0].Path, pkg.Files[1].Path}
	if !slices.Contains(paths, "README.md") || !slices.Contains(paths, "src/main.go") {
		t.Fatalf("unexpected packed paths: %v", paths)
	}

	envelopePath := filepath.Join(root, "pack.codepack.txt")
	if err := os.WriteFile(envelopePath, []byte(envelope), 0644); err != nil {
		t.Fatalf("write envelope: %v", err)
	}

	decoded, err := readPackage(envelopePath)
	if err != nil {
		t.Fatalf("readPackage failed: %v", err)
	}

	outDir := filepath.Join(root, "restored")
	if err := unpackPackage(decoded, outDir); err != nil {
		t.Fatalf("unpackPackage failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "src", "main.go"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(got) != "package main\n\nfunc main() {}\n" {
		t.Fatalf("unexpected restored content: %q", string(got))
	}
}

func TestInterspersedArgs(t *testing.T) {
	got := interspersedArgs(
		[]string{"input.codepack.txt", "-o", "restored"},
		nil,
		map[string]bool{"o": true},
	)
	want := []string{"-o", "restored", "input.codepack.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
