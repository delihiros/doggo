import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import test from "node:test";

test("VSIX file selection excludes internal review and retains public files", () => {
  const require = createRequire(import.meta.url);
  const output = execFileSync(process.execPath, [require.resolve("@vscode/vsce/vsce"), "ls"], {
    cwd: fileURLToPath(new URL("..", import.meta.url)),
    encoding: "utf8",
    windowsHide: true
  });
  const files = new Set(output.trim().split(/\r?\n/).map((name) => name.replaceAll("\\", "/")));
  assert.ok(!files.has("DEPENDENCY_REVIEW.md"), "Internal dependency review must not enter the VSIX");
  for (const name of ["README.md", "LICENSE", "THIRD_PARTY_NOTICES", "language-configuration.json", "media/icon.png", "syntaxes/doggo.tmLanguage.json"]) {
    assert.ok(files.has(name), `VSIX must retain ${name}`);
  }
});
