package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
)

func assertRenameV5Rejected(t *testing.T, source, actual string, response map[string]any) {
	t.Helper()
	if _, rejected := response["error"]; !rejected {
		t.Fatalf("unsafe rename accepted: %v; source = %q", response, actual)
	}
	if _, partial := response["result"]; partial || actual != source {
		t.Fatalf("rejected rename returned partial edits: %v; source = %q", response, actual)
	}
}

func TestRenameV5NamespaceLiteral(t *testing.T) {
	for _, ns := range []string{"target", "unrelated"} {
		t.Run(ns, func(t *testing.T) {
			source := "(ns " + ns + ")\n(def target 7)\n" + ns + "/target\n"
			if before := runRenameTestSource(t, source); before != "7" {
				t.Fatalf("before = %s, want 7", before)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			want := "(ns " + ns + ")\n(def renamed 7)\n" + ns + "/renamed\n"
			if actual != want {
				t.Fatalf("namespace literal changed: %q, want %q; response = %v", actual, want, response)
			}
			if after := runRenameTestSource(t, actual); after != "7" {
				t.Fatalf("after = %s, want 7", after)
			}
		})
	}
}

func TestRenameV5NamespaceDeclarationData(t *testing.T) {
	root := t.TempDir()
	mainURI := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	mustWrite(t, filepath.Join(root, "target.dog"), "(ns target)\n(def other 2)\n")
	source := "(ns demo (:require [target :as target]) (:refer-clojure :exclude [target]))\n(def target 7)\ndemo/target\n"
	if before := runRenameTestSourceInProject(t, root, source); before != "7" {
		t.Fatalf("before = %s, want 7", before)
	}
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": mainURI, "text": source, "version": 1}}),
		req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": mainURI}, "position": map[string]any{"line": 1, "character": 5}, "newName": "renamed"}),
		notify(lspMethodExit, map[string]any{}),
	})
	response := findResponseByID(t, messages, 2)
	actual := applyRenameTestEdits(t, source, mainURI, response)
	want := strings.Replace(strings.Replace(source, "(def target", "(def renamed", 1), "demo/target", "demo/renamed", 1)
	if actual != want {
		t.Fatalf("declaration data changed: %q, want %q; response = %v", actual, want, response)
	}
	if after := runRenameTestSourceInProject(t, root, actual); after != "7" {
		t.Fatalf("after = %s, want 7", after)
	}
}

func TestRenameV5NamespaceLiteralCursor(t *testing.T) {
	source := "(ns target)\n(def target 7)\ntarget/target\n"
	actual, response := renameTestSource(t, source, 0, 5)
	if actual != source {
		t.Fatalf("namespace cursor selected global: %q; response = %v", actual, response)
	}
}

func TestRenameV5NamespaceMetadataAndRuntimeRequire(t *testing.T) {
	for _, tc := range []struct{ source, want, value string }{
		{"(ns ^:probe target)\n(def target 7)\ntarget/target\n", "(ns ^:probe target)\n(def renamed 7)\ntarget/renamed\n", "7"},
		{"(ns audit)\n(def target 'doggo.string)\n(require target)\ntarget\n", "(ns audit)\n(def renamed 'doggo.string)\n(require renamed)\nrenamed\n", "doggo.string"},
		{"(ns audit (:refer-clojure :rename {inc local-inc}))\n(def target 7)\n[target (local-inc 2)]\n", "(ns audit (:refer-clojure :rename {inc local-inc}))\n(def renamed 7)\n[renamed (local-inc 2)]\n", "[7 3]"},
	} {
		if before := runRenameTestSource(t, tc.source); before != tc.value {
			t.Fatalf("before = %s, want %s", before, tc.value)
		}
		actual, response := renameTestSource(t, tc.source, 1, 5)
		if actual != tc.want {
			t.Fatalf("rename = %q, want %q; response = %v", actual, tc.want, response)
		}
		if after := runRenameTestSource(t, actual); after != tc.value {
			t.Fatalf("after = %s, want %s", after, tc.value)
		}
	}
}

func TestRenameV5MetadataRequireDeclarations(t *testing.T) {
	for _, tc := range []struct{ name, clause string }{
		{"clause", "^:probe (:require [app.lib :refer [$name]])"},
		{"spec vector", "(:require ^:probe [app.lib :refer [$name]])"},
		{"refer vector", "(:require [app.lib :refer ^:probe [$name]])"},
		{"literal clause metadata", "^{:probe target} (:require [app.lib :refer [$name]])"},
		{"literal spec metadata", "(:require ^{:probe [target]} [app.lib :refer [$name]])"},
		{"literal refer metadata", "(:require [app.lib :refer ^{:probe (target)} [$name]])"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			libPath := filepath.Join(root, "app", "lib.dog")
			libURI := toolindex.PathToFileURI(libPath)
			mainURI := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
			lib := "(ns app.lib)\n(def target 7)\n"
			main := "(ns demo " + strings.ReplaceAll(tc.clause, "$name", "target") + ")\ntarget\n"
			mustWrite(t, libPath, lib)
			if before := runRenameTestSourceInProject(t, root, main); before != "7" {
				t.Fatalf("before = %s, want 7", before)
			}
			messages := runServer(t, []map[string]any{
				req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
				notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": libURI, "text": lib, "version": 1}}),
				notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": mainURI, "text": main, "version": 1}}),
				req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": libURI}, "position": map[string]any{"line": 1, "character": 5}, "newName": "renamed"}),
				notify(lspMethodExit, map[string]any{}),
			})
			response := findResponseByID(t, messages, 2)
			renamedLib := applyRenameTestEdits(t, lib, libURI, response)
			renamedMain := applyRenameTestEdits(t, main, mainURI, response)
			wantMain := "(ns demo " + strings.ReplaceAll(tc.clause, "$name", "renamed") + ")\nrenamed\n"
			if renamedLib != "(ns app.lib)\n(def renamed 7)\n" || renamedMain != wantMain {
				t.Errorf("declaration rename = %q / %q, want main %q; response = %v", renamedLib, renamedMain, wantMain, response)
			}
			mustWrite(t, libPath, renamedLib)
			if after := runRenameTestSourceInProject(t, root, renamedMain); after != "7" {
				t.Fatalf("after = %s, want 7", after)
			}
		})
	}
}

func TestRenameV5ReservedDestination(t *testing.T) {
	source := "(ns audit)\n(def target (fn [x] (+ x 1)))\n(target 2)\n"
	if before := runRenameTestSource(t, source); before != "3" {
		t.Fatalf("before = %s, want 3", before)
	}
	for _, destination := range []string{"if", "do", "fn", "let", "def", "quote", "try", "ns", "defmulti", "defmethod", "var"} {
		t.Run(destination, func(t *testing.T) {
			actual, response := renameAuditSourceTo(t, source, destination)
			assertRenameV5Rejected(t, source, actual, response)
		})
	}
	actual, response := renameAuditSourceTo(t, source, "renamed")
	if actual != strings.ReplaceAll(source, "target", "renamed") {
		t.Fatalf("ordinary destination rejected: %q; response = %v", actual, response)
	}
	if after := runRenameTestSource(t, actual); after != "3" {
		t.Fatalf("after = %s, want 3", after)
	}
}

func TestRenameV5ShadowableBuiltinDestination(t *testing.T) {
	for _, destination := range []string{"+", "-", "*", "mod", "=", "not", "count"} {
		t.Run(destination, func(t *testing.T) {
			source := "(ns audit (:refer-clojure :exclude [" + destination + "]))\n(def target (fn [x] x))\n(target 2)\n"
			if before := runRenameTestSource(t, source); before != "2" {
				t.Fatalf("before = %s, want 2", before)
			}
			actual, response := renameAuditSourceTo(t, source, destination)
			if actual != strings.ReplaceAll(source, "target", destination) {
				t.Fatalf("shadowable destination rejected: %q; response = %v", actual, response)
			}
			if after := runRenameTestSource(t, actual); after != "2" {
				t.Fatalf("after = %s, want 2", after)
			}
		})
	}
}

func TestRenameV5MultimethodDestination(t *testing.T) {
	for _, declaration := range []string{
		"(defmulti renamed (fn [x] x))",
		"(defmulti ^:probe renamed (fn [x] x))",
		"(do (defmulti renamed (fn [x] x)))",
		"(do (do (defmulti renamed (fn [x] x))))",
		"(ns other)\n(defmulti unrelated (fn [x] x))\n(ns audit)\n(defmulti renamed (fn [x] x))",
	} {
		t.Run(declaration, func(t *testing.T) {
			source := "(ns audit)\n(def target 7)\n" + declaration + "\n(defmethod renamed :a [x] 2)\n[target (renamed :a)]\n"
			if before := runRenameTestSource(t, source); before != "[7 2]" {
				t.Fatalf("before = %s, want [7 2]", before)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			assertRenameV5Rejected(t, source, actual, response)
		})
	}
}

func TestRenameV5UnrelatedMultimethod(t *testing.T) {
	for _, prefix := range []string{"", "(ns other)\n(defmulti renamed (fn [x] x))\n(ns audit)\n", "'(defmulti renamed (fn [x] x))\n"} {
		source := "(ns audit)\n(def target 7)\n" + prefix + "(defmulti other (fn [x] x))\n(defmethod other :a [x] 2)\n[target (other :a)]\n"
		if before := runRenameTestSource(t, source); before != "[7 2]" {
			t.Fatalf("before = %s, want [7 2]", before)
		}
		actual, response := renameTestSource(t, source, 1, 5)
		if actual != strings.ReplaceAll(strings.Replace(source, "(def target", "(def renamed", 1), "[target (other", "[renamed (other") {
			t.Fatalf("unrelated multimethod changed: %q; response = %v", actual, response)
		}
		if after := runRenameTestSource(t, actual); after != "[7 2]" {
			t.Fatalf("after = %s, want [7 2]", after)
		}
	}
}

func TestRenameV5DefmethodCapture(t *testing.T) {
	for _, tc := range []struct{ name, dispatch, declaration, call string }{
		{"parameter", "(fn [x] x)", ":a [renamed] target", "(choose :a)"},
		{"metadata", "(fn [x] x)", ":a ^:probe [^:probe renamed] target", "(choose :a)"},
		{"vector destructure", "(fn [x] :a)", ":a [[renamed]] target", "(choose [2])"},
		{"map destructure", "(fn [x] :a)", ":a [{:keys [renamed]}] target", "(choose {:renamed 2})"},
		{"arity", "(fn [x] x)", ":a ([renamed] target) ([x y] target)", "(choose :a)"},
		{"rest", "(fn [x & xs] x)", ":a [x & renamed] target", "(choose :a 2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns audit)\n(def target 7)\n(defmulti choose " + tc.dispatch + ")\n(defmethod choose " + tc.declaration + ")\n" + tc.call + "\n"
			if before := runRenameTestSource(t, source); before != "7" {
				t.Fatalf("before = %s, want 7", before)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			assertRenameV5Rejected(t, source, actual, response)
		})
	}
}

func TestRenameV5DefmethodScopeControls(t *testing.T) {
	for _, tc := range []struct{ name, definition, call, expected string }{
		{"unrelated parameter", "(defmethod choose :a [unrelated] target)", "(choose :a)", "7"},
		{"outer dispatch", "(defmethod choose (do target :a) [renamed] 7)", "(choose :a)", "7"},
		{"qualified body", "(defmethod choose :a [renamed] audit/target)", "(choose :a)", "7"},
		{"shadowed body", "(defmethod choose :a [target] target)", "[target (choose :a)]", "[7 :a]"},
		{"arity isolation", "(defmethod choose :a ([renamed] 7) ([x y] target))", "(choose :a)", "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns audit)\n(def target 7)\n(defmulti choose (fn [x] x))\n" + tc.definition + "\n" + tc.call + "\n"
			if before := runRenameTestSource(t, source); before != tc.expected {
				t.Fatalf("before = %s, want %s", before, tc.expected)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			if _, rejected := response["error"]; rejected || actual == source {
				t.Fatalf("safe rename rejected: %q; response = %v", actual, response)
			}
			if tc.name == "shadowed body" && !strings.Contains(actual, tc.definition) {
				t.Fatalf("method parameter or local body changed: %q", actual)
			}
			if after := runRenameTestSource(t, actual); after != tc.expected {
				t.Fatalf("after = %s, want %s", after, tc.expected)
			}
		})
	}
}

func TestRenameV5DefmethodLocalCursor(t *testing.T) {
	method := "(defmethod choose :a [target] target)"
	source := "(ns audit)\n(def target 7)\n(defmulti choose (fn [x] x))\n" + method + "\ntarget\n"
	for _, character := range []int{strings.Index(method, "target"), strings.LastIndex(method, "target")} {
		actual, response := renameTestSource(t, source, 3, character)
		if actual != source {
			t.Fatalf("method local cursor selected global: %q; response = %v", actual, response)
		}
	}
}

func TestRenameV5DefmethodNameUsesOuterScope(t *testing.T) {
	source := "(ns audit)\n(def target (create-multimethod 'target (fn [x] x)))\n(defmethod target :a [renamed] 7)\n(target :a)\n"
	if before := runRenameTestSource(t, source); before != "7" {
		t.Fatalf("before = %s, want 7", before)
	}
	actual, response := renameTestSource(t, source, 1, 5)
	want := strings.ReplaceAll(source, "(def target", "(def renamed")
	want = strings.ReplaceAll(want, "(defmethod target", "(defmethod renamed")
	want = strings.ReplaceAll(want, "(target :a)", "(renamed :a)")
	if actual != want {
		t.Fatalf("outer multimethod reference changed scope: %q, want %q; response = %v", actual, want, response)
	}
	if after := runRenameTestSource(t, actual); after != "7" {
		t.Fatalf("after = %s, want 7", after)
	}
}
