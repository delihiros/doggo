package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
	toolinglint "doggo/internal/tooling/lint"
)

func TestV7MacroScopesRejectCapture(t *testing.T) {
	for _, tc := range []struct{ name, body, value string }{
		{"for let", "(first (for [x [0] :let [renamed 2]] target))", "7"},
		{"for let destructuring", "(first (for [x [0] :let [{:keys [renamed]} {:renamed 2}]] target))", "7"},
		{"for later predicate", "(first (for [x [0] :let [renamed 2] :when (= target 7)] x))", "0"},
		{"for while", "(first (for [renamed [2] :while (= target 7)] renamed))", "2"},
		{"dotimes", "(let [a (atom 0)] (dotimes [renamed 1] (reset! a target)) @a)", "7"},
		{"dotimes count", "(dotimes [renamed (do target 0)] nil)", "nil"},
		{"when first", "(when-first [renamed [2]] target)", "7"},
		{"when first later body", "(when-first [renamed [2]] nil target)", "7"},
		{"when let later body", "(when-let [renamed 2] nil target)", "7"},
		{"as arrow", "(as-> 2 renamed target)", "7"},
		{"ordinary for control", "(first (for [renamed [2]] target))", "7"},
		{"ordinary let control", "(let [renamed 2] target)", "7"},
		{"unshadowable let", "(let [let identity] (let [renamed 2] target))", "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns audit)\n(def target 7)\n" + tc.body + "\n"
			if before := runRenameTestSource(t, source); before != tc.value {
				t.Fatalf("before = %s, want %s", before, tc.value)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			assertRenameV5Rejected(t, source, actual, response)
		})
	}
}

func TestV7MacroScopesPreserveVisibility(t *testing.T) {
	for _, tc := range []struct{ name, body, value string }{
		{"collection before later binder", "(first (for [x [target] :let [renamed 2]] x))", "7"},
		{"sequential let initializer", "(first (for [x [0] :let [y target renamed 2]] y))", "7"},
		{"predicate before later binder", "(first (for [x [2] :when (= target 7) :let [renamed 3]] x))", "2"},
		{"when first initializer", "(when-first [renamed [target]] renamed)", "7"},
		{"as arrow initializer", "(as-> target renamed (+ renamed 1))", "8"},
		{"as arrow without steps", "(as-> target renamed)", "7"},
		{"qualified reference", "(first (for [renamed [2] :let [x 0]] audit/target))", "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns audit)\n(def target 7)\n" + tc.body + "\n"
			if before := runRenameTestSource(t, source); before != tc.value {
				t.Fatalf("before = %s, want %s", before, tc.value)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			if want := strings.ReplaceAll(source, "target", "renamed"); actual != want {
				t.Fatalf("rename = %q, want %q; response = %v", actual, want, response)
			}
			if after := runRenameTestSource(t, actual); after != tc.value {
				t.Fatalf("after = %s, want %s", after, tc.value)
			}
		})
	}
}

func TestV7MacroLocalCursorsCannotSelectGlobal(t *testing.T) {
	for _, body := range []string{
		"(first (for [x [0] :let [target 2]] target))",
		"(dotimes [target 1] target)",
		"(when-first [target [2]] nil target)",
		"(as-> 2 target target)",
	} {
		t.Run(body, func(t *testing.T) {
			source := "(ns audit)\n(def target 7)\n" + body + "\n"
			for _, offset := range []int{strings.Index(source, body) + strings.Index(body, "target"), strings.LastIndex(source, "target")} {
				position := byteOffsetToLSPPosition(source, offset)
				actual, response := renameTestSource(t, source, position.Line, position.Character)
				if actual != source {
					t.Fatalf("local cursor changed global: %q; response = %v", actual, response)
				}
			}
			if references := v7References(t, source, 1, 5); len(references) != 1 {
				t.Fatalf("local bindings leaked into global references: %v", references)
			}
		})
	}
}

func TestV7MacroHeadsResolveActualBindings(t *testing.T) {
	for _, tc := range []struct{ name, source, want, value string }{
		{"global for", "(ns audit)\n(def for (fn [x] x))\n(for 7)\n", "(ns audit)\n(def renamed (fn [x] x))\n(renamed 7)\n", "7"},
		{"local for arguments", "(ns audit)\n(def target 7)\n(let [for (fn [a b] a)] (for [target] nil))\n", "(ns audit)\n(def renamed 7)\n(let [for (fn [a b] a)] (for [renamed] nil))\n", "[7]"},
		{"emitted global for arguments", "(ns audit)\n(def target 7)\n(defmulti for (fn [x] :a))\n(defmethod for :a [x] x)\n(for [target])\n", "(ns audit)\n(def renamed 7)\n(defmulti for (fn [x] :a))\n(defmethod for :a [x] x)\n(for [renamed])\n", "[7]"},
		{"local renamed macro arguments", "(ns audit (:refer-clojure :rename {for collect}))\n(def target 7)\n(let [collect identity] (collect [target]))\n", "(ns audit (:refer-clojure :rename {for collect}))\n(def renamed 7)\n(let [collect identity] (collect [renamed]))\n", "[7]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if before := runRenameTestSource(t, tc.source); before != tc.value {
				t.Fatalf("before = %s, want %s", before, tc.value)
			}
			actual, response := renameTestSource(t, tc.source, 1, 5)
			if actual != tc.want {
				t.Fatalf("rename = %q, want %q; response = %v", actual, tc.want, response)
			}
			if references := v7References(t, tc.source, 1, 5); len(references) != 2 {
				t.Fatalf("evaluated call references missing: %v", references)
			}
			if after := runRenameTestSource(t, actual); after != tc.value {
				t.Fatalf("after = %s, want %s", after, tc.value)
			}
		})
	}
}

func TestV7DoseqIgnoresUnusedBindingOperands(t *testing.T) {
	source := "(ns audit)\n(def target 7)\n(let [a (atom 0)] (doseq [x [0] :let [target 2]] (reset! a target)) @a)\n"
	if before := runRenameTestSource(t, source); before != "7" {
		t.Fatalf("before = %s, want 7", before)
	}
	actual, response := renameTestSource(t, source, 1, 5)
	want := strings.Replace(strings.Replace(source, "(def target", "(def renamed", 1), "(reset! a target)", "(reset! a renamed)", 1)
	if actual != want {
		t.Fatalf("ignored doseq operands were evaluated: %q, want %q; response = %v", actual, want, response)
	}
	if after := runRenameTestSource(t, actual); after != "7" {
		t.Fatalf("after = %s, want 7", after)
	}
}

func TestV7CanonicalMacroHeadsProtectBindings(t *testing.T) {
	for _, tc := range []struct{ name, declaration, head string }{
		{"qualified", "(ns audit)", "doggo.core/for"},
		{"namespace alias", "(ns audit (:require [doggo.core :as c]))", "c/for"},
		{"require rename", "(ns audit (:require [doggo.core :refer [for] :rename {for collect}]))", "collect"},
		{"core rename", "(ns audit (:refer-clojure :rename {for collect}))", "collect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.declaration + "\n(def target 7)\n(first (" + tc.head + " [renamed [2] :let [x 0]] target))\n"
			if before := runRenameTestSource(t, source); before != "7" {
				t.Fatalf("before = %s, want 7", before)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			assertRenameV5Rejected(t, source, actual, response)
			localSource := strings.ReplaceAll(source, "renamed", "target")
			position := byteOffsetToLSPPosition(localSource, strings.LastIndex(localSource, "target"))
			actual, response = renameTestSource(t, localSource, position.Line, position.Character)
			if actual != localSource {
				t.Fatalf("canonical macro local selected global: %q; response = %v", actual, response)
			}
		})
	}
}

func TestV7CoreAliasesRejectDestinationCollision(t *testing.T) {
	for _, tc := range []struct{ source, body, value string }{
		{"inc", "[target (renamed 2)]", "[7 3]"},
		{"mapcat", "[target (renamed identity [[2]])]", "[7 (2)]"},
		{"for", "[target (renamed [] 2)]", "[7 [2]]"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			source := "(ns audit (:refer-clojure :rename {" + tc.source + " renamed}))\n(def target 7)\n" + tc.body + "\n"
			if before := runRenameTestSource(t, source); before != tc.value {
				t.Fatalf("before = %s, want %s", before, tc.value)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			assertRenameV5Rejected(t, source, actual, response)
		})
	}
}

func TestV7CoreAliasDiagnosticsAndNoncollision(t *testing.T) {
	for _, tc := range []struct{ source, body, value string }{
		{"inc", "[target (localAlias 2)]", "[7 3]"},
		{"mapcat", "[target (localAlias identity [[2]])]", "[7 (2)]"},
		{"for", "[target (localAlias [x [2]] x)]", "[7 (2)]"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			source := "(ns audit (:refer-clojure :rename {" + tc.source + " localAlias}))\n(def target 7)\n" + tc.body + "\n"
			if before := runRenameTestSource(t, source); before != tc.value {
				t.Fatalf("before = %s, want %s", before, tc.value)
			}
			uri, messages := v7Messages(t, source, nil)
			diagnostic := findNotification(t, messages, lspMethodPublishDiagnostics)
			params := diagnostic["params"].(map[string]any)
			if diagnostics := params["diagnostics"].([]any); len(diagnostics) != 0 {
				t.Fatalf("valid core alias has diagnostics for %s: %v", uri, diagnostics)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			if want := strings.ReplaceAll(source, "target", "renamed"); actual != want {
				t.Fatalf("unrelated alias changed: %q, want %q; response = %v", actual, want, response)
			}
			if after := runRenameTestSource(t, actual); after != tc.value {
				t.Fatalf("after = %s, want %s", after, tc.value)
			}
		})
	}
}

func TestV7BuiltinAliasHasNoEditableDefinition(t *testing.T) {
	source := "(ns audit (:refer-clojure :rename {inc localAlias}))\n(localAlias 2)\n"
	uri, messages := v7Messages(t, source, func(uri string) []map[string]any {
		return []map[string]any{
			req(2, lspMethodDefinition, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 2}}),
			req(3, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 2}, "newName": "renamed"}),
		}
	})
	if result := findResponseByID(t, messages, 2)["result"].([]any); len(result) != 0 {
		t.Fatalf("builtin alias fabricated a definition: %v", result)
	}
	response := findResponseByID(t, messages, 3)
	if actual := applyRenameTestEdits(t, source, uri, response); actual != source {
		t.Fatalf("builtin alias was edited: %q; response = %v", actual, response)
	}
}

func TestV7MacroHeadResolutionFollowsCompilerTiming(t *testing.T) {
	for _, tc := range []struct{ name, source, value string }{
		{"later namespace rename", "(ns audit)\n(def target 7)\n(def earlier (first (for [renamed [2]] target)))\n(ns audit (:refer-clojure :rename {for collect}))\nearlier\n", "7"},
		{"later namespace exclusion", "(ns audit)\n(def target 7)\n(def earlier (first (for [renamed [2]] target)))\n(ns audit (:refer-clojure :exclude [for]))\nearlier\n", "7"},
		{"later global function", "(ns audit)\n(def target 7)\n(def earlier (first (for [renamed [2]] target)))\n(def for identity)\nearlier\n", "7"},
		{"shadowing def initializer", "(ns audit)\n(def target 7)\n(def for (first (for [renamed [2]] target)))\nfor\n", "7"},
		{"shadowing defn body", "(ns audit)\n(def target 7)\n(defn for [] (first (for [renamed [2]] target)))\n(for)\n", "7"},
		{"function before future definition", "(ns audit)\n(def target 7)\n(defn read-target [] (first (for [renamed [2]] target)))\n(def for identity)\n(read-target)\n", "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if before := runRenameTestSource(t, tc.source); before != tc.value {
				t.Fatalf("compiler result = %s, want %s", before, tc.value)
			}
			t.Logf("fresh compiler result = %s", tc.value)
			actual, response := renameTestSource(t, tc.source, 1, 5)
			assertRenameV5Rejected(t, tc.source, actual, response)
		})
	}
}

func TestV7MacroTimingDiagnostics(t *testing.T) {
	for _, source := range []string{
		"(ns audit)\n(def target 7)\n(def earlier (first (for [renamed [2]] target)))\n(ns audit (:refer-clojure :rename {for collect}))\nearlier\n",
		"(ns audit)\n(def target 7)\n(def earlier (first (for [renamed [2]] target)))\n(def for identity)\nearlier\n",
		"(ns audit)\n(def target 7)\n(def for (first (for [renamed [2]] target)))\nfor\n",
	} {
		if value := runRenameTestSource(t, source); value != "7" {
			t.Fatalf("compiler result = %s, want 7", value)
		}
		_, messages := v7Messages(t, source, nil)
		diagnostic := findNotification(t, messages, lspMethodPublishDiagnostics)
		params := diagnostic["params"].(map[string]any)
		if diagnostics := params["diagnostics"].([]any); len(diagnostics) != 0 {
			t.Errorf("later declaration changed earlier macro diagnostics: %v", diagnostics)
		}
	}
}

func TestV7MacroDiagnosticsStartBeforeFirstNamespace(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "foreign.dog"), "(ns audit)\n(def for identity)\n")
	source := "(def target 7)\n(def earlier (first (for [renamed [2]] target)))\n(ns audit)\nuser/earlier\n"
	if value := runRenameTestSourceInProject(t, root, source); value != "7" {
		t.Fatalf("compiler result = %s, want 7", value)
	}
	uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": source, "version": 1}}),
		req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 5}, "newName": "renamed"}),
		notify(lspMethodExit, map[string]any{}),
	})
	diagnostic := findNotification(t, messages, lspMethodPublishDiagnostics)
	params := diagnostic["params"].(map[string]any)
	if diagnostics := params["diagnostics"].([]any); len(diagnostics) != 0 {
		t.Errorf("later namespace changed default-user diagnostics: %v", diagnostics)
	}
	response := findResponseByID(t, messages, 2)
	assertRenameV5Rejected(t, source, applyRenameTestEdits(t, source, uri, response), response)
}

func TestV7StandaloneLintPreservesExplicitNamespace(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "foreign.dog"), "(ns user)\n(def target 7)\n(ns audit)\n(def for identity)\n(def target 7)\n")
	idx, err := toolindex.BuildWorkspaceIndex(toolindex.BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, source, namespace, setup, value string }{
		{"explicit context", "(for [target])", "audit", "(ns audit)\n(def for identity)\n(def target 7)\n", "[7]"},
		{"default context before namespace", "(first (for [renamed [2]] target))\n(ns audit)\nuser/target", "", "(ns user)\n(def target 7)\n", "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if value := runRenameTestSourceInProject(t, root, tc.setup+tc.source); value != tc.value {
				t.Fatalf("compiler result = %s, want %s", value, tc.value)
			}
			issues := toolinglint.AnalyzeWithOptions(filepath.Join(root, "standalone.dog"), tc.source, toolinglint.AnalyzeOptions{
				Index: idx, CurrentNamespace: tc.namespace, EnableUndefinedSymbol: true,
			})
			if len(issues) != 0 {
				t.Fatalf("standalone lint context changed: %+v", issues)
			}
		})
	}
}

func TestV7IncompleteDefmultiKeepsServerResponsive(t *testing.T) {
	for _, source := range []string{"(defmulti)", "(defmulti target)", "(defmulti target identity)"} {
		t.Run(source, func(t *testing.T) {
			_, messages := v7Messages(t, source, func(uri string) []map[string]any {
				return []map[string]any{req(2, lspMethodDocumentSymbol, map[string]any{"textDocument": map[string]any{"uri": uri}})}
			})
			diagnostic := findNotification(t, messages, lspMethodPublishDiagnostics)
			params := diagnostic["params"].(map[string]any)
			if _, ok := params["diagnostics"].([]any); !ok {
				t.Fatalf("editor buffer did not publish diagnostics: %v", diagnostic)
			}
			if response := findResponseByID(t, messages, 2); response["result"] == nil {
				t.Fatalf("server did not answer request after opening buffer: %v", response)
			}
		})
	}
}

func v7Messages(t *testing.T, source string, requests func(string) []map[string]any) (string, []map[string]any) {
	t.Helper()
	root := t.TempDir()
	uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	input := []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": source, "version": 1}}),
	}
	if requests != nil {
		input = append(input, requests(uri)...)
	}
	input = append(input, notify(lspMethodExit, map[string]any{}))
	return uri, runServer(t, input)
}

func v7References(t *testing.T, source string, line, character int) []any {
	t.Helper()
	_, messages := v7Messages(t, source, func(uri string) []map[string]any {
		return []map[string]any{req(2, lspMethodReferences, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": line, "character": character}, "context": map[string]any{"includeDeclaration": true}})}
	})
	return findResponseByID(t, messages, 2)["result"].([]any)
}
