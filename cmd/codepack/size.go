package main

import (
	"fmt"
	"strconv"
	"strings"
)

// parseSize parses human byte sizes: bare digits are bytes; IEC suffixes
// (KiB, MiB, GiB) are powers of 1024; SI suffixes (KB, MB, GB) are powers of
// 1000; a trailing "B" alone is bytes. Case-insensitive.
func parseSize(s string) (int64, error) {
	in := strings.TrimSpace(s)
	if in == "" {
		return 0, fmt.Errorf("empty size")
	}
	upper := strings.ToUpper(in)

	multipliers := []struct {
		suffix string
		factor int64
	}{
		{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30},
		{"KB", 1_000}, {"MB", 1_000_000}, {"GB", 1_000_000_000},
		{"B", 1},
	}

	factor := int64(1)
	num := upper
	for _, m := range multipliers {
		if strings.HasSuffix(upper, m.suffix) {
			factor = m.factor
			num = strings.TrimSpace(strings.TrimSuffix(upper, m.suffix))
			break
		}
	}

	value, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q (examples: 1048576, 512KiB, 2MB)", s)
	}
	if value < 0 {
		return 0, fmt.Errorf("size must not be negative")
	}
	return int64(value * float64(factor)), nil
}
