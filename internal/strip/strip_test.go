package strip

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlashComments(t *testing.T) {
	in := "package main\n\n// line comment\nfunc main() {\n\tx := \"//not a comment\"\n\t/* block\n\tcomment */\n\ty := '/'\n\t_ = x\n\t_ = y // trailing\n}\n"
	got := string(Comments([]byte(in), ".go"))

	require.NotContains(t, got, "line comment")
	require.NotContains(t, got, "block")
	require.NotContains(t, got, "trailing")
	require.Contains(t, got, `"//not a comment"`, "strings survive")
	require.Contains(t, got, "func main() {")
}

func TestHashComments(t *testing.T) {
	in := "#!/usr/bin/env python\n# full line comment\nx = 1  # trailing stays (documented v0.1 limitation)\n"
	got := string(Comments([]byte(in), ".py"))

	require.Contains(t, got, "#!/usr/bin/env python", "shebang survives")
	require.NotContains(t, got, "full line comment")
	require.Contains(t, got, "# trailing stays", "inline hash comments are kept by design")
}

func TestCSSBlockOnly(t *testing.T) {
	in := "/* banner */\na { background: url(//cdn.example.com/x.png); }\n"
	got := string(Comments([]byte(in), ".css"))

	require.NotContains(t, got, "banner")
	require.Contains(t, got, "url(//cdn.example.com/x.png)", "// is not a comment in CSS")
}

func TestUnknownExtensionUntouched(t *testing.T) {
	in := "// looks like a comment but .xyz is unknown\n"
	require.Equal(t, in, string(Comments([]byte(in), ".xyz")))
}

func TestTidyCollapsesBlankRuns(t *testing.T) {
	in := "a := 1\n// gone\n\n\n\n\nb := 2\n"
	got := string(Comments([]byte(in), ".go"))
	require.NotContains(t, got, "\n\n\n\n")
}
