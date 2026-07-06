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
    const binaryPath = config.get("codepackLz.binaryPath", "codepack-lz");
    const maxFileSize = config.get("codepackLz.maxFileSize", 1048576);
    const pruneComments = config.get("codepackLz.pruneComments", false);
    const workspacePath = folder.uri.fsPath;
    const outputPath = path.join(os.tmpdir(), `codepack-lz-${Date.now()}.codepack.txt`);

    const args = [
      "pack",
      "-o",
      outputPath,
      "--max-file-size",
      String(maxFileSize),
    ];
    if (pruneComments) {
      args.push("--prune-comments");
    }
    args.push(workspacePath);

    try {
      await execFile(binaryPath, args, workspacePath);
      const envelope = await fs.readFile(outputPath, "utf8");
      await vscode.env.clipboard.writeText(envelope);
      vscode.window.showInformationMessage(
        `CodePack-LZ copied ${formatBytes(Buffer.byteLength(envelope, "utf8"))} to clipboard.`
      );
    } catch (error) {
      const message = error && error.message ? error.message : String(error);
      vscode.window.showErrorMessage(`CodePack-LZ failed: ${message}`);
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
        maxBuffer: 1024 * 1024 * 64,
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
