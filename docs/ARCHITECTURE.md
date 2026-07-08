# Architecture

One static Go binary, no CGO, no external runtime. All logic lives in
`internal/` behind small packages; `cmd/codepack` is a thin cobra shell.

## Pack pipeline

```
directory
  -> walk      filter chain: symlink -> self-output -> default ignores ->
              .codepackignore -> --exclude -> --include -> size cap
              (every skip recorded with a reason; dirs pruned once)
  -> pack      bounded worker pool per file:
              read -> binary sniff -> secret scan -> [redact] -> [strip comments]
              -> [structural prune] -> sha256 -> token count
              then: sort by path, dedup identical bytes, totals
  -> format    md | xml | txt  (readable, token-labeled)
              codepack        (NDJSON -> gzip/zstd -> [AES-GCM] -> base64
                               + plaintext header)
```

Key invariants (tested):

- **Determinism.** Concurrency never affects output: results are assembled
  in sorted path order; gzip is written with zero mtime; `SOURCE_DATE_EPOCH`
  pins the one timestamp. Same input + same version => same bytes.
- **`sha256(content)` is the stored content's hash -- always.** Redaction is
  the only transform the envelope permits, and the manifest records it.
  `--strip-comments` and `--compress` are rejected for the envelope at the
  CLI boundary.
- **Nothing is silently dropped.** Binary, oversized, ignored, unreadable,
  symlink -- all end up in the manifest with a reason.

## Unpack pipeline

```
.codepack.txt
  -> header     parsed without decoding (stats stops here)
  -> payload    base64 reader -> [AES-GCM decrypt] -> gzip/zstd reader
              -> json.Decoder (NDJSON)
  -> restore    path validation -> refuse overwrite -> write 0644
  -> verify     sha256 per file; abort naming the file on mismatch
```

Plain unpack is fully streaming. Encrypted unpack reads ciphertext first so
AES-GCM can authenticate it before decompression. Pack streams NDJSON through
the selected compressor into a temp spool, records `Bytes-Packed`, then
base64-frames the spool into the final output.

## Package map

| Package | Responsibility |
|---|---|
| `internal/model` | core types (`Pack`, `File`, `Skip`, `Redaction`), skip reasons |
| `internal/walk` | directory walk, ignore rulesets (embedded defaults + `.codepackignore`), include/exclude globs |
| `internal/pack` | pipeline orchestration, hashing, dedup, language hints |
| `internal/secret` | embedded gitleaks-derived rules, entropy, allowlist, redaction |
| `internal/strip` | heuristic comment stripping (readable formats only) |
| `internal/prune` | structural source pruning for readable compression |
| `internal/tokens` | `Counter` interface; offline heuristic and Anthropic API counter |
| `internal/format` | renderers: markdown, xml, txt, envelope; file tree |
| `internal/unpack` | header parsing, streaming restore, hash verification, path safety |
| `internal/stats` | inspect outputs without decoding payloads |
| `internal/version` | tool + format version (ldflags-overridable) |

Growth points: `tokens.Counter` can host more provider counters, and
`prune.Pruner` can be swapped for stronger language parsers later while the
default build stays CGO-free.

## VS Code extension

`vscode-extension/` is a thin wrapper: it shells out to the `codepack`
binary on PATH and copies the output to the clipboard. It never duplicates
format logic.

## Design decisions with rationale

- **Dual mode, honestly split.** gzip/zstd+base64 saves transport bytes, not LLM
  tokens; the readable formats do the LLM job with token estimates, the
  envelope does transport/round-trip. See `brainstorm.md` in the parent
  planning docs for the full analysis.
- **Token counts are labeled estimates.** The default counter is a local
  heuristic, not a provider tokenizer. Exact Claude counts use the
  Anthropic count-tokens API as an opt-in.
- **NDJSON over one giant JSON.** Enables streaming reads today and
  streaming writes later; corruption localizes to a line.
- **Secret scan on by default with a CI-stable exit code (3).** Matching
  the incumbent's table stakes (Repomix/Secretlint) with zero setup.
