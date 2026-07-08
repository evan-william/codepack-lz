const vscode = require("vscode");
const childProcess = require("child_process");
const fs = require("fs/promises");
const os = require("os");
const path = require("path");

function activate(context) {
  const disposable = vscode.commands.registerCommand("codepack-lz.packWorkspace", async () => {
    const folder = vscode.workspace.workspaceFolders?.[0];
    if (!folder) {
      vscode.window.showWarningMessage("Open a workspace folder before running CodePack-LZ.");
      return;
    }

    const config = vscode.workspace.getConfiguration();
    const binaryPath = config.get("codepackLz.binaryPath", "codepack");
    const format = config.get("codepackLz.format", "md");
    const codec = config.get("codepackLz.codec", "gzip");
    const maxFileSize = config.get("codepackLz.maxFileSize", "1MiB");
    const stripComments = config.get("codepackLz.stripComments", false);
    const compress = config.get("codepackLz.compress", false);
    const countTokens = config.get("codepackLz.countTokens", "est");
    const redact = config.get("codepackLz.redact", false);
    const workspacePath = folder.uri.fsPath;
    const ext = format === "codepack" ? "codepack.txt" : format;
    const outputPath = path.join(os.tmpdir(), `codepack-${Date.now()}.${ext}`);

    const args = ["pack", workspacePath, "--format", format, "-o", outputPath, "--max-file-size", String(maxFileSize)];
    if (format === "codepack") {
      args.push("--codec", String(codec));
    }
    if (stripComments && format !== "codepack") {
      args.push("--strip-comments");
    }
    if (compress && format !== "codepack") {
      args.push("--compress");
    }
    args.push("--count-tokens", String(countTokens));
    if (redact) {
      args.push("--redact");
    }

    try {
      await execFile(binaryPath, args, workspacePath);
      const output = await fs.readFile(outputPath, "utf8");
      await vscode.env.clipboard.writeText(output);
      vscode.window.showInformationMessage(
        `CodePack-LZ copied ${formatBytes(Buffer.byteLength(output, "utf8"))} (${format}) to clipboard.`
      );
    } catch (error) {
      const message = error && error.message ? error.message : String(error);
      if (message.includes("potential secret")) {
        vscode.window.showErrorMessage(
          `CodePack-LZ blocked the pack: ${message} - enable codepackLz.redact to mask secrets instead.`
        );
      } else {
        vscode.window.showErrorMessage(`CodePack-LZ failed: ${message}`);
      }
    } finally {
      await fs.rm(outputPath, { force: true }).catch(() => {});
    }
  });

  context.subscriptions.push(disposable);
}

function execFile(command, args, cwd) {
  return new Promise((resolve, reject) => {
    childProcess.execFile(
      command,
      args,
      {
        cwd,
        windowsHide: true,
        maxBuffer: 1024 * 1024 * 256,
      },
      (error, stdout, stderr) => {
        if (error) {
          reject(new Error((stderr || stdout || error.message).trim()));
          return;
        }
        resolve(stdout);
      }
    );
  });
}

function formatBytes(size) {
  const units = ["B", "KB", "MB", "GB"];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return unit === 0 ? `${size} ${units[unit]}` : `${value.toFixed(1)} ${units[unit]}`;
}

function deactivate() {}

module.exports = {
  activate,
  deactivate,
};
