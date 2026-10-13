import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";

type OnEnterRule = {
  beforeText: string;
  afterText?: string;
  action?: {
    indentAction?: string;
  };
};

type LanguageConfiguration = {
  indentationRules?: {
    increaseIndentPattern?: string;
    decreaseIndentPattern?: string;
    indentNextLinePattern?: string;
  };
  onEnterRules?: OnEnterRule[];
};

const configurationPath = path.resolve(__dirname, "../../language-configuration.json");

async function loadConfiguration(): Promise<LanguageConfiguration> {
  const raw = await fs.readFile(configurationPath, "utf8");
  return JSON.parse(raw) as LanguageConfiguration;
}

function matches(pattern: string | undefined, text: string): boolean {
  if (pattern === undefined) {
    return false;
  }
  return new RegExp(pattern).test(text);
}

function matchingIndentActions(config: LanguageConfiguration, beforeText: string, afterText = ""): string[] {
  const rules = config.onEnterRules ?? [];
  const actions: string[] = [];
  for (const rule of rules) {
    if (!matches(rule.beforeText, beforeText)) {
      continue;
    }
    if (rule.afterText !== undefined && !matches(rule.afterText, afterText)) {
      continue;
    }
    if (rule.action?.indentAction !== undefined) {
      actions.push(rule.action.indentAction);
    }
  }
  return actions;
}

test("contains indentation configuration", async () => {
  const config = await loadConfiguration();

  assert.ok(config.indentationRules?.increaseIndentPattern);
  assert.ok(config.indentationRules?.decreaseIndentPattern);
  assert.ok(config.indentationRules?.indentNextLinePattern);
  assert.ok((config.onEnterRules ?? []).length > 0);
});

test("indents after common block forms", async () => {
  const config = await loadConfiguration();
  const specialFormPattern = config.indentationRules?.indentNextLinePattern;

  assert.equal(matches(specialFormPattern, "(defn fib [n]"), true);
  assert.equal(matches(specialFormPattern, "(if (<= n 1)"), true);
  assert.equal(matches(specialFormPattern, "(println n)"), false);

  const ifActions = matchingIndentActions(config, "(if (<= n 1)");
  assert.ok(ifActions.includes("indent"));
});

test("indents for generic function/operator calls like Clojure style", async () => {
  const config = await loadConfiguration();
  const increasePattern = config.indentationRules?.increaseIndentPattern;

  const plusActions = matchingIndentActions(config, "(+ 1");
  assert.ok(plusActions.includes("indent"));
  assert.equal(matches(increasePattern, "(+ 1"), true);

  const mapActions = matchingIndentActions(config, "(map inc xs");
  assert.ok(mapActions.includes("indent"));
  assert.equal(matches(increasePattern, "(map inc xs"), true);
  assert.equal(matches(increasePattern, "(defn fib [n]"), true);
});

test("supports indentOutdent when newline is between open and close delimiters", async () => {
  const config = await loadConfiguration();

  const actions = matchingIndentActions(config, "(let [x 1", ")");
  assert.ok(actions.includes("indentOutdent"));
});

test("outdents after closing delimiters on current line", async () => {
  const config = await loadConfiguration();

  assert.ok(matchingIndentActions(config, "2)").includes("outdent"));
  assert.ok(matchingIndentActions(config, "foo])").includes("outdent"));
  assert.ok(matchingIndentActions(config, "bar}}").includes("outdent"));
  assert.ok(matchingIndentActions(config, ")").includes("outdent"));
});

test("decreases indentation on closing delimiters", async () => {
  const config = await loadConfiguration();
  const pattern = config.indentationRules?.decreaseIndentPattern;

  assert.equal(matches(pattern, ")"), true);
  assert.equal(matches(pattern, "  ]"), true);
  assert.equal(matches(pattern, "    }"), true);
  assert.equal(matches(pattern, "  value"), false);
});
