package pack

import (
	"path"
	"strings"
)

// langByExt maps lowercase extensions to language hints used for fenced code
// blocks and metadata. Unknown extensions fall back to the bare extension.
var langByExt = map[string]string{
	".go": "go", ".js": "javascript", ".jsx": "jsx", ".mjs": "javascript",
	".cjs": "javascript", ".ts": "typescript", ".tsx": "tsx",
	".json": "json", ".jsonc": "jsonc", ".md": "markdown", ".markdown": "markdown",
	".py": "python", ".rb": "ruby", ".java": "java", ".rs": "rust",
	".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".hpp": "cpp",
	".cs": "csharp", ".kt": "kotlin", ".kts": "kotlin", ".swift": "swift",
	".dart": "dart", ".scala": "scala", ".php": "php", ".lua": "lua",
	".html": "html", ".htm": "html", ".xml": "xml", ".svg": "xml",
	".css": "css", ".scss": "scss", ".less": "less",
	".yml": "yaml", ".yaml": "yaml", ".toml": "toml", ".ini": "ini",
	".sh": "bash", ".bash": "bash", ".zsh": "zsh", ".fish": "fish",
	".ps1": "powershell", ".psm1": "powershell", ".bat": "batch", ".cmd": "batch",
	".sql": "sql", ".graphql": "graphql", ".proto": "protobuf",
	".tf": "hcl", ".hcl": "hcl", ".dockerfile": "dockerfile",
	".r": "r", ".pl": "perl", ".ex": "elixir", ".exs": "elixir",
	".erl": "erlang", ".hs": "haskell", ".ml": "ocaml", ".zig": "zig",
	".vue": "vue", ".svelte": "svelte", ".astro": "astro",
	".txt": "text", ".csv": "csv", ".tsv": "tsv",
}

// langByName maps well-known extensionless filenames.
var langByName = map[string]string{
	"dockerfile": "dockerfile", "makefile": "makefile", "gnumakefile": "makefile",
	"rakefile": "ruby", "gemfile": "ruby", "vagrantfile": "ruby",
	"jenkinsfile": "groovy", "cmakelists.txt": "cmake",
}

// langForPath returns the language hint for a slash-separated relative path.
func langForPath(relPath string) string {
	base := strings.ToLower(path.Base(relPath))
	if lang, ok := langByName[base]; ok {
		return lang
	}
	ext := strings.ToLower(path.Ext(relPath))
	if lang, ok := langByExt[ext]; ok {
		return lang
	}
	return strings.TrimPrefix(ext, ".")
}

// extForPath returns the lowercase extension (with dot) for strip decisions.
func extForPath(relPath string) string {
	return strings.ToLower(path.Ext(relPath))
}
