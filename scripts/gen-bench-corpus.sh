#!/usr/bin/env bash
# Generates the synthetic benchmark corpus used for the README performance
# numbers: 2,000 Go files (~56 MB) across 40 packages.
#
# Usage: ./scripts/gen-bench-corpus.sh <target-dir>
set -euo pipefail

target="${1:?usage: gen-bench-corpus.sh <target-dir>}"
mkdir -p "$target"
cd "$target"

for d in $(seq -w 1 40); do
  mkdir -p "pkg$d"
  for f in $(seq -w 1 50); do
    {
      echo "package pkg$d"
      echo
      for i in $(seq 1 120); do
        echo "// handler $i validates input and writes the response for route $i."
        echo "func Handler${f}_$i(w io.Writer, r *Request) error {"
        echo "	if r == nil { return errNilRequest }"
        echo "	return writeJSON(w, map[string]int{\"route\": $i})"
        echo "}"
        echo
      done
    } > "pkg$d/file$f.go"
  done
done

echo "corpus ready: $(find . -type f | wc -l) files, $(du -sh . | cut -f1)"
