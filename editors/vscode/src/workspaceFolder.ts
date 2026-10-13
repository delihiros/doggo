import type * as vscode from "vscode";

export function getPreferredWorkspaceFolder(
  workspace: typeof vscode.workspace,
  window: typeof vscode.window,
  documentURI?: vscode.Uri
): vscode.WorkspaceFolder | undefined {
  if (documentURI !== undefined) {
    const fromDocument = workspace.getWorkspaceFolder(documentURI);
    if (fromDocument !== undefined) {
      return fromDocument;
    }
  }

  const activeEditor = window.activeTextEditor;
  if (activeEditor !== undefined) {
    const fromActiveEditor = workspace.getWorkspaceFolder(activeEditor.document.uri);
    if (fromActiveEditor !== undefined) {
      return fromActiveEditor;
    }
  }

  const workspaceFolders = workspace.workspaceFolders;
  if (workspaceFolders === undefined || workspaceFolders.length === 0) {
    return undefined;
  }

  return workspaceFolders[0];
}
