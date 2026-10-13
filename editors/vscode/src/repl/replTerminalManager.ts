import fs from "node:fs";
import path from "node:path";

import type * as vscode from "vscode";

import type { DoggoSettings } from "../config/settings";
import { resolveDoggoCommand } from "../serverCommand";

interface ReplTerminalManagerOptions {
  output: vscode.OutputChannel;
  createTerminal: (options: vscode.TerminalOptions) => vscode.Terminal;
  resolveWorkspaceFolderPath: (documentURI: vscode.Uri) => string | undefined;
  onStartError: () => Promise<void>;
}

export class ReplTerminalManager {
  private terminal: vscode.Terminal | undefined;

  private readonly output: vscode.OutputChannel;
  private readonly createTerminal: (options: vscode.TerminalOptions) => vscode.Terminal;
  private readonly resolveWorkspaceFolderPath: (documentURI: vscode.Uri) => string | undefined;
  private readonly onStartError: () => Promise<void>;

  constructor(options: ReplTerminalManagerOptions) {
    this.output = options.output;
    this.createTerminal = options.createTerminal;
    this.resolveWorkspaceFolderPath = options.resolveWorkspaceFolderPath;
    this.onStartError = options.onStartError;
  }

  handleDidCloseTerminal(closedTerminal: vscode.Terminal): void {
    if (this.terminal !== undefined && this.terminal === closedTerminal) {
      this.terminal = undefined;
    }
  }

  async ensureTerminal(documentURI: vscode.Uri, settings: DoggoSettings): Promise<vscode.Terminal | undefined> {
    if (this.terminal !== undefined) {
      return this.terminal;
    }

    const workspaceFolderPath = this.resolveWorkspaceFolderPath(documentURI);
    const resolved = resolveDoggoCommand({
      subcommand: "repl",
      configuredCommand: settings.lspCommand,
      configuredArgs: [],
      configuredEnv: settings.lspEnv
    });

    this.output.appendLine(
      `[doggo] starting REPL terminal (${resolved.source}): ${resolved.command} ${resolved.args.join(" ")}`
    );

    if (!isCommandLikelyAvailable(resolved.command, resolved.env)) {
      this.output.appendLine(`[doggo] failed to start REPL terminal: command not found: ${resolved.command}`);
      await this.onStartError();
      return undefined;
    }

    const options: vscode.TerminalOptions = {
      name: "Doggo REPL",
      shellPath: resolved.command,
      shellArgs: resolved.args,
      env: resolved.env
    };
    if (workspaceFolderPath !== undefined) {
      options.cwd = workspaceFolderPath;
    }

    this.terminal = this.createTerminal(options);
    this.output.appendLine("[doggo] REPL terminal started.");
    return this.terminal;
  }
}

function isCommandLikelyAvailable(command: string, env: NodeJS.ProcessEnv): boolean {
  const trimmed = command.trim();
  if (trimmed === "") {
    return false;
  }

  if (trimmed.includes("/") || trimmed.includes("\\") || path.isAbsolute(trimmed)) {
    return fs.existsSync(trimmed);
  }

  const pathValue = env.PATH ?? process.env.PATH ?? "";
  if (pathValue === "") {
    return false;
  }
  const pathEntries = pathValue.split(path.delimiter).filter((entry) => entry.trim() !== "");
  if (pathEntries.length === 0) {
    return false;
  }

  if (process.platform === "win32") {
    const pathext = env.PATHEXT ?? process.env.PATHEXT ?? ".EXE;.CMD;.BAT;.COM";
    const extensions = pathext
      .split(";")
      .map((ext) => ext.trim())
      .filter((ext) => ext !== "")
      .map((ext) => ext.startsWith(".") ? ext : `.${ext}`);
    for (const entry of pathEntries) {
      const exact = path.join(entry, trimmed);
      if (fs.existsSync(exact)) {
        return true;
      }
      for (const ext of extensions) {
        if (fs.existsSync(path.join(entry, `${trimmed}${ext}`))) {
          return true;
        }
      }
    }
    return false;
  }

  for (const entry of pathEntries) {
    if (fs.existsSync(path.join(entry, trimmed))) {
      return true;
    }
  }
  return false;
}
