import assert from "node:assert/strict";
import test from "node:test";

import { evaluateIndentationRules } from "../indentDebug";

const config = {
  indentationRules: {
    increaseIndentPattern: "^\\s*(?:[^\\n;\"']*\\([^\\n;\"')]*$|[^\\n;\"']*\\[[^\\n;\"'\\]]*$|[^\\n;\"']*\\{[^\\n;\"'}]*$)",
    decreaseIndentPattern: "^\\s*[\\)\\]\\}]",
    indentNextLinePattern: "^\\s*\\((?:def|defn|defmacro|defonce|fn|let|letfn|loop|if|if-let|when|when-let|do|try|catch|doseq|for|ns|require|import|go|dosync|deftype|defrecord|defmulti|defmethod)\\b.*$"
  },
  onEnterRules: [
    {
      beforeText: "^\\s*.*[\\(\\[\\{][^\\)\\]\\}]*$",
      afterText: "^\\s*[\\)\\]\\}].*$",
      action: { indentAction: "indentOutdent" }
    },
    {
      beforeText: "^\\s*.*[\\)\\]\\}]\\s*$",
      action: { indentAction: "outdent" }
    },
    {
      beforeText: "^\\s*\\((?:def|defn|defmacro|defonce|fn|let|letfn|loop|if|if-let|when|when-let|do|try|catch|doseq|for|ns|require|import|go|dosync|deftype|defrecord|defmulti|defmethod)\\b.*$",
      action: { indentAction: "indent" }
    },
    {
      beforeText: "^\\s*\\([^\\)]*\\S\\s*$",
      action: { indentAction: "indent" }
    },
    {
      beforeText: "^\\s*.*[\\(\\[\\{]\\s*$",
      action: { indentAction: "indent" }
    }
  ]
};

test("matches generic operator/function call indentation", () => {
  const result = evaluateIndentationRules(config, "(+ 1", "");
  assert.equal(result.increaseIndent, true);
  assert.equal(result.indentNextLine, false);
  assert.ok(result.matchedOnEnter.some((match) => match.indentAction === "indent"));
});

test("matches defn indentation via special-form pattern", () => {
  const result = evaluateIndentationRules(config, "(defn fib [n]", "");
  assert.equal(result.increaseIndent, true);
  assert.equal(result.indentNextLine, true);
  assert.ok(result.matchedOnEnter.some((match) => match.indentAction === "indent"));
});

test("matches indentOutdent when right delimiter exists on next line", () => {
  const result = evaluateIndentationRules(config, "(let [x 1", ")");
  assert.ok(result.matchedOnEnter.some((match) => match.indentAction === "indentOutdent"));
});

test("matches outdent when line ends with closing delimiters", () => {
  const numericClose = evaluateIndentationRules(config, "2)", "");
  assert.ok(numericClose.matchedOnEnter.some((match) => match.indentAction === "outdent"));

  const mixedClose = evaluateIndentationRules(config, "foo])", "");
  assert.ok(mixedClose.matchedOnEnter.some((match) => match.indentAction === "outdent"));

  const nestedClose = evaluateIndentationRules(config, "bar}}", "");
  assert.ok(nestedClose.matchedOnEnter.some((match) => match.indentAction === "outdent"));
});

test("detects decrease indentation for closing delimiter", () => {
  const result = evaluateIndentationRules(config, ")", "");
  assert.equal(result.decreaseIndent, true);
});
