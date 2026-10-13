import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

// Resolve from the language client so this covers the dependency bundled into
// the extension, even when packaging tools use a different minimatch version.
const languageClientRequire = createRequire(
  require.resolve("vscode-languageclient/package.json"),
);
const runtimeMinimatch = languageClientRequire("minimatch") as {
  (path: string, pattern: string): boolean;
  braceExpand(pattern: string): string[];
};

test("language client glob expansion handles deeply nested brace patterns", () => {
  // GHSA-qhr7-859c-m2p7: this 6,403-character payload overflowed the stack
  // in the previously bundled brace-expansion 2.1.0.
  const pattern = "{".repeat(3200) + "a,b" + "}".repeat(3200);

  let expanded: string[] = [];
  assert.doesNotThrow(() => {
    expanded = runtimeMinimatch.braceExpand(pattern);
  });
  assert.ok(expanded.length >= 1 && expanded.length <= 2);
});

test("language client glob matching retains ordinary brace alternatives", () => {
  const pattern = "**/*.{dog,doggo}";

  assert.equal(runtimeMinimatch("src/example.dog", pattern), true);
  assert.equal(runtimeMinimatch("src/example.doggo", pattern), true);
  assert.equal(runtimeMinimatch("src/example.txt", pattern), false);
  assert.deepEqual(runtimeMinimatch.braceExpand(pattern), [
    "**/*.dog",
    "**/*.doggo",
  ]);
});
