import assert from "node:assert/strict";
import test from "node:test";

import { DoggoSettings, parseDoggoSettings } from "../config/settings";

function getDefaultSettings(overrides: Partial<DoggoSettings> = {}): DoggoSettings {
  return {
    lspCommand: "",
    lspArgs: [],
    lspEnv: {},
    traceServer: "off",
    indentTrace: false,
    ...overrides
  };
}

test("parseDoggoSettings trims command and filters blank args", () => {
  const parsed = parseDoggoSettings({
    lspCommand: "  doggo-custom  ",
    lspArgs: ["", " --alpha ", " ", "\t", "--beta"],
    lspEnv: {},
    traceServer: "off",
    indentTrace: false
  });

  assert.equal(parsed.lspCommand, "doggo-custom");
  assert.deepEqual(parsed.lspArgs, ["--alpha", "--beta"]);
});

test("parseDoggoSettings normalizes env keys and values", () => {
  const parsed = parseDoggoSettings({
    lspCommand: "",
    lspArgs: [],
    lspEnv: {
      " FOO ": "bar",
      "": "x",
      "DOGGO_NUM": 123
    },
    traceServer: "off",
    indentTrace: false
  });

  assert.deepEqual(parsed.lspEnv, {
    FOO: "bar",
    DOGGO_NUM: "123"
  });
});

test("parseDoggoSettings falls back to defaults for invalid values", () => {
  const parsed = parseDoggoSettings({
    lspCommand: "",
    lspArgs: "not-array",
    lspEnv: "not-object",
    traceServer: "invalid",
    indentTrace: "yes"
  } as unknown as DoggoSettings);

  assert.deepEqual(parsed, getDefaultSettings());
});
