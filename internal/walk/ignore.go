package walk

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Ruleset is an ordered list of ignore rules using a documented subset of
// gitignore semantics (see docs/format-spec.md):
//
//   - blank lines and lines starting with "#" are skipped
//   - a trailing "/" makes the rule match directories only
//   - a leading "!" negates the rule; the LAST matching rule wins
//   - a pattern containing "/" is anchored to the root and matched against
//     the full slash-separated relative path with doublestar globs
//   - a pattern without "/" matches at any depth (equivalent to "**/pattern")
//
// Like gitignore, a file inside an ignored (pruned) directory cannot be
// re-included by a negation.
type Ruleset struct {
	rules []rule
}

type rule struct {
	pattern string
	negate  bool
	dirOnly bool
}

// ParseRules reads one rule per line. src names the origin for error messages.
func ParseRules(r io.Reader, src string) (*Ruleset, error) {
	rs := &Ruleset{}
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ru := rule{}
		if strings.HasPrefix(line, "!") {
			ru.negate = true
			line = strings.TrimPrefix(line, "!")
		}
		if strings.HasSuffix(line, "/") {
			ru.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		line = strings.TrimPrefix(line, "/") // leading slash means root-anchored; path is already relative
		if line == "" {
			continue
		}
		if !strings.Contains(line, "/") {
			line = "**/" + line // match at any depth
		}
		if !doublestar.ValidatePattern(line) {
			return nil, fmt.Errorf("%s:%d: invalid pattern %q", src, lineNo, line)
		}
		ru.pattern = line
		rs.rules = append(rs.rules, ru)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", src, err)
	}
	return rs, nil
}

// Match reports whether relPath (slash-separated, relative to the root) is
// ignored by the ruleset. The last matching rule wins.
func (rs *Ruleset) Match(relPath string, isDir bool) bool {
	ignored := false
	for _, ru := range rs.rules {
		if ru.dirOnly && !isDir {
			continue
		}
		ok, err := doublestar.Match(ru.pattern, relPath)
		if err != nil || !ok {
			continue
		}
		ignored = !ru.negate
	}
	return ignored
}

// Empty reports whether the ruleset has no rules.
func (rs *Ruleset) Empty() bool { return len(rs.rules) == 0 }

// globs is a plain doublestar glob list used for --include / --exclude.
// A pattern without "/" matches at any depth, mirroring Ruleset convenience.
type globs []string

func compileGlobs(patterns []string, flag string) (globs, error) {
	out := make(globs, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			p = "**/" + p
		}
		if !doublestar.ValidatePattern(p) {
			return nil, fmt.Errorf("%s: invalid glob %q", flag, p)
		}
		out = append(out, p)
	}
	return out, nil
}

func (g globs) match(relPath string) bool {
	for _, p := range g {
		if ok, _ := doublestar.Match(p, relPath); ok {
			return true
		}
	}
	return false
}

// matchDir reports whether a directory should be treated as matched by the
// glob list: either the directory path itself matches, or a pattern is scoped
// entirely inside it (so pruning would be wrong). Only exact directory matches
// prune; this helper matches the directory path itself.
func (g globs) matchDir(relPath string) bool {
	for _, p := range g {
		if ok, _ := doublestar.Match(p, relPath); ok {
			return true
		}
		if ok, _ := doublestar.Match(strings.TrimSuffix(p, "/**"), relPath); ok {
			return true
		}
	}
	return false
}
