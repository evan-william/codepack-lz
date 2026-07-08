// Package secret detects hardcoded credentials in file content before it is
// packed, using an embedded ruleset (regex + keyword prefilter + Shannon
// entropy) adapted from the gitleaks default rules (MIT).
//
// Scanning is a risk reducer, not a guarantee: novel or oddly formatted
// secrets can pass. The docs state this loudly; see docs/security.md.
package secret

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

//go:embed rules.json
var rulesJSON []byte

// AllowMarker suppresses findings on the line that contains it.
const AllowMarker = "codepack:allow"

// Finding is one detected secret. Start/End are byte offsets of the secret
// within the file content; Preview is safe to display (never the full secret).
type Finding struct {
	RuleID  string
	Path    string
	Line    int // 1-based
	Start   int
	End     int
	Preview string
}

type ruleSpec struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Regex       string   `json:"regex"`
	SecretGroup int      `json:"secret_group"`
	Entropy     float64  `json:"entropy"`
	Keywords    []string `json:"keywords"`
}

type rulesFile struct {
	Allowlist []string   `json:"allowlist"`
	Rules     []ruleSpec `json:"rules"`
}

type compiledRule struct {
	ruleSpec
	re *regexp.Regexp
}

// Scanner holds the compiled ruleset. Safe for concurrent use.
type Scanner struct {
	rules []compiledRule
	allow []*regexp.Regexp
}

// Load compiles the embedded ruleset.
func Load() (*Scanner, error) {
	var rf rulesFile
	if err := json.Unmarshal(rulesJSON, &rf); err != nil {
		return nil, fmt.Errorf("parse embedded secret rules: %w", err)
	}
	s := &Scanner{}
	for _, spec := range rf.Rules {
		re, err := regexp.Compile(spec.Regex)
		if err != nil {
			return nil, fmt.Errorf("rule %s: compile regex: %w", spec.ID, err)
		}
		if spec.SecretGroup > re.NumSubexp() {
			return nil, fmt.Errorf("rule %s: secret_group %d exceeds groups %d", spec.ID, spec.SecretGroup, re.NumSubexp())
		}
		s.rules = append(s.rules, compiledRule{ruleSpec: spec, re: re})
	}
	for i, pat := range rf.Allowlist {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("allowlist[%d]: compile regex: %w", i, err)
		}
		s.allow = append(s.allow, re)
	}
	return s, nil
}

// Scan returns all findings in content, sorted by Start offset.
func (s *Scanner) Scan(path string, content []byte) []Finding {
	lower := strings.ToLower(string(content))
	var findings []Finding
	for i := range s.rules {
		rule := &s.rules[i]
		if !rule.prefilter(lower) {
			continue
		}
		for _, m := range rule.re.FindAllSubmatchIndex(content, -1) {
			g := rule.SecretGroup * 2
			if g+1 >= len(m) || m[g] < 0 {
				continue
			}
			start, end := m[g], m[g+1]
			sec := content[start:end]
			if s.allowlisted(sec) || suppressed(content, start) {
				continue
			}
			if rule.Entropy > 0 && shannonEntropy(sec) < rule.Entropy {
				continue
			}
			findings = append(findings, Finding{
				RuleID:  rule.ID,
				Path:    path,
				Line:    1 + bytes.Count(content[:start], []byte{'\n'}),
				Start:   start,
				End:     end,
				Preview: preview(sec),
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Start != findings[j].Start {
			return findings[i].Start < findings[j].Start
		}
		return findings[i].RuleID < findings[j].RuleID
	})
	return findings
}

// Redact replaces every finding's secret bytes with "[REDACTED:<rule-id>]".
// Overlapping ranges are merged (the first rule's ID wins). Returns the new
// content and the number of replacements.
func Redact(content []byte, findings []Finding) ([]byte, int) {
	if len(findings) == 0 {
		return content, 0
	}
	type span struct {
		start, end int
		rule       string
	}
	spans := make([]span, 0, len(findings))
	for _, f := range findings {
		spans = append(spans, span{f.Start, f.End, f.RuleID})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	merged := spans[:1]
	for _, sp := range spans[1:] {
		last := &merged[len(merged)-1]
		if sp.start < last.end { // overlap: extend, keep first rule id
			if sp.end > last.end {
				last.end = sp.end
			}
			continue
		}
		merged = append(merged, sp)
	}

	var out bytes.Buffer
	out.Grow(len(content))
	prev := 0
	for _, sp := range merged {
		out.Write(content[prev:sp.start])
		out.WriteString("[REDACTED:" + sp.rule + "]")
		prev = sp.end
	}
	out.Write(content[prev:])
	return out.Bytes(), len(merged)
}

func (r *compiledRule) prefilter(lower string) bool {
	if len(r.Keywords) == 0 {
		return true
	}
	for _, kw := range r.Keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func (s *Scanner) allowlisted(secret []byte) bool {
	for _, re := range s.allow {
		if re.Match(secret) {
			return true
		}
	}
	return false
}

// suppressed reports whether the line containing offset carries AllowMarker.
func suppressed(content []byte, offset int) bool {
	lineStart := bytes.LastIndexByte(content[:offset], '\n') + 1
	lineEnd := bytes.IndexByte(content[offset:], '\n')
	if lineEnd < 0 {
		lineEnd = len(content)
	} else {
		lineEnd += offset
	}
	return bytes.Contains(content[lineStart:lineEnd], []byte(AllowMarker))
}

func shannonEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}
	var freq [256]int
	for _, b := range data {
		freq[b]++
	}
	entropy := 0.0
	n := float64(len(data))
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		entropy -= p * math.Log2(p)
	}
	return entropy
}

// preview renders the first few characters of a secret for reports without
// exposing the secret itself.
func preview(secret []byte) string {
	const keep = 4
	if len(secret) <= keep {
		return string(secret) + "..."
	}
	return string(secret[:keep]) + "..."
}
