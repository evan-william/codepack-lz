package prune

import (
	"bytes"
	"strings"
	"unicode"
)

// Heuristic is a dependency-free structural pruner. It is intentionally
// conservative: unsupported languages are returned unchanged, and supported
// languages keep imports, declarations, and signatures while replacing bodies
// with a small placeholder.
type Heuristic struct{}

func NewHeuristic() Heuristic { return Heuristic{} }

func (Heuristic) Name() string { return "heuristic-structure" }

func (Heuristic) Prune(content []byte, lang string) ([]byte, error) {
	switch lang {
	case "go", "javascript", "typescript", "tsx":
		return pruneBraceLanguage(content), nil
	case "python":
		return prunePython(content), nil
	default:
		return content, nil
	}
}

func pruneBraceLanguage(content []byte) []byte {
	lines := splitLines(content)
	var out bytes.Buffer
	skipping := false
	depth := 0
	wrotePlaceholder := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !skipping {
			if startsBody(trimmed) {
				out.WriteString(line)
				if !strings.HasSuffix(line, "\n") {
					out.WriteByte('\n')
				}
				out.WriteString(indentOf(line))
				out.WriteString("\t...\n")
				depth = braceDelta(line)
				if depth <= 0 {
					continue
				}
				skipping = true
				wrotePlaceholder = true
				continue
			}
			out.WriteString(line)
			continue
		}

		depth += braceDelta(line)
		if depth <= 0 {
			out.WriteString(indentOf(line))
			out.WriteString("}\n")
			skipping = false
			wrotePlaceholder = false
			continue
		}
		if !wrotePlaceholder && trimmed != "" {
			out.WriteString(indentOf(line))
			out.WriteString("...\n")
			wrotePlaceholder = true
		}
	}
	if skipping {
		out.WriteString("}\n")
	}
	if out.Len() >= len(content) {
		return content
	}
	return out.Bytes()
}

func startsBody(trimmed string) bool {
	if !strings.Contains(trimmed, "{") {
		return false
	}
	switch {
	case strings.HasPrefix(trimmed, "func "):
		return true
	case strings.HasPrefix(trimmed, "function "):
		return true
	case strings.HasPrefix(trimmed, "async function "):
		return true
	case strings.HasPrefix(trimmed, "class "):
		return true
	case strings.HasPrefix(trimmed, "export function "):
		return true
	case strings.HasPrefix(trimmed, "export async function "):
		return true
	case strings.HasPrefix(trimmed, "export class "):
		return true
	default:
		return looksLikeMethod(trimmed)
	}
}

func looksLikeMethod(trimmed string) bool {
	if strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "for ") ||
		strings.HasPrefix(trimmed, "switch ") || strings.HasPrefix(trimmed, "while ") {
		return false
	}
	open := strings.IndexByte(trimmed, '(')
	brace := strings.IndexByte(trimmed, '{')
	return open > 0 && brace > open && !strings.HasPrefix(trimmed, "//")
}

func braceDelta(line string) int {
	delta := 0
	inString := rune(0)
	escaped := false
	for _, r := range line {
		if inString != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == inString {
				inString = 0
			}
			continue
		}
		switch r {
		case '"', '\'', '`':
			inString = r
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}

func prunePython(content []byte) []byte {
	lines := splitLines(content)
	var out bytes.Buffer
	skipIndent := -1

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := leadingSpaces(line)
		if skipIndent >= 0 {
			if trimmed == "" {
				continue
			}
			if indent > skipIndent {
				continue
			}
			skipIndent = -1
		}
		if strings.HasPrefix(trimmed, "class ") {
			out.WriteString(line)
			continue
		}
		if strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "async def ") {
			out.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				out.WriteByte('\n')
			}
			out.WriteString(strings.Repeat(" ", indent+4))
			out.WriteString("...\n")
			skipIndent = indent
			continue
		}
		out.WriteString(line)
	}
	if out.Len() >= len(content) {
		return content
	}
	return out.Bytes()
}

func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	raw := strings.SplitAfter(string(content), "\n")
	if raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	return raw
}

func indentOf(line string) string {
	for i, r := range line {
		if !unicode.IsSpace(r) || r == '\n' || r == '\r' {
			return line[:i]
		}
	}
	return line
}

func leadingSpaces(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}
