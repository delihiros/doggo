import assert from "node:assert/strict";
import test from "node:test";

import { resolveRuntimeServerOptions } from "../lsp/runtime";

test("runtime launches doggo exec from the workspace directory", () => {
  const resolved = resolveRuntimeServerOptions({
    lspCommand: "",
    lspArgs: [],
    lspEnv: {},
    traceServer: "off",
    indentTrace: false
  }, "/workspace/project");

  assert.equal(resolved.serverOptions.command, "doggo");
  assert.deepEqual(resolved.serverOptions.args, ["exec", "--", "lsp"]);
  assert.equal(resolved.serverOptions.options?.cwd, "/workspace/project");
});
