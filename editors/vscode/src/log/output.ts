import type * as vscode from "vscode";

export function createDoggoOutputChannel(vscodeAPI: typeof vscode): vscode.OutputChannel {
  return vscodeAPI.window.createOutputChannel("Doggo Language Server");
}
