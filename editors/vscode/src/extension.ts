import * as vscode from "vscode";

import { registerDoggoCommands } from "./commands/registerCommands";
import { readDoggoSettings } from "./config/settings";
import { IndentController } from "./indent/indentController";
import { createRuntimeLanguageClientManager } from "./lsp/runtime";
import { createDoggoOutputChannel } from "./log/output";
import { prepareSelectionsForRepl } from "./replCommand";
import { ReplTerminalManager } from "./repl/replTerminalManager";
import { getPreferredWorkspaceFolder } from "./workspaceFolder";

let lspManager:
  | ReturnType<typeof createRuntimeLanguageClientManager>
  | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  const outputChannel = createDoggoOutputChannel(vscode);
  context.subscriptions.push(outputChannel);

  const getSettings = () => readDoggoSettings(vscode.workspace.getConfiguration("doggo"));
  const getWorkspaceFolderPath = (documentURI?: vscode.Uri): string | undefined => {
    const folder = getPreferredWorkspaceFolder(vscode.workspace, vscode.window, documentURI);
    return folder?.uri.fsPath;
  };

  const indentController = new IndentController({
    extensionPath: context.extensionPath,
    output: outputChannel,
    traceEnabled: getSettings().indentTrace
  });

  lspManager = createRuntimeLanguageClientManager({
    output: outputChannel,
    getSettings,
    getWorkspaceFolderPath: () => getWorkspaceFolderPath(),
    onStartError: async () => {
      await showServerStartError(outputChannel);
    }
  });

  const replManager = new ReplTerminalManager({
    output: outputChannel,
    createTerminal: (options) => vscode.window.createTerminal(options),
    resolveWorkspaceFolderPath: (documentURI) => getWorkspaceFolderPath(documentURI),
    onStartError: async () => {
      await showReplStartError(outputChannel);
    }
  });

  registerDoggoCommands(vscode.commands, context.subscriptions, {
    restartLanguageServer: async () => {
      await lspManager?.restart();
      void vscode.window.showInformationMessage("Doggo language server restarted.");
    },
    showLanguageServerOutput: () => {
      outputChannel.show(true);
    },
    runSelectionInRepl: async () => {
      await runSelectionInRepl(replManager, getSettings());
    },
    diagnoseIndentation: async () => {
      const editor = vscode.window.activeTextEditor;
      if (editor === undefined) {
        void vscode.window.showWarningMessage("No active editor found.");
        return;
      }
      indentController.runDiagnostics(editor);
      outputChannel.show(true);
    }
  });

  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration(async (event) => {
      if (!event.affectsConfiguration("doggo.lsp") && !event.affectsConfiguration("doggo.trace.server")) {
        return;
      }
      const action = await vscode.window.showInformationMessage(
        "Doggo language server settings changed. Restart the language server to apply the new settings.",
        "Restart"
      );
      if (action === "Restart") {
        await lspManager?.restart();
      }
    })
  );

  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (!event.affectsConfiguration("doggo.debug.indentTrace")) {
        return;
      }
      indentController.setTraceEnabled(getSettings().indentTrace);
    })
  );

  context.subscriptions.push(
    vscode.workspace.onDidChangeTextDocument((event) => {
      indentController.traceIndentationChange(event);
    })
  );

  context.subscriptions.push(
    vscode.window.onDidCloseTerminal((closedTerminal) => {
      replManager.handleDidCloseTerminal(closedTerminal);
    })
  );

  void lspManager.start();
}

export async function deactivate(): Promise<void> {
  await lspManager?.stop();
  lspManager = undefined;
}

async function runSelectionInRepl(
  replManager: ReplTerminalManager,
  settings: ReturnType<typeof readDoggoSettings>
): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (editor === undefined) {
    void vscode.window.showWarningMessage("No active editor found.");
    return;
  }

  const selections = editor.selections.map((selection) => ({
    start: editor.document.offsetAt(selection.start),
    end: editor.document.offsetAt(selection.end),
    text: editor.document.getText(selection)
  }));
  const prepared = prepareSelectionsForRepl(editor.document.languageId, selections);
  if (!prepared.ok) {
    if (prepared.reason === "not-doggo") {
      void vscode.window.showWarningMessage("Run Selection in REPL is available only for Doggo files.");
      return;
    }
    void vscode.window.showWarningMessage("No non-empty selection found.");
    return;
  }

  const terminal = await replManager.ensureTerminal(editor.document.uri, settings);
  if (terminal === undefined) {
    return;
  }

  terminal.show(true);
  for (const text of prepared.texts) {
    terminal.sendText(text, true);
  }
}

async function showServerStartError(outputChannel: vscode.OutputChannel): Promise<void> {
  const action = await vscode.window.showErrorMessage(
    "Doggo language server failed to start. Run `doggo build`, then `doggo exec -- lsp`, or set `doggo.lsp.command`.",
    "Open Settings",
    "Show Output"
  );
  if (action === "Open Settings") {
    await vscode.commands.executeCommand("workbench.action.openSettings", "doggo.lsp.command");
  } else if (action === "Show Output") {
    outputChannel.show(true);
  }
}

async function showReplStartError(outputChannel: vscode.OutputChannel): Promise<void> {
  const action = await vscode.window.showErrorMessage(
    "Doggo REPL failed to start. Run `doggo build`, then `doggo exec -- repl`, or set `doggo.lsp.command`.",
    "Open Settings",
    "Show Output"
  );
  if (action === "Open Settings") {
    await vscode.commands.executeCommand("workbench.action.openSettings", "doggo.lsp.command");
  } else if (action === "Show Output") {
    outputChannel.show(true);
  }
}
