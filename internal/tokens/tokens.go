// Package tokens estimates LLM token counts for packed files.
//
// The default counter is a small local heuristic, not a provider tokenizer.
// That keeps the binary dependency-light and works fully offline. No public
// offline tokenizer exists for Anthropic models, so every local count is an
// approximation for any model -- callers must present these numbers as
// estimates, never as exact. Provider-backed counters can report exact counts
// when the user explicitly opts in.
package tokens

import (
	"unicode"
	"unicode/utf8"
)

// Counter counts tokens in text. Implementations must be safe for concurrent
// use by multiple goroutines.
type Counter interface {
	// Name identifies the estimator, e.g. "heuristic".
	Name() string
	// Estimate reports whether counts are approximations (true for every
	// local estimator; false only for provider-exact API counts).
	Estimate() bool
	// Count returns the token count of text.
	Count(text []byte) (int, error)
}

type heuristicCounter struct{}

// NewEstimator returns the default offline counter.
func NewEstimator() (Counter, error) {
	return heuristicCounter{}, nil
}

func (heuristicCounter) Name() string   { return "heuristic" }
func (heuristicCounter) Estimate() bool { return true }

func (heuristicCounter) Count(text []byte) (int, error) {
	tokens := 0
	wordBytes := 0
	flushWord := func() {
		if wordBytes == 0 {
			return
		}
		// A rough BPE-like heuristic: long identifiers/words usually split.
		tokens += (wordBytes + 3) / 4
		wordBytes = 0
	}

	for len(text) > 0 {
		r, size := utf8.DecodeRune(text)
		if r == utf8.RuneError && size == 0 {
			break
		}
		text = text[size:]

		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' {
			wordBytes += size
			continue
		}
		flushWord()
		if !unicode.IsSpace(r) {
			tokens++
		}
	}
	flushWord()
	return tokens, nil
}
