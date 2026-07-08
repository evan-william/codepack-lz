# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

No unreleased changes yet.

## [0.3.0] -- 2026-07-08

Prototype scope completed through the v0.3 target.

### Added
- `--codec zstd` for codepack envelopes via pure-Go
  `github.com/klauspost/compress/zstd`.
- `--split-output` and `--split-size` for readable outputs that need to fit
  chat or tool size limits.
- `--count-tokens=api` for exact Anthropic message token counts using
  `ANTHROPIC_API_KEY`, `ANTHROPIC_MODEL`, and an optional `--token-model`.
- `codepack mcp`, a stdio MCP server exposing `pack_codebase` and
  `stats_codepack` tools.
- `--compress`, a dependency-free structural source pruner for readable
  Go, JavaScript, TypeScript, TSX, and Python files.
- Streaming envelope write path: payload NDJSON is written through the
  compressor to a temp spool before base64 framing, avoiding a full
  compressed-payload buffer in memory.
- `--encrypt` / `--key-env` for AES-256-GCM encrypted envelopes using a
  32-byte hex key from the environment.
- VS Code wrapper settings for codec, compression, and token-count mode.

### Changed
- Tool version is now `0.3.0`; envelope format remains `v1` because the
  payload schema is compatible and header additions are optional/validated.
- Readable renderers now distinguish estimated local counts from exact
  provider counts.

### Fixed
- Roadmap text no longer advertises implemented v0.2/v0.3 features as future
  work.

## [0.1.0] -- 2026-07-08

First real release; supersedes the unreleased single-file prototype.

### Added
- `pack` with dual-mode output: readable `md`/`xml`/`txt` for LLM pasting,
  `codepack` envelope (NDJSON -> gzip -> base64, plaintext header) for
  lossless transport. Versioned format spec in `docs/format-spec.md`.
- `unpack` with streaming decode, path-traversal defense, no-overwrite
  policy, and per-file SHA-256 verification.
- `stats`: inspect envelopes without decoding the payload.
- Secret scanning on by default: 28 embedded rules (adapted from gitleaks,
  MIT) + Shannon entropy + allowlist; `--redact`, `codepack:allow`
  suppression, and CI-stable exit code 3.
- Deterministic output: sorted walk, zero gzip mtime, `SOURCE_DATE_EPOCH`
  support; identical trees produce byte-identical packs.
- Token estimates (local heuristic -- fully offline), always
  labeled as estimates.
- Filtering: embedded default ignore list, `.codepackignore` (gitignore
  subset with negation), `--include`/`--exclude` doublestar globs,
  `--max-file-size` with human units, binary/symlink detection; every skip
  recorded in the manifest with a reason.
- Content dedup: byte-identical files stored once (`dup_of` references).
- `--strip-comments` heuristic comment removal for readable formats.
- `--copy` best-effort clipboard integration.
- Cross-platform: CGO-free build for linux/darwin/windows x amd64/arm64.

### Changed
- **Breaking vs prototype:** binary renamed `codepack-lz` -> `codepack`,
  module path fixed to `github.com/evan-william/codepack-lz`, envelope format
  redesigned (v1). Prototype envelopes are detected and rejected with
  guidance to repack.
- Positioning: "AI-ready Base64" claim dropped; the envelope is transport,
  the readable formats are the LLM surface.

### Fixed
- Prototype stored normalized (mutated) content while hashing the original
  bytes, making round-trips lossy and hashes unverifiable. Content is now
  stored verbatim and every stored hash covers the stored bytes.
