package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPListTools(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	var out bytes.Buffer
	err := runMCP(strings.NewReader(fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(req), req)), &out)
	require.NoError(t, err)
	require.Contains(t, out.String(), "pack_codebase")
	require.Contains(t, out.String(), "stats_codepack")
}
