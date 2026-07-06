# CodePack-LZ

Zero-dependency, ultra-fast codebase context packer that compresses a repository into a single AI-ready Base64 envelope.

CodePack-LZ is a first prototype of a CLI and VS Code workflow for sending complete project context to LLMs without copying files one by one. It walks a directory, skips noisy build/dependency folders, stores text files with metadata, compresses the payload with gzip, and emits one portable `.codepack.txt` file that can be decoded locally.

> Prototype note: this version uses proven gzip/DEFLATE compression as the first LZ-family backend. Custom structural encoding, AST pruning, and model-native token codecs are planned after the file format stabilizes.

## Why

Tools like Repomix and Gitingest make repositories easier to paste into AI chats, but they usually emit large raw text or XML-style bundles. CodePack-LZ explores a smaller envelope format for developer-to-agent context transfer: one command, one compressed file, reversible by design.

## Features

- Packs a whole directory into one `.codepack.txt` envelope.
- Skips common noise like `.git`, `node_modules`, build outputs, caches, and logs.
- Detects binary files and oversized files to avoid bloated packs.
- Preserves file paths, byte sizes, hashes, language hints, and full text content.
- Unpacks the envelope back into a directory for verification or handoff.
- Includes a VS Code extension prototype command that copies a workspace pack to clipboard.

## Install From Source

```bash
go install github.com/evan-william/codepack-lz/cmd/codepack-lz@latest
```

During local development:

```bash
go build -o bin/codepack-lz.exe ./cmd/codepack-lz
```

## CLI Usage

Pack the current directory:

```bash
codepack-lz pack . -o codepack-lz-output.codepack.txt
```

Unpack a generated envelope:

```bash
codepack-lz unpack codepack-lz-output.codepack.txt -o restored-project
```

Inspect pack stats:

```bash
codepack-lz stats codepack-lz-output.codepack.txt
```

Useful flags:

```bash
codepack-lz pack . --max-file-size 524288 --include-hidden --prune-comments
```

## VS Code Prototype

The `vscode-extension` folder contains a JavaScript-only extension prototype. It looks for a `codepack-lz` binary on your PATH, runs `codepack-lz pack <workspace>`, and copies the generated envelope to the clipboard.

Command palette:

```text
CodePack-LZ: Pack Workspace to Clipboard
```

## Format

The output file is intentionally simple:

```text
CODEPACK-LZ v0.1
encoding: gzip+base64

---BEGIN CODEPACK-LZ---
...
---END CODEPACK-LZ---
```

The encoded payload is JSON compressed with gzip and encoded with Base64. Use `codepack-lz unpack` or `codepack-lz stats` to decode it safely.

## Roadmap

- Streaming packer for very large repositories.
- `.codepackignore` support.
- Language-aware AST pruning.
- Custom token dictionary for repeated syntax patterns.
- Native VS Code extension packaging.
- MCP server integration for agent workflows.

## License

MIT
