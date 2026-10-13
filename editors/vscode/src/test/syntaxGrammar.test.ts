import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import { INITIAL, Registry, parseRawGrammar, type IGrammar, type IRawGrammar } from "vscode-textmate";
import { loadWASM, OnigScanner, OnigString } from "vscode-oniguruma";

const grammarPath = path.resolve(__dirname, "../../syntaxes/doggo.tmLanguage.json");
const onigWasmPath = path.resolve(__dirname, "../../node_modules/vscode-oniguruma/release/onig.wasm");

let grammarPromise: Promise<IGrammar> | undefined;

function getGrammar(): Promise<IGrammar> {
  if (grammarPromise !== undefined) {
    return grammarPromise;
  }
  grammarPromise = loadGrammar();
  return grammarPromise;
}

async function loadGrammar(): Promise<IGrammar> {
  const wasmBuffer = await fs.readFile(onigWasmPath);
  const wasmBytes = wasmBuffer.buffer.slice(wasmBuffer.byteOffset, wasmBuffer.byteOffset + wasmBuffer.byteLength);
  await loadWASM(wasmBytes);

  const registry = new Registry({
    onigLib: Promise.resolve({
      createOnigScanner(patterns: string[]): OnigScanner {
        return new OnigScanner(patterns);
      },
      createOnigString(content: string): OnigString {
        return new OnigString(content);
      }
    }),
    loadGrammar: async (scopeName: string): Promise<IRawGrammar | null> => {
      if (scopeName !== "source.doggo") {
        return null;
      }
      const content = await fs.readFile(grammarPath, "utf8");
      return parseRawGrammar(content, grammarPath);
    }
  });

  const grammar = await registry.loadGrammar("source.doggo");
  assert.ok(grammar, "failed to load source.doggo grammar");
  return grammar;
}

async function scopesAt(line: string, charIndex: number): Promise<string[]> {
  const grammar = await getGrammar();
  const tokenized = grammar.tokenizeLine(line, INITIAL);
  const token = tokenized.tokens.find((entry) => charIndex >= entry.startIndex && charIndex < entry.endIndex);
  assert.ok(token, `no token found at index=${charIndex} in line=${line}`);
  return token.scopes;
}

async function expectScope(line: string, index: number, expectedScope: string): Promise<void> {
  const scopes = await scopesAt(line, index);
  assert.ok(scopes.includes(expectedScope), `scope ${expectedScope} not found in ${JSON.stringify(scopes)} for line ${line}`);
}

test("highlights comment", async () => {
  await expectScope("; comment", 2, "comment.line.semicolon.doggo");
});

test("highlights string and regex", async () => {
  await expectScope(`"str"`, 1, "string.quoted.double.doggo");
  await expectScope(`#"re"`, 2, "string.regexp.doggo");
});

test("highlights numbers and keyword literals", async () => {
  await expectScope("-1", 0, "constant.numeric.doggo");
  await expectScope("1.2e3", 0, "constant.numeric.doggo");
  await expectScope(":ns/name", 0, "constant.other.keyword.doggo");
});

test("highlights list head symbols and definitions", async () => {
  await expectScope("(if (<= n 1) n)", 1, "keyword.control.doggo");
  await expectScope("(if (<= n 1) n)", 5, "entity.name.function.doggo");
  await expectScope("(defn fib [n] n)", 1, "keyword.control.doggo");
  await expectScope("(defn fib [n] n)", 6, "entity.name.function.definition.doggo");
  await expectScope("(def value 1)", 1, "keyword.control.doggo");
  await expectScope("(def value 1)", 5, "variable.other.definition.doggo");
});

test("highlights reader macro operators", async () => {
  await expectScope("'x", 0, "keyword.operator.reader.doggo");
  await expectScope("`x", 0, "keyword.operator.reader.doggo");
  await expectScope("~x", 0, "keyword.operator.reader.doggo");
  await expectScope("~@xs", 0, "keyword.operator.reader.doggo");
  await expectScope("@x", 0, "keyword.operator.reader.doggo");
  await expectScope("#'v", 0, "keyword.operator.reader.doggo");
  await expectScope("#_x", 0, "keyword.operator.reader.doggo");
  await expectScope("^:private", 0, "keyword.operator.reader.doggo");
});
