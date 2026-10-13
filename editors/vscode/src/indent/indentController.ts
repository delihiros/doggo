import fs from "node:fs";
import path from "node:path";

import * as vscode from "vscode";

import { evaluateIndentationRules, LanguageIndentConfiguration } from "../indentDebug";

interface IndentControllerOptions {
  extensionPath: string;
  output: vscode.OutputChannel;
  traceEnabled: boolean;
}

export class IndentController {
  private readonly output: vscode.OutputChannel;
  private readonly languageConfiguration: LanguageIndentConfiguration;
  private traceEnabled: boolean;

  constructor(options: IndentControllerOptions) {
    this.output = options.output;
    this.traceEnabled = options.traceEnabled;
    this.languageConfiguration = loadLanguageConfiguration(options.extensionPath, options.output);
  }

  setTraceEnabled(enabled: boolean): void {
    this.traceEnabled = enabled;
    this.output.appendLine(`[doggo][indent-trace] doggo.debug.indentTrace=${String(enabled)}`);
  }

  runDiagnostics(editor: vscode.TextEditor): void {
    const document = editor.document;
    const position = editor.selection.active;
    const lineText = document.lineAt(position.line).text;
    const beforeText = lineText.slice(0, position.character);
    const afterText = lineText.slice(position.character);
    const evaluation = evaluateIndentationRules(this.languageConfiguration, beforeText, afterText);
    const editorConfig = vscode.workspace.getConfiguration("editor", document.uri);

    this.output.appendLine("[doggo][indent] ===== Indentation Diagnostics =====");
    this.output.appendLine(`[doggo][indent] document=${document.uri.toString()}`);
    this.output.appendLine(`[doggo][indent] languageId=${document.languageId}`);
    this.output.appendLine(`[doggo][indent] cursor=${position.line + 1}:${position.character + 1}`);
    this.output.appendLine(`[doggo][indent] beforeText=${JSON.stringify(beforeText)}`);
    this.output.appendLine(`[doggo][indent] afterText=${JSON.stringify(afterText)}`);
    this.output.appendLine(`[doggo][indent] editor.autoIndent=${String(editorConfig.get("autoIndent"))}`);
    this.output.appendLine(`[doggo][indent] editor.detectIndentation=${String(editorConfig.get("detectIndentation"))}`);
    this.output.appendLine(`[doggo][indent] editor.insertSpaces=${String(editorConfig.get("insertSpaces"))}`);
    this.output.appendLine(`[doggo][indent] editor.tabSize=${String(editorConfig.get("tabSize"))}`);
    this.output.appendLine(`[doggo][indent] rule.increaseIndent=${String(evaluation.increaseIndent)}`);
    this.output.appendLine(`[doggo][indent] rule.decreaseIndent=${String(evaluation.decreaseIndent)}`);
    this.output.appendLine(`[doggo][indent] rule.indentNextLine=${String(evaluation.indentNextLine)}`);
    this.output.appendLine(`[doggo][indent] rule.onEnterMatches=${JSON.stringify(evaluation.matchedOnEnter)}`);
    this.output.appendLine(`[doggo][indent] traceEnabled=${String(this.traceEnabled)}`);
    this.output.appendLine("[doggo][indent] ===================================");
  }

  traceIndentationChange(event: vscode.TextDocumentChangeEvent): void {
    if (!this.traceEnabled) {
      return;
    }
    if (event.document.languageId !== "doggo") {
      return;
    }
    if (event.contentChanges.length === 0) {
      return;
    }

    for (const change of event.contentChanges) {
      if (!change.text.includes("\n")) {
        continue;
      }

      const lineIndex = Math.min(change.range.start.line, Math.max(event.document.lineCount - 1, 0));
      const beforeText = event.document.lineAt(lineIndex).text;
      const afterLineIndex = Math.min(lineIndex + 1, Math.max(event.document.lineCount - 1, 0));
      const afterText = event.document.lineAt(afterLineIndex).text;
      const evaluation = evaluateIndentationRules(this.languageConfiguration, beforeText, afterText);
      const insertedSuffix = change.text.split("\n").at(-1) ?? "";
      const insertedIndent = /^[\t ]*$/.test(insertedSuffix) ? insertedSuffix : "";

      this.output.appendLine(
        `[doggo][indent-trace] newline change at ${change.range.start.line + 1}:${change.range.start.character + 1}`
      );
      this.output.appendLine(`[doggo][indent-trace] inserted=${JSON.stringify(change.text)}`);
      this.output.appendLine(`[doggo][indent-trace] insertedIndent=${JSON.stringify(insertedIndent)}`);
      this.output.appendLine(`[doggo][indent-trace] beforeText=${JSON.stringify(beforeText)}`);
      this.output.appendLine(`[doggo][indent-trace] afterText=${JSON.stringify(afterText)}`);
      this.output.appendLine(`[doggo][indent-trace] matches=${JSON.stringify(evaluation)}`);
    }
  }
}

function loadLanguageConfiguration(
  extensionPath: string,
  output: vscode.OutputChannel
): LanguageIndentConfiguration {
  const configurationPath = path.join(extensionPath, "language-configuration.json");
  try {
    const content = fs.readFileSync(configurationPath, "utf8");
    const parsed = JSON.parse(content) as LanguageIndentConfiguration;
    output.appendLine(`[doggo][indent] loaded language configuration from ${configurationPath}`);
    return parsed;
  } catch (error) {
    output.appendLine(`[doggo][indent] failed to load language configuration: ${String(error)}`);
    return {};
  }
}
