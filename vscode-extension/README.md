# CodePack-LZ -- VS Code extension (prototype)

Thin wrapper around the `codepack` CLI: runs
`codepack pack <workspace> --format <fmt> -o <tmp>` and copies the result to
your clipboard.

Requires the `codepack` binary on your PATH (or set `codepackLz.binaryPath`):

```bash
go install github.com/evan-william/codepack-lz/cmd/codepack@latest
```

Command palette -> **CodePack-LZ: Pack Workspace to Clipboard**.

Settings: `codepackLz.format` (md/xml/txt/codepack), `codepackLz.codec`
(gzip/zstd for envelopes), `codepackLz.maxFileSize`,
`codepackLz.stripComments`, `codepackLz.compress`,
`codepackLz.countTokens`, `codepackLz.redact`, `codepackLz.binaryPath`.

If the pack is blocked with a "potential secret" error, the CLI's secret
scanner found something; review it, add a `codepack:allow` comment for
intended fixtures, or enable `codepackLz.redact`.
