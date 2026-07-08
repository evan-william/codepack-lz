# Security model

Packing a whole repository to hand to an LLM (or anyone) is a
**secret-leak vector**. codepack-lz treats that as a first-class problem.

## Base64 is not encryption -- read this first

A `.codepack.txt` envelope *looks* opaque. It is not. Base64 + gzip/zstd is an
encoding anyone can reverse in one line of code. **Every secret that goes
into an envelope can be read by anyone who has the file.** The envelope
header carries this warning verbatim, and so does every `pack`/`stats` run.

`--encrypt` changes the payload from "encoded" to AES-256-GCM encrypted:

```bash
export CODEPACK_KEY_HEX=$(openssl rand -hex 32)
codepack pack . --format codepack --codec zstd --encrypt -o snap.codepack.txt
codepack unpack snap.codepack.txt --out restored
```

That protects file contents only while the 32-byte key stays secret. Header
metadata remains plaintext by design: root name, file counts, byte counts,
codec, and secret-scan status. Encryption is layered **on top of** scanning,
never instead of it.

## Secret scanning (on by default)

Every packed file's content is scanned **before any output is written**:

- **Ruleset:** ~28 regex rules adapted from the
  [gitleaks](https://github.com/gitleaks/gitleaks) default rules (MIT),
  simplified for Go's RE2 engine and embedded in the binary
  (`internal/secret/rules.json`) -- scanning needs no network and no config.
- **Keyword prefilter** per rule keeps scanning fast.
- **Shannon entropy** thresholds on generic rules (quoted/unquoted
  keyword-adjacent values, connection-string passwords) cut false positives
  from ordinary identifiers.
- **Allowlist** patterns drop obvious placeholders (`EXAMPLE`, `changeme`,
  `${VAR}`, `<your-key>`, `process.env...`).

### Outcomes

| Situation | Behavior |
|---|---|
| findings, default | pack **fails with exit code 3**, prints `path:line rule (prev...)` -- never the secret itself |
| findings, `--redact` | each match is replaced with `[REDACTED:<rule-id>]`; the manifest records path, rule, and count; stored hashes cover the redacted bytes so round-trip stays verifiable |
| intended fixture | add a `codepack:allow` comment on that line |
| `--no-secret-scan` | scanning off; you own the consequences |

Exit code 3 is stable API: gate CI on it.

```yaml
# CI example: fail the job if a pack would leak credentials
- run: codepack pack . --format md -o /dev/null
```

### Limits -- scanning is a risk reducer, not a guarantee

Regex + entropy cannot catch every secret: novel token formats, split
strings, encrypted blobs, or credentials in comments phrased as prose all
pass. Detection also deliberately skips binary files (they are never packed).
**Always review output before sharing it**, whatever the scan says.

## Defense on the read side

`codepack unpack`:

- rejects absolute paths, `..` segments, drive letters, backslashes, and NUL
  in stored paths *before* touching the filesystem (path-traversal defense);
- never overwrites an existing file;
- never follows or creates symlinks (they are skipped at pack time);
- verifies every restored file against its stored SHA-256 and aborts, naming
  the file, on the first mismatch -- a tampered or corrupted pack cannot
  restore silently.

## Default exclusions

The built-in ignore list keeps `.env`, `.env.*` (except `.env.example` and
friends), `*.pem`, `*.key`, keystores, and similar conventional secret files
out of packs entirely -- recorded as skips, overridable only explicitly.

## Reporting

Found a way around any of this? Open a GitHub security advisory on the
repository (preferred) or an issue with minimal reproduction details.
