import assert from "node:assert/strict";
import test from "node:test";

import { prepareSelectionsForRepl } from "../replCommand";

test("returns not-doggo for non-doggo files", () => {
  const prepared = prepareSelectionsForRepl("plaintext", [
    { start: 10, end: 20, text: "(+ 1 2)" }
  ]);

  assert.deepEqual(prepared, { ok: false, reason: "not-doggo" });
});

test("returns no-selection when all selections are empty", () => {
  const prepared = prepareSelectionsForRepl("doggo", [
    { start: 0, end: 0, text: "" },
    { start: 5, end: 5, text: "" }
  ]);

  assert.deepEqual(prepared, { ok: false, reason: "no-selection" });
});

test("sorts multi selections by document offset and keeps all non-empty selections", () => {
  const prepared = prepareSelectionsForRepl("doggo", [
    { start: 50, end: 60, text: "(+ 2 3)" },
    { start: 10, end: 20, text: "(def x 1)" },
    { start: 30, end: 30, text: "" },
    { start: 30, end: 45, text: "(println x)" }
  ]);

  assert.deepEqual(prepared, {
    ok: true,
    texts: [
      "(def x 1)",
      "(println x)",
      "(+ 2 3)"
    ]
  });
});
