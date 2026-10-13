import assert from "node:assert/strict";
import test from "node:test";

import { DOGGO_COMMAND_IDS, registerDoggoCommands } from "../commands/registerCommands";

test("registerDoggoCommands registers all public command IDs", () => {
  const registered: string[] = [];
  const subscriptions: Array<{ dispose: () => void }> = [];

  registerDoggoCommands(
    {
      registerCommand: (id, callback) => {
        registered.push(id);
        assert.equal(typeof callback, "function");
        return {
          dispose: () => undefined
        };
      }
    },
    subscriptions,
    {
      restartLanguageServer: async () => undefined,
      showLanguageServerOutput: () => undefined,
      runSelectionInRepl: async () => undefined,
      diagnoseIndentation: async () => undefined
    }
  );

  assert.deepEqual(registered.sort(), [...DOGGO_COMMAND_IDS].sort());
  assert.equal(subscriptions.length, DOGGO_COMMAND_IDS.length);
});

test("registerDoggoCommands wires callbacks", async () => {
  const callbacks = new Map<string, (...args: unknown[]) => unknown>();
  const calls: string[] = [];

  registerDoggoCommands(
    {
      registerCommand: (id, callback) => {
        callbacks.set(id, callback);
        return {
          dispose: () => undefined
        };
      }
    },
    [],
    {
      restartLanguageServer: async () => {
        calls.push("restart");
      },
      showLanguageServerOutput: () => {
        calls.push("show");
      },
      runSelectionInRepl: async () => {
        calls.push("repl");
      },
      diagnoseIndentation: async () => {
        calls.push("diagnose");
      }
    }
  );

  await callbacks.get("doggo.restartLanguageServer")?.();
  callbacks.get("doggo.showLanguageServerOutput")?.();
  await callbacks.get("doggo.runSelectionInRepl")?.();
  await callbacks.get("doggo.diagnoseIndentation")?.();

  assert.deepEqual(calls, ["restart", "show", "repl", "diagnose"]);
});
