# Benchmarks

These numbers are intentionally small and reproducible. They are not a claim
that every repository behaves the same way; they show the budgets CodePack-LZ
makes visible: readable input tokens, transport bytes, and restore integrity.

## Local repo smoke run

Run date: 2026-07-10 local workspace  
Tool: `codepack 0.3.0 (envelope format v1)`  
Corpus: this repository, `69` packed files, `5` skipped files, `205,004` raw
packed bytes, secret scan clean

| Output | Command | Artifact bytes | Estimated tokens | Time |
|---|---|---:|---:|---:|
| Readable Markdown | `codepack pack . --format md --redact` | `213,244` | `~77,527` | `55.2 ms` |
| Pruned readable Markdown | `codepack pack . --format md --strip-comments --compress --redact` | `82,531` | `~27,452` | `54.5 ms` |
| Readable Markdown, no token count | `codepack pack . --format md --count-tokens off --redact` | `211,763` | n/a | `72.1 ms` |
| Lossless capsule | `codepack pack . --format codepack --codec zstd --redact` | `84,283` | not LLM-readable | `102.2 ms` |
| Verify capsule | `codepack unpack repo.codepack.txt --dry-run` | n/a | n/a | `46.3 ms` |

Capsule header from the same run:

```text
files:        69 (5 skipped)
bytes raw:    205004
bytes packed: 62099 (zstd, before base64)
ratio:        30.3%
secret scan:  clean
```

## Reproduce

```bash
mkdir -p dist/readme-bench

codepack pack . --format md --redact \
  -o dist/readme-bench/repo.default.md

codepack pack . --format md --strip-comments --compress --redact \
  -o dist/readme-bench/repo.compress.md

codepack pack . --format codepack --codec zstd --redact \
  -o dist/readme-bench/repo.codepack.txt

codepack stats dist/readme-bench/repo.codepack.txt
codepack unpack dist/readme-bench/repo.codepack.txt --dry-run
```

## Reading the charts

- The token chart is about **chat input cost**. Only readable text can be
  understood by a normal chat model, and only readable text should be counted
  as LLM input.
- The size chart is about **transport/storage cost**. The `.codepack.txt`
  capsule is compact and restorable, but a normal LLM will not understand its
  base64 payload without a decoder.
- Dollar cost changes by provider and date. CodePack-LZ exposes estimated or
  provider-counted input tokens so users can multiply by their current model
  price instead of trusting stale README numbers.
