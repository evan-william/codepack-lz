# CodePack envelope format -- v1

This document is the authoritative specification of the `.codepack.txt`
envelope written by `codepack pack --format codepack` and read by
`codepack unpack` / `codepack stats`. The write-side reference implementation
is `internal/format/envelope.go`; the read side is `internal/unpack`.

The envelope is a **lossless, verifiable transport container**, not an
LLM-readable document. For pasting into a chat, use the readable formats
(`md`, `xml`, `txt`).

## Layout

A UTF-8 text file with three regions:

```
CODEPACK-LZ v1
<header: "Key: value" lines>

--- BEGIN CODEPACK PAYLOAD ---
<base64 of compressed or encrypted payload, wrapped at 76 columns>
--- END CODEPACK PAYLOAD ---
```

## Header

Plaintext, readable and greppable **without decoding anything** -- that is the
point of keeping it outside the fence. Keys:

| Key | Meaning |
|---|---|
| `Format-Version` | integer; this spec is version `1` |
| `Tool-Version` | codepack-lz version that wrote the file |
| `Encoding` | always `base64` |
| `Codec` | `gzip` or `zstd` |
| `Encryption` | `none` or `aes-256-gcm` |
| `Nonce` | base64 AES-GCM nonce; present only when encrypted |
| `Created` | RFC 3339 UTC. Honors `SOURCE_DATE_EPOCH` for reproducible packs |
| `Root` | base name of the packed directory |
| `Files` | number of file entries in the payload |
| `Skipped` | number of skip records in the manifest |
| `Bytes-Raw` | sum of stored file sizes |
| `Bytes-Packed` | payload size in bytes before base64; compressed bytes when unencrypted, ciphertext bytes when encrypted |
| `Hash-Algo` | always `sha256` |
| `Secret-Scan` | `clean`, `redacted`, or `skipped` |
| `Warning` | plaintext warning; either base64-is-not-encryption or encrypted-payload notice |

Readers must tolerate unknown keys (added by newer minor revisions) and must
reject `Format-Version` values greater than they support.

## Payload

NDJSON (one JSON object per line), compressed with the codec, optionally
encrypted, then base64.
The first line is the **manifest**; every following line is one **file**, in
path-ascending byte order.

```jsonc
// line 1 -- manifest
{"type":"manifest","root":"myrepo","files":123,"skipped":4,
 "skips":[{"path":"logo.png","reason":"binary","size":20480}],
 "redactions":[{"path":"cfg.go","rule":"aws-access-key-id","count":1}],
 "order":"path-asc","hash_algo":"sha256"}

// line 2..N -- one per file
{"type":"file","path":"src/main.go","size":812,"sha256":"...","lang":"go","content":"..."}

// duplicate files elide content and reference the canonical copy
{"type":"file","path":"src/copy.go","size":812,"sha256":"...","lang":"go","dup_of":"src/main.go"}
```

### Rules

- **Paths** are slash-separated (`/`), relative, and never contain `..`, `.`,
  empty segments, backslashes, drive letters, or NUL. Readers must reject
  violations before writing anything (path-traversal defense).
- **Ordering** is path-ascending byte order and is part of the format:
  identical input trees produce identical payloads.
- **`sha256` is always the hash of the stored `content` bytes.** When no
  transform was applied, stored bytes are the original file bytes. The only
  permitted transform is secret redaction, which is recorded per file in the
  manifest's `redactions` list.
- **Duplicates:** files whose bytes are identical to an earlier file carry
  `dup_of` (referencing the canonical, lexicographically earlier path) and no
  `content`. An empty file has `"content":""` -- present, not omitted.
- **Skips:** everything not packed appears in `skips` with a reason:
  `default-ignore`, `ignore-file`, `exclude-flag`, `not-included`,
  `max-file-size`, `binary`, `symlink`, `self-output`, `unreadable`.
  Directory skips carry a trailing `/` and stand for the whole subtree.
- **Content is UTF-8 text.** Binary files (NUL byte or invalid UTF-8) are
  never packed; they are recorded as skips. CRLF line endings are content and
  are preserved verbatim.

## Restore semantics

`codepack unpack`:

1. Parses and validates the header.
2. Streams the payload (base64 -> optional decrypt -> codec -> NDJSON). Plain
   envelopes stream end-to-end; encrypted envelopes read the ciphertext before
   AES-GCM authentication/decryption.
3. Validates every path, refuses to overwrite existing files, restores
   regular files with `0644` permissions.
4. Verifies every restored file's SHA-256 against the stored value and fails,
   naming the file, on any mismatch.
5. Cross-checks header, manifest, and payload counts.

## Determinism

Same input tree + same tool version => byte-identical envelope, provided
`SOURCE_DATE_EPOCH` is set (otherwise only the `Created` header line
differs). gzip output is written with a zero modification time for this
reason; zstd output is deterministic for identical input and encoder version.
Encrypted envelopes are intentionally non-deterministic because AES-GCM uses
a fresh nonce. Determinism across *different* Go toolchain or codec versions
is not guaranteed; the round-trip guarantee is unaffected either way.

## Limitations (deliberate)

- File modes, mtimes, owners, symlinks, and empty directories are **not**
  preserved. This is a context artifact, not a backup format.
- The envelope stores text files only.
- Base64 + gzip/zstd is **transport** compression: it saves bytes on the
  wire, never LLM tokens. See the README's token table.
- AES-256-GCM encryption protects payload confidentiality only while the key
  remains secret. Header metadata (root, file count, byte counts, codec,
  secret-scan status) remains plaintext by design.

## Versioning

Any change that breaks readers -- new required fields, different ordering,
different hash algorithm, different framing -- bumps `Format-Version` and the
magic line. Readers keep accepting all older versions they know, or fail
with a clear message.

## Legacy prototype (pre-release)

The unreleased v0.1 prototype used magic `CODEPACK-LZ v0.1` with
`---BEGIN CODEPACK-LZ---` markers and a single-JSON payload. Readers detect
it and reply: repack with the current tool. It is not otherwise supported.
