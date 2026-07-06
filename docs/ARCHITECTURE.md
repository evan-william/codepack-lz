# Architecture

CodePack-LZ currently has two prototype layers:

1. `cmd/codepack-lz`: the Go CLI that owns the file format, directory walking, filtering, compression, unpacking, and stats.
2. `vscode-extension`: a thin VS Code command wrapper that shells out to the CLI and copies the pack to the clipboard.

## Pack Flow

```text
directory
  -> recursive walker
  -> noise filter
  -> text/binary classifier
  -> metadata collector
  -> JSON payload
  -> gzip
  -> base64 envelope
```

## Unpack Flow

```text
.codepack.txt
  -> marker extraction
  -> base64 decode
  -> gzip inflate
  -> JSON payload
  -> file writer
```

## Design Goals

- Keep the first prototype dependency-free.
- Make every pack reversible.
- Avoid silently including generated dependency directories.
- Keep claims measurable until the custom codec exists.
- Let the VS Code extension depend on the CLI instead of duplicating format logic.
