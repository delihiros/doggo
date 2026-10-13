import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { runInNewContext } from "node:vm";

const checkNodeSource = readFileSync(
  path.resolve(__dirname, "../../scripts/check-node-version.js"),
  "utf8",
);

// Execute the actual CLI guard with each Node version. A VM supplies the
// otherwise immutable process.versions.node without requiring old Node installs.
for (const [version, supported] of [
  ["20.18.1", false],
  ["22.0.0", false],
  ["22.11.0", false],
  ["22.11.99", false],
  ["22.12.0", true],
  ["22.12.1", true],
  ["23.0.0", true],
  ["24.4.1", true],
] as const) {
  test(`release Node guard ${supported ? "accepts" : "rejects"} ${version}`, () => {
    const exits: number[] = [];
    const errors: string[] = [];
    runInNewContext(checkNodeSource, {
      process: {
        versions: { node: version },
        exit(code: number) {
          exits.push(code);
        },
      },
      console: {
        error(message: string) {
          errors.push(message);
        },
      },
    });

    if (supported) {
      assert.deepEqual(exits, []);
      assert.deepEqual(errors, []);
    } else {
      assert.deepEqual(exits, [1]);
      assert.deepEqual(errors, [
        `Node ${version} is unsupported. Use Node >=22.12.0 for Doggo release tooling.`,
      ]);
    }
  });
}
