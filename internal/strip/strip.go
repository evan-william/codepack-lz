// Package strip removes comments from source text as a cheap token
// reduction for the readable output formats.
//
// This is a heuristic, extension-driven stripper, not a parser. Known
// limitations (documented in the README): regex literals containing "/*" in
// JavaScript can be misread as block comments, and hash-language stripping
// removes full-line comments only, so trailing `x = 1  # note` comments stay.
// Structural pruning now lives behind the Pruner interface in internal/prune.
// Stripping is never applied to the lossless envelope format.
package strip

import "strings"

type mode int

const (
	modeNone      mode = iota
	modeSlash          // // line + /* block */ with string awareness
	modeBlockOnly      // /* block */ only (CSS: "//" is not a comment there)
	modeHash           // full lines starting with #
)

var extModes = map[string]mode{
	".go": modeSlash, ".js": modeSlash, ".jsx": modeSlash, ".mjs": modeSlash,
	".cjs": modeSlash, ".ts": modeSlash, ".tsx": modeSlash, ".java": modeSlash,
	".c": modeSlash, ".h": modeSlash, ".cc": modeSlash, ".cpp": modeSlash,
	".hpp": modeSlash, ".cs": modeSlash, ".rs": modeSlash, ".kt": modeSlash,
	".kts": modeSlash, ".swift": modeSlash, ".dart": modeSlash, ".scala": modeSlash,
	".proto": modeSlash, ".scss": modeSlash, ".less": modeSlash,
	".css": modeBlockOnly,
	".py":  modeHash, ".rb": modeHash, ".sh": modeHash, ".bash": modeHash,
	".zsh": modeHash, ".fish": modeHash, ".ps1": modeHash, ".yml": modeHash,
	".yaml": modeHash, ".toml": modeHash, ".r": modeHash, ".pl": modeHash,
}

// Comments strips comments from content based on the file extension (with
// leading dot, lowercase). Unknown extensions are returned unchanged.
func Comments(content []byte, ext string) []byte {
	switch extModes[strings.ToLower(ext)] {
	case modeSlash:
		return tidy(stripSlash(string(content), true))
	case modeBlockOnly:
		return tidy(stripSlash(string(content), false))
	case modeHash:
		return tidy(stripHash(string(content)))
	default:
		return content
	}
}

// stripSlash removes /* block */ comments and, when lineComments is set,
// // line comments -- while respecting single, double, and backtick strings.
func stripSlash(content string, lineComments bool) string {
	var out strings.Builder
	out.Grow(len(content))
	var (
		inString bool
		quote    byte
		escaped  bool
		inLine   bool
		inBlock  bool
	)
	for i := 0; i < len(content); i++ {
		ch := content[i]
		var next byte
		if i+1 < len(content) {
			next = content[i+1]
		}

		switch {
		case inLine:
			if ch == '\n' {
				inLine = false
				out.WriteByte(ch)
			}
		case inBlock:
			if ch == '*' && next == '/' {
				inBlock = false
				i++
			} else if ch == '\n' {
				out.WriteByte(ch) // keep line numbers roughly stable
			}
		case inString:
			out.WriteByte(ch)
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == quote:
				inString = false
			}
		case ch == '"' || ch == '\'' || ch == '`':
			inString = true
			quote = ch
			out.WriteByte(ch)
		case lineComments && ch == '/' && next == '/':
			inLine = true
			i++
		case ch == '/' && next == '*':
			inBlock = true
			i++
		default:
			out.WriteByte(ch)
		}
	}
	return out.String()
}

// stripHash blanks lines whose first non-space character is '#', except
// shebang lines (#!...) on the first line.
func stripHash(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		if i == 0 && strings.HasPrefix(trimmed, "#!") {
			continue
		}
		lines[i] = ""
	}
	return strings.Join(lines, "\n")
}

// tidy trims trailing whitespace stripping left behind and collapses runs of
// more than two blank lines. Only readable output passes through here, so
// this cleanup is lossy by request, never part of the envelope.
func tidy(content string) []byte {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			blanks++
			if blanks > 2 {
				continue
			}
		} else {
			blanks = 0
		}
		out = append(out, line)
	}
	s := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if s != "" {
		s += "\n"
	}
	return []byte(s)
}
