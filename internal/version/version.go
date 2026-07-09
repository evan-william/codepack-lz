// Package version holds the tool version, overridable at build time via
// -ldflags "-X github.com/evan-william/codepack-lz/internal/version.Version=vX.Y.Z".
package version

// Version is the tool version reported in output headers and `codepack version`.
var Version = "0.3.1"

// FormatVersion is the envelope format version. Bump on any breaking payload
// change; see docs/format-spec.md.
const FormatVersion = 1
