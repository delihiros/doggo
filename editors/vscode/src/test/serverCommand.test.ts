import assert from "node:assert/strict";
import test from "node:test";

import { resolveDoggoCommand, resolveServerCommand } from "../serverCommand";

test("defaults LSP to doggo exec through PATH", () => {
  const resolved = resolveServerCommand({
    configuredCommand: "",
    configuredArgs: [],
    configuredEnv: {}
  });

  assert.equal(resolved.command, "doggo");
  assert.deepEqual(resolved.args, ["exec", "--", "lsp"]);
  assert.equal(resolved.source, "path");
});

test("defaults REPL to doggo exec through PATH", () => {
  const resolved = resolveDoggoCommand({
    subcommand: "repl",
    configuredCommand: "",
    configuredArgs: [],
    configuredEnv: {}
  });

  assert.equal(resolved.command, "doggo");
  assert.deepEqual(resolved.args, ["exec", "--", "repl"]);
  assert.equal(resolved.source, "path");
});

test("configured command invokes the direct subcommand", () => {
  const resolved = resolveServerCommand({
    configuredCommand: "custom-doggo",
    configuredArgs: ["--trace"],
    configuredEnv: {
      DOGGO_REPL_HISTORY: "/tmp/history"
    }
  });

  assert.equal(resolved.command, "custom-doggo");
  assert.deepEqual(resolved.args, ["lsp", "--trace"]);
  assert.equal(resolved.source, "config");
  assert.equal(resolved.env.DOGGO_REPL_HISTORY, "/tmp/history");
});
