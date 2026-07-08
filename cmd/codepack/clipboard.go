package main

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

// copyToClipboard pipes data to the platform clipboard tool. Strictly
// best-effort: no bundled dependency, and a missing tool is a warning, not a
// failure. On Windows, clip.exe interprets stdin in the console code page,
// so non-ASCII content can arrive mangled -- documented in the README.
func copyToClipboard(data []byte) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "windows":
		candidates = [][]string{{"clip"}}
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	default:
		candidates = [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--input", "--clipboard"},
		}
	}

	for _, argv := range candidates {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		if _, err := stdin.Write(data); err != nil {
			return err
		}
		if err := stdin.Close(); err != nil && err != io.ErrClosedPipe {
			return err
		}
		return cmd.Wait()
	}
	return fmt.Errorf("no clipboard tool found (looked for %s)", clipboardToolNames(candidates))
}

func clipboardToolNames(candidates [][]string) string {
	names := ""
	for i, argv := range candidates {
		if i > 0 {
			names += ", "
		}
		names += argv[0]
	}
	return names
}
