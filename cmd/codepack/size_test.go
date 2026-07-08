package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"1048576", 1 << 20},
		{"1MiB", 1 << 20},
		{"512KiB", 512 << 10},
		{"1GiB", 1 << 30},
		{"2MB", 2_000_000},
		{"500KB", 500_000},
		{"1GB", 1_000_000_000},
		{"64B", 64},
		{"0", 0},
		{" 1mib ", 1 << 20}, // case-insensitive, trimmed
		{"1.5MiB", 1_572_864},
	}
	for _, c := range cases {
		got, err := parseSize(c.in)
		require.NoError(t, err, c.in)
		require.Equal(t, c.want, got, c.in)
	}

	for _, bad := range []string{"", "abc", "-5", "12XB"} {
		_, err := parseSize(bad)
		require.Error(t, err, bad)
	}
}
