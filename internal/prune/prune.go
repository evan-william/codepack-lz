// Package prune provides source compression for readable output: keep
// declarations, signatures, imports, and other high-signal structure while
// dropping implementation bodies that usually spend the most tokens.
package prune

// Pruner reduces source content to its structural skeleton. Implementations
// must be safe for concurrent use and must return the input unchanged for
// languages they do not support.
type Pruner interface {
	// Name identifies the backend, e.g. "gotreesitter".
	Name() string
	// Prune returns a token-reduced view of content for the given language
	// hint (as produced by the pack pipeline, e.g. "go", "typescript").
	Prune(content []byte, lang string) ([]byte, error)
}
