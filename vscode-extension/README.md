# CodePack-LZ VS Code Extension

Prototype VS Code wrapper for the `codepack-lz` CLI.

## Requirements

Install or build the CLI first:

```bash
go install github.com/evan-william/codepack-lz/cmd/codepack-lz@latest
```

Or configure `codepackLz.binaryPath` to point at a local build, such as:

```text
bin/codepack-lz.exe
```

## Command

```text
CodePack-LZ: Pack Workspace to Clipboard
```

The command packs the first open workspace folder, copies the generated envelope to your clipboard, and deletes the temporary output file.
