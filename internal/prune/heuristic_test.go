package prune

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeuristicPrunesGoFunctionBodies(t *testing.T) {
	src := []byte("package demo\n\nfunc Add(a, b int) int {\n\tresult := a + b\n\treturn result\n}\n")
	got, err := NewHeuristic().Prune(src, "go")
	require.NoError(t, err)
	require.Less(t, len(got), len(src))
	require.Contains(t, string(got), "func Add(a, b int) int {")
	require.Contains(t, string(got), "...")
	require.NotContains(t, string(got), "result := a + b")
}

func TestHeuristicPrunesPythonBlocks(t *testing.T) {
	src := []byte("import os\n\nclass App:\n    def run(self):\n        value = os.getcwd()\n        return value\n")
	got, err := NewHeuristic().Prune(src, "python")
	require.NoError(t, err)
	require.Less(t, len(got), len(src))
	require.Contains(t, string(got), "class App:")
	require.Contains(t, string(got), "def run(self):")
	require.NotContains(t, string(got), "os.getcwd")
}

func TestHeuristicLeavesUnknownLanguages(t *testing.T) {
	src := []byte("hello\n")
	got, err := NewHeuristic().Prune(src, "text")
	require.NoError(t, err)
	require.Equal(t, src, got)
}
