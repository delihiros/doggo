import type * as vscode from "vscode";
import type {
  Executable,
  LanguageClientOptions
} from "vscode-languageclient/node";

import type { DoggoSettings } from "../config/settings";
import { resolveServerCommand } from "../serverCommand";
import { LanguageClientManager, ManagedClient } from "./clientManager";

interface RuntimeLanguageClientManagerOptions {
  output: vscode.OutputChannel;
  getSettings: () => DoggoSettings;
  getWorkspaceFolderPath: () => string | undefined;
  onStartError: (error: unknown) => Promise<void>;
}

export function resolveRuntimeServerOptions(
  settings: DoggoSettings,
  workspaceFolderPath: string | undefined
): { startupMessage: string; serverOptions: Executable } {
  const resolved = resolveServerCommand({
    configuredCommand: settings.lspCommand,
    configuredArgs: settings.lspArgs,
    configuredEnv: settings.lspEnv
  });
  return {
    startupMessage: `[doggo] starting language server (${resolved.source}): ${resolved.command} ${resolved.args.join(" ")}`,
    serverOptions: {
      command: resolved.command,
      args: resolved.args,
      options: {
        env: resolved.env,
        cwd: workspaceFolderPath
      }
    }
  };
}

export function createRuntimeLanguageClientManager(
  options: RuntimeLanguageClientManagerOptions
): LanguageClientManager {
  const { LanguageClient, Trace } = require("vscode-languageclient/node") as typeof import("vscode-languageclient/node");
  return new LanguageClientManager({
    createClient: () => {
      const settings = options.getSettings();
      const { startupMessage, serverOptions } = resolveRuntimeServerOptions(
        settings,
        options.getWorkspaceFolderPath()
      );
      const clientOptions: LanguageClientOptions = {
        documentSelector: [
          { scheme: "file", language: "doggo" }
        ],
        outputChannel: options.output,
        traceOutputChannel: options.output
      };
      const languageClient = new LanguageClient(
        "doggo-language-server",
        "Doggo Language Server",
        serverOptions,
        clientOptions
      );
      languageClient.setTrace(
        settings.traceServer === "messages"
          ? Trace.Messages
          : settings.traceServer === "verbose"
            ? Trace.Verbose
            : Trace.Off
      );

      const managedClient: ManagedClient = {
        start: async () => {
          await languageClient.start();
        },
        stop: async () => {
          await languageClient.stop();
        }
      };
      return {
        startupMessage,
        client: managedClient
      };
    },
    output: options.output,
    onStartError: options.onStartError
  });
}
