// Package model defines the core data types shared by every stage of the
// pack pipeline: walking, scanning, rendering, and unpacking.
package model

// Skip reasons recorded in the manifest. Every file or directory that is not
// packed carries exactly one of these so nothing is ever silently dropped.
const (
	SkipDefaultIgnore = "default-ignore"
	SkipIgnoreFile    = "ignore-file"
	SkipExcludeFlag   = "exclude-flag"
	SkipNotIncluded   = "not-included"
	SkipMaxFileSize   = "max-file-size"
	SkipBinary        = "binary"
	SkipSymlink       = "symlink"
	SkipSelfOutput    = "self-output"
	SkipUnreadable    = "unreadable"
)

// TokensUncounted marks packs produced with --count-tokens=off.
const TokensUncounted = -1

// File is one packed file.
//
// Invariant: SHA256 is always the hex SHA-256 of Content -- the bytes actually
// stored in the pack -- never of some other version of the file. When no
// transform is applied, Content is byte-identical to the file on disk. When a
// transform is applied (redaction), the manifest records it.
type File struct {
	Path    string // slash-separated path relative to the pack root
	Size    int64  // len(Content)
	SHA256  string // hex SHA-256 of Content
	Lang    string // language hint derived from the extension; may be ""
	Content []byte
	DupOf   string // when non-empty, Content is elided and identical to the file at DupOf
	Tokens  int    // token count; TokensUncounted when counting is off
}

// Skip records a file or directory that was not packed. Directory paths carry
// a trailing "/" and represent their whole subtree.
type Skip struct {
	Path   string
	Reason string
	Size   int64 // 0 for directories
}

// Redaction records that secret matches were replaced in a packed file.
type Redaction struct {
	Path  string
	Rule  string
	Count int
}

// Pack is a fully assembled, deterministic snapshot of a directory tree.
// Files and Skips are sorted by Path ascending (byte order).
type Pack struct {
	Root          string
	Files         []File
	Skips         []Skip
	Redactions    []Redaction
	TotalBytes    int64  // sum of File.Size
	TotalTokens   int    // sum of File.Tokens; TokensUncounted when counting is off
	Tokenizer     string // name of the counter, e.g. "heuristic"; "" when off
	TokenEstimate bool   // true when Tokenizer reports approximate counts
	SecretScan    string // "clean", "redacted", or "skipped" -- shown in output headers
}
