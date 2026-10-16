import assert from "node:assert/strict";
import test from "node:test";
import { compatibilityErrors } from "./check-vscode-compatibility.mjs";

function fixture(supported = "^1.90.0", required = "^1.82.0", types = "1.90.0") {
  const manifest = { engines: { vscode: supported }, devDependencies: { "@types/vscode": types } };
  const lock = { packages: {
    "": structuredClone(manifest),
    "node_modules/vscode-languageclient": { engines: { vscode: required } },
    "node_modules/@types/vscode": { version: types, dev: true }
  } };
  return [manifest, lock];
}

test("accepts dependencies compatible with the full supported VS Code range", () => {
  assert.deepEqual(compatibilityErrors(...fixture()), []);
  assert.deepEqual(compatibilityErrors(...fixture("^1.91.0", "^1.91.0", "1.91.0")), []);
});

test("rejects PR #3's higher runtime minimum and narrower runtime ranges", () => {
  assert.match(compatibilityErrors(...fixture("^1.90.0", "^1.91.0"))[0], /requires VS Code/);
  assert.match(compatibilityErrors(...fixture("^1.90.0", ">=1.90.0 <1.100.0"))[0], /requires VS Code/);
});

test("rejects API types newer than the supported minimum or floating declarations", () => {
  assert.match(compatibilityErrors(...fixture("^1.90.0", "^1.82.0", "1.109.0"))[0], /Pin @types/);
  const [manifest, lock] = fixture();
  manifest.devDependencies["@types/vscode"] = "^1.90.0";
  assert.match(compatibilityErrors(manifest, lock)[0], /Pin @types/);
});

test("rejects stale lockfile engine declarations", () => {
  const [manifest, lock] = fixture();
  lock.packages[""].engines.vscode = "^1.91.0";
  assert.match(compatibilityErrors(manifest, lock)[0], /differs/);
});
