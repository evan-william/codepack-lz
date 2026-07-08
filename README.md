# CodePack-LZ

**Pack a repository for LLMs from a single static binary -- no Node, no
Python, no CGO.** Readable output with token estimates or exact Anthropic
counts for pasting into Claude/ChatGPT, plus a lossless, hash-verified
envelope for moving exact repo snapshots across any text channel.

```
codepack pack .                        # -> markdown context on stdout
codepack pack . --format codepack --codec zstd -o snap.codepack.txt
codepack unpack snap.codepack.txt      # -> byte-identical tree, every hash verified
```

## Why this instead of Repomix/Gitingest?

Those are excellent tools and they shaped this one. codepack-lz differs where
it matters to CI, enterprise, air-gapped, and non-JS environments:

|  | codepack-lz | Repomix | Gitingest |
|---|---|---|---|
| Install | **one static binary** (`go install` / download) | Node + npm | browser / pip |
| Runtime deps | none, CGO-free, works offline | Node.js | Python/web |
| Readable LLM output | md / xml / txt, token-labeled | yes (md/xml/plain) | yes (text) |
| Token counts | local estimates or exact Anthropic counts | yes | yes |
| Secret scan default | yes on, exit code 3 for CI | yes | no |
| Lossless round-trip artifact | yes **hash-verified `pack -> unpack`** | no (output is one-way) | no |
| Comment stripping | yes | yes | no |
| Structural compression | yes (`--compress`, pure Go heuristic) | yes tree-sitter | no |
| MCP server | yes (`codepack mcp`, stdio tools) | yes | no |

If you live happily in a Node toolchain, Repomix is excellent. If you want
one dependency-light Go binary in CI, provable round-trips, secret scanning
on by default, and honest token math -- this is the lane codepack-lz owns.

## Two modes, honestly separated

**Readable (`--format md|xml|txt`)** -- for the LLM job. File tree, fenced
contents, per-file and total token counts. The default `est` mode is marked
`~ ... est.` because it is a local heuristic; `--count-tokens=api` asks
Anthropic for exact counts for the configured model. Optional
`--strip-comments` and `--compress` reduce tokens before rendering.

**Envelope (`--format codepack`)** -- for the transport job. NDJSON ->
gzip/zstd -> base64 with a plaintext header, restored by `unpack` with
**every file verified against its stored SHA-256**. Survives clipboard, chat,
email, DB columns. It is *not* LLM-readable. By default it is not encrypted;
`--encrypt` adds AES-256-GCM using a key from `CODEPACK_KEY_HEX`.

> gzip/zstd saves **bytes**, never **tokens**: a model reads unpacked text, which
> costs the same tokens as the original. Token reduction comes from the
> readable pipeline (`--strip-comments`, `--compress`), not from
> compression. This tool refuses to pretend otherwise.

## Install

```bash
go install github.com/evan-william/codepack-lz/cmd/codepack@latest
```

Or build a static binary anywhere Go runs (no C toolchain needed):

```bash
CGO_ENABLED=0 go build -o codepack ./cmd/codepack
```

## Usage

```bash
codepack pack [path] [flags]     # pack a directory (default: ., stdout)
codepack unpack <file> [--out DIR] [--dry-run]
codepack stats <file>            # inspect without decoding the payload
codepack mcp                     # stdio MCP server for agents
codepack version
```

Common flags for `pack`:

| Flag | Default | Notes |
|---|---|---|
| `--format` | `md` | `md`, `xml`, `txt`, `codepack` |
| `-o, --output` | stdout | summary always goes to stderr |
| `--include` / `--exclude` | -- | doublestar globs, repeatable; no-slash patterns match at any depth |
| `--no-default-ignore` | off | disable the built-in noise list |
| `--max-file-size` | `1MiB` | accepts `512KiB`, `2MB`, raw bytes |
| `--strip-comments` | off | readable formats only |
| `--compress` | off | structurally prune readable source files |
| `--count-tokens` | `est` | `est`, `off`, or `api` (requires `ANTHROPIC_API_KEY`) |
| `--codec` | `gzip` | envelope only: `gzip` or `zstd` |
| `--split-output` | off | readable formats only; writes `.part001` files |
| `--split-size` | `1MiB` | maximum bytes per split part |
| `--encrypt` | off | envelope only; AES-256-GCM key from `CODEPACK_KEY_HEX` |
| `--key-env` | `CODEPACK_KEY_HEX` | env var holding a 32-byte hex key |
| `--redact` | off | mask detected secrets as `[REDACTED:<rule>]` |
| `--no-secret-scan` | off | you own the consequences |
| `--copy` | off | best-effort clipboard copy |

Exact Anthropic counts:

```bash
ANTHROPIC_API_KEY=... codepack pack . --count-tokens api --token-model claude-sonnet-4-20250514
```

Encrypted envelopes:

```bash
# 32 random bytes as 64 hex characters
export CODEPACK_KEY_HEX=$(openssl rand -hex 32)
codepack pack . --format codepack --codec zstd --encrypt -o snap.codepack.txt
codepack unpack snap.codepack.txt --out restored
```

A `.codepackignore` at the pack root adds project-specific rules (gitignore
subset: `#` comments, trailing `/` for dirs, `!` negation with last-match
wins, no-slash patterns match at any depth).

### Exit codes

`0` ok - `1` runtime error - `2` usage error - **`3` secrets detected and
not redacted** -- gate CI on it:

```yaml
- run: codepack pack . --format md -o /dev/null   # fails the job on leaked credentials
```

## Security

- **Secret scanning is on by default**: ~28 embedded rules (adapted from
  gitleaks' MIT ruleset) + Shannon entropy + placeholder allowlist. Findings
  print as `path:line rule (prev...)` -- never the secret itself. Suppress a
  known fixture with a `codepack:allow` comment on that line.
- **Base64 != encryption.** Plain envelopes are trivially decodable; the
  header carries that warning verbatim. `--encrypt` encrypts the compressed
  payload with AES-256-GCM, but scanning/review still matter. Details and
  threat model: [docs/security.md](docs/security.md).
- Unpack rejects path traversal, never overwrites, never follows symlinks.

## Performance

Honest numbers, not adjectives -- i7-8750H laptop (6C/12T), Windows 11,
synthetic 2,000-file / 56 MB Go corpus, secret scan on:

| Operation | Time |
|---|---|
| `pack --format md --count-tokens off` | **0.49 s** |
| `pack --format md` (with token estimates) | 2.3 s |
| `pack --format codepack` (48 MB text -> 1.1 MB envelope) | 0.73 s |
| `unpack` + verify all 2,000 hashes | 5.1 s (FS-bound) |

Reproduce: `go test -bench=BenchmarkBuild ./internal/pack/`, or generate the
corpus with `scripts/gen-bench-corpus.sh` and time `codepack pack` yourself.
Compression ratios are corpus-dependent; this repo packs at ~31%.

## Format stability

The envelope format is versioned and specified in
[docs/format-spec.md](docs/format-spec.md). Determinism is part of the
contract: same tree + same version => byte-identical output (pin `Created`
with `SOURCE_DATE_EPOCH`). Packs from the unreleased prototype are detected
and rejected with a clear message.

## Status

Current prototype target: **v0.3.0**. Implemented: `--codec zstd`,
`--split-output`, exact Anthropic count-tokens API mode, stdio MCP server,
`--compress`, streaming envelope write path, and optional AES-256-GCM
envelope encryption. Future work: stronger language-aware pruning backends,
more MCP tools, signed release artifacts, and packaged VS Code marketplace
distribution.

## VS Code

`vscode-extension/` ships a prototype command -- *CodePack-LZ: Pack Workspace
to Clipboard* -- that shells out to the `codepack` binary on your PATH.

## License

MIT. Secret-detection rules adapted from
[gitleaks](https://github.com/gitleaks/gitleaks) (MIT).
