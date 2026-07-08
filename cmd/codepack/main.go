// Command codepack packs a repository into an LLM-readable bundle or a
// lossless transport envelope, and restores envelopes with hash verification.
//
// Exit codes: 0 ok - 1 runtime error - 2 usage error - 3 secrets detected
// and not redacted (so CI can gate on leaked credentials).
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/evan-william/codepack-lz/internal/pack"
)

const (
	exitOK      = 0
	exitError   = 1
	exitUsage   = 2
	exitSecrets = 3
)

// usageError marks user mistakes (bad flags/args) for exit code 2.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }
func (u usageError) Unwrap() error { return u.err }

func main() { os.Exit(run()) }

func run() int {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		var se *pack.SecretError
		if errors.As(err, &se) {
			fmt.Fprintf(os.Stderr, "\n%s\n", pack.FormatFindings(se.Findings))
		}
		return exitCodeFor(err)
	}
	return exitOK
}

// exitCodeFor maps error kinds to the documented exit codes.
func exitCodeFor(err error) int {
	var se *pack.SecretError
	var ue usageError
	switch {
	case errors.As(err, &se):
		return exitSecrets
	case errors.As(err, &ue):
		return exitUsage
	default:
		return exitError
	}
}
