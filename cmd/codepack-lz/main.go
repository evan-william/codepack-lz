package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	appName      = "codepack-lz"
	version      = "0.1.0"
	formatName   = "CODEPACK-LZ"
	formatVer    = "0.1"
	beginMarker  = "---BEGIN CODEPACK-LZ---"
	endMarker    = "---END CODEPACK-LZ---"
	defaultOut   = "codepack-lz-output.codepack.txt"
	maxBase64Col = 80
)

var ignoredDirs = map[string]bool{
	".git":          true,
	".hg":           true,
	".svn":          true,
	".next":         true,
	".nuxt":         true,
	".turbo":        true,
	".vite":         true,
	".cache":        true,
	"node_modules":  true,
	"vendor":        true,
	"dist":          true,
	"build":         true,
	"out":           true,
	"target":        true,
	"coverage":      true,
	"__pycache__":   true,
	".pytest_cache": true,
	".mypy_cache":   true,
	".gradle":       true,
	".idea":         true,
	"bin":           true,
	"obj":           true,
	"tmp":           true,
	"temp":          true,
	"logs":          true,
	".codex":        true,
	".agents":       true,
}

var ignoredExtensions = map[string]bool{
	".7z":    true,
	".a":     true,
	".avi":   true,
	".bin":   true,
	".bmp":   true,
	".class": true,
	".dll":   true,
	".dylib": true,
	".exe":   true,
	".gif":   true,
	".ico":   true,
	".jar":   true,
	".jpeg":  true,
	".jpg":   true,
	".lock":  true,
	".mov":   true,
	".mp3":   true,
	".mp4":   true,
	".o":     true,
	".obj":   true,
	".pdf":   true,
	".png":   true,
	".so":    true,
	".tar":   true,
	".ttf":   true,
	".wasm":  true,
	".webm":  true,
	".woff":  true,
	".woff2": true,
	".zip":   true,
}

type Package struct {
	Format      string        `json:"format"`
	Version     string        `json:"version"`
	ToolVersion string        `json:"toolVersion"`
	GeneratedAt string        `json:"generatedAt"`
	RootName    string        `json:"rootName"`
	Files       []PackedFile  `json:"files"`
	Skipped     []SkippedFile `json:"skipped,omitempty"`
	Stats       PackStats     `json:"stats"`
}

type PackedFile struct {
	Path     string `json:"path"`
	Language string `json:"language,omitempty"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
	Content  string `json:"content"`
}

type SkippedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type PackStats struct {
	FileCount       int   `json:"fileCount"`
	SkippedCount    int   `json:"skippedCount"`
	OriginalBytes   int64 `json:"originalBytes"`
	StoredTextBytes int64 `json:"storedTextBytes"`
	JSONBytes       int64 `json:"jsonBytes"`
	CompressedBytes int64 `json:"compressedBytes"`
	Base64Bytes     int64 `json:"base64Bytes"`
}

type PackOptions struct {
	Output        string
	MaxFileSize   int64
	IncludeHidden bool
	PruneComments bool
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "pack":
		err = runPack(os.Args[2:])
	case "unpack":
		err = runUnpack(os.Args[2:])
	case "stats":
		err = runStats(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("%s %s\n", appName, version)
		return
	case "help", "--help", "-h":
		printUsage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runPack(args []string) error {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts PackOptions
	fs.StringVar(&opts.Output, "o", defaultOut, "output envelope path")
	fs.Int64Var(&opts.MaxFileSize, "max-file-size", 1024*1024, "maximum file size to include, in bytes")
	fs.BoolVar(&opts.IncludeHidden, "include-hidden", false, "include hidden files and directories")
	fs.BoolVar(&opts.PruneComments, "prune-comments", false, "strip common comment forms from supported text files")

	if err := fs.Parse(interspersedArgs(args, map[string]bool{
		"include-hidden": true,
		"prune-comments": true,
	}, map[string]bool{
		"o":             true,
		"max-file-size": true,
	})); err != nil {
		printPackUsage()
		return err
	}

	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}

	pkg, envelope, err := packDirectory(root, opts)
	if err != nil {
		return err
	}

	if err := os.WriteFile(opts.Output, []byte(envelope), 0644); err != nil {
		return err
	}

	ratio := compressionRatio(pkg.Stats.OriginalBytes, pkg.Stats.CompressedBytes)
	fmt.Printf("packed %d files into %s\n", pkg.Stats.FileCount, opts.Output)
	fmt.Printf("original=%s compressed=%s ratio=%.1f%% skipped=%d\n",
		humanBytes(pkg.Stats.OriginalBytes),
		humanBytes(pkg.Stats.CompressedBytes),
		ratio,
		pkg.Stats.SkippedCount,
	)
	return nil
}

func runUnpack(args []string) error {
	fs := flag.NewFlagSet("unpack", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	outDir := "codepack-lz-unpacked"
	fs.StringVar(&outDir, "o", outDir, "output directory")

	if err := fs.Parse(interspersedArgs(args, nil, map[string]bool{
		"o": true,
	})); err != nil {
		printUnpackUsage()
		return err
	}
	if fs.NArg() < 1 {
		printUnpackUsage()
		return errors.New("missing input envelope")
	}

	pkg, err := readPackage(fs.Arg(0))
	if err != nil {
		return err
	}
	if err := unpackPackage(pkg, outDir); err != nil {
		return err
	}
	fmt.Printf("unpacked %d files into %s\n", len(pkg.Files), outDir)
	return nil
}

func runStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		printStatsUsage()
		return err
	}
	if fs.NArg() < 1 {
		printStatsUsage()
		return errors.New("missing input envelope")
	}

	pkg, err := readPackage(fs.Arg(0))
	if err != nil {
		return err
	}

	fmt.Printf("format: %s v%s\n", pkg.Format, pkg.Version)
	fmt.Printf("tool: %s\n", pkg.ToolVersion)
	fmt.Printf("generated: %s\n", pkg.GeneratedAt)
	fmt.Printf("root: %s\n", pkg.RootName)
	fmt.Printf("files: %d\n", pkg.Stats.FileCount)
	fmt.Printf("skipped: %d\n", pkg.Stats.SkippedCount)
	fmt.Printf("original: %s\n", humanBytes(pkg.Stats.OriginalBytes))
	fmt.Printf("json: %s\n", humanBytes(pkg.Stats.JSONBytes))
	fmt.Printf("compressed: %s\n", humanBytes(pkg.Stats.CompressedBytes))
	fmt.Printf("base64: %s\n", humanBytes(pkg.Stats.Base64Bytes))
	fmt.Printf("compression ratio: %.1f%%\n", compressionRatio(pkg.Stats.OriginalBytes, pkg.Stats.CompressedBytes))
	return nil
}

func packDirectory(root string, opts PackOptions) (Package, string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Package{}, "", err
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return Package{}, "", err
	}
	if !info.IsDir() {
		return Package{}, "", fmt.Errorf("%s is not a directory", root)
	}

	pkg := Package{
		Format:      formatName,
		Version:     formatVer,
		ToolVersion: version,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		RootName:    filepath.Base(absRoot),
	}

	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == absRoot {
			return nil
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := entry.Name()

		if entry.IsDir() {
			if shouldSkipDir(name, opts.IncludeHidden) {
				pkg.Skipped = append(pkg.Skipped, SkippedFile{Path: rel + "/", Reason: "ignored directory"})
				return filepath.SkipDir
			}
			return nil
		}

		if shouldSkipFile(name, opts.IncludeHidden) {
			pkg.Skipped = append(pkg.Skipped, SkippedFile{Path: rel, Reason: "ignored file"})
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > opts.MaxFileSize {
			pkg.Skipped = append(pkg.Skipped, SkippedFile{Path: rel, Reason: "larger than max-file-size"})
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if isLikelyBinary(data) {
			pkg.Skipped = append(pkg.Skipped, SkippedFile{Path: rel, Reason: "binary file"})
			return nil
		}

		normalized := normalizeText(string(data))
		if opts.PruneComments {
			normalized = pruneComments(normalized, filepath.Ext(name))
		}

		sum := sha256.Sum256(data)
		pkg.Files = append(pkg.Files, PackedFile{
			Path:     rel,
			Language: languageForPath(name),
			Bytes:    info.Size(),
			SHA256:   hex.EncodeToString(sum[:]),
			Content:  normalized,
		})
		pkg.Stats.OriginalBytes += info.Size()
		pkg.Stats.StoredTextBytes += int64(len(normalized))
		return nil
	})
	if err != nil {
		return Package{}, "", err
	}

	sort.Slice(pkg.Files, func(i, j int) bool {
		return pkg.Files[i].Path < pkg.Files[j].Path
	})
	sort.Slice(pkg.Skipped, func(i, j int) bool {
		return pkg.Skipped[i].Path < pkg.Skipped[j].Path
	})

	pkg.Stats.FileCount = len(pkg.Files)
	pkg.Stats.SkippedCount = len(pkg.Skipped)

	encoded := ""
	for i := 0; i < 5; i++ {
		payload, err := json.Marshal(pkg)
		if err != nil {
			return Package{}, "", err
		}
		compressed, err := gzipBytes(payload)
		if err != nil {
			return Package{}, "", err
		}
		encoded = base64.StdEncoding.EncodeToString(compressed)

		nextStats := pkg.Stats
		nextStats.JSONBytes = int64(len(payload))
		nextStats.CompressedBytes = int64(len(compressed))
		nextStats.Base64Bytes = int64(len(encoded))
		if nextStats == pkg.Stats {
			break
		}
		pkg.Stats = nextStats
	}

	return pkg, buildEnvelope(encoded), nil
}

func unpackPackage(pkg Package, outDir string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	outAbs, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}

	for _, file := range pkg.Files {
		cleanPath := filepath.Clean(filepath.FromSlash(file.Path))
		if cleanPath == "." || strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
			return fmt.Errorf("unsafe path in pack: %s", file.Path)
		}

		target := filepath.Join(outAbs, cleanPath)
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(targetAbs, outAbs+string(os.PathSeparator)) && targetAbs != outAbs {
			return fmt.Errorf("unsafe path in pack: %s", file.Path)
		}

		if err := os.MkdirAll(filepath.Dir(targetAbs), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(targetAbs, []byte(file.Content), 0644); err != nil {
			return err
		}
	}
	return nil
}

func readPackage(path string) (Package, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Package{}, err
	}

	encoded, err := extractBase64(string(raw))
	if err != nil {
		return Package{}, err
	}

	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Package{}, err
	}

	payload, err := gunzipBytes(compressed)
	if err != nil {
		return Package{}, err
	}

	var pkg Package
	if err := json.Unmarshal(payload, &pkg); err != nil {
		return Package{}, err
	}
	if pkg.Format != formatName {
		return Package{}, fmt.Errorf("unsupported format %q", pkg.Format)
	}
	return pkg, nil
}

func shouldSkipDir(name string, includeHidden bool) bool {
	if ignoredDirs[name] {
		return true
	}
	return !includeHidden && isHidden(name)
}

func shouldSkipFile(name string, includeHidden bool) bool {
	if !includeHidden && isHidden(name) {
		return true
	}
	if name == defaultOut || strings.HasSuffix(name, ".codepack.txt") {
		return true
	}
	if ignoredExtensions[strings.ToLower(filepath.Ext(name))] {
		return true
	}
	if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".tmp") {
		return true
	}
	return false
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

func isLikelyBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.Contains(data, []byte{0}) {
		return true
	}
	return !utf8.Valid(data)
}

func normalizeText(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			blankRun++
			if blankRun > 2 {
				continue
			}
		} else {
			blankRun = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n")) + "\n"
}

func pruneComments(content string, ext string) string {
	ext = strings.ToLower(ext)
	switch ext {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".java", ".c", ".cc", ".cpp", ".cs", ".css", ".scss", ".rs", ".kt", ".swift":
		return stripSlashComments(content)
	case ".py", ".rb", ".sh", ".bash", ".zsh", ".ps1", ".yml", ".yaml", ".toml":
		return stripHashComments(content)
	default:
		return content
	}
}

func stripHashComments(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			lines[i] = ""
		}
	}
	return normalizeText(strings.Join(lines, "\n"))
}

func stripSlashComments(content string) string {
	var out strings.Builder
	inString := false
	stringQuote := rune(0)
	escaped := false
	inLineComment := false
	inBlockComment := false
	runes := []rune(content)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				out.WriteRune(ch)
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			out.WriteRune(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == stringQuote {
				inString = false
			}
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = true
			stringQuote = ch
			out.WriteRune(ch)
			continue
		}

		if ch == '/' && next == '/' {
			inLineComment = true
			i++
			continue
		}
		if ch == '/' && next == '*' {
			inBlockComment = true
			i++
			continue
		}

		out.WriteRune(ch)
	}

	return normalizeText(out.String())
}

func languageForPath(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".json":
		return "json"
	case ".md", ".markdown":
		return "markdown"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".html":
		return "html"
	case ".css", ".scss":
		return "css"
	case ".yml", ".yaml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".ps1":
		return "powershell"
	default:
		return strings.TrimPrefix(ext, ".")
	}
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipBytes(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func buildEnvelope(encoded string) string {
	var b strings.Builder
	b.WriteString(formatName + " v" + formatVer + "\n")
	b.WriteString("encoding: gzip+base64\n")
	b.WriteString("decoder: run `codepack-lz unpack <file> -o <directory>` to restore the payload.\n")
	b.WriteString("note: payload is compressed JSON containing repository paths, metadata, and text content.\n\n")
	b.WriteString(beginMarker + "\n")
	for len(encoded) > maxBase64Col {
		b.WriteString(encoded[:maxBase64Col] + "\n")
		encoded = encoded[maxBase64Col:]
	}
	if encoded != "" {
		b.WriteString(encoded + "\n")
	}
	b.WriteString(endMarker + "\n")
	return b.String()
}

func extractBase64(envelope string) (string, error) {
	start := strings.Index(envelope, beginMarker)
	end := strings.Index(envelope, endMarker)
	if start == -1 || end == -1 || end <= start {
		return "", errors.New("invalid CodePack-LZ envelope markers")
	}

	body := envelope[start+len(beginMarker) : end]
	fields := strings.Fields(body)
	return strings.Join(fields, ""), nil
}

func compressionRatio(original, compressed int64) float64 {
	if original <= 0 {
		return 0
	}
	return float64(compressed) / float64(original) * 100
}

func humanBytes(size int64) string {
	units := []string{"B", "KB", "MB", "GB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", size, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func printUsage() {
	fmt.Println(`CodePack-LZ - compressed codebase context packing

Usage:
  codepack-lz pack [directory] [flags]
  codepack-lz unpack <file> -o <directory>
  codepack-lz stats <file>
  codepack-lz version`)
}

func interspersedArgs(args []string, boolFlags map[string]bool, valueFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}

		name := strings.TrimLeft(arg, "-")
		if eq := strings.Index(name, "="); eq >= 0 {
			name = name[:eq]
		}

		if boolFlags[name] {
			flags = append(flags, arg)
			continue
		}
		if valueFlags[name] {
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}

		flags = append(flags, arg)
	}

	return append(flags, positionals...)
}

func printPackUsage() {
	fmt.Println(`Usage:
  codepack-lz pack [directory] -o codepack-lz-output.codepack.txt

Flags:
  -o string
        output envelope path
  --max-file-size int
        maximum file size to include, in bytes
  --include-hidden
        include hidden files and directories
  --prune-comments
        strip common comment forms from supported text files`)
}

func printUnpackUsage() {
	fmt.Println(`Usage:
  codepack-lz unpack <file> -o restored-project`)
}

func printStatsUsage() {
	fmt.Println(`Usage:
  codepack-lz stats <file>`)
}
