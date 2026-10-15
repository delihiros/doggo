package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
)

func TestRenameRejectsDestinationCaptureAtomically(t *testing.T) {
	for _, body := range []string{
		"(let [renamed 2] target)",
		"((fn [renamed] target) 2)",
		"(letfn [(renamed [] 2)] target)",
		"(def renamed 2)\ntarget",
	} {
		t.Run(body, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + body + "\n"
			if before := runRenameTestSource(t, source); before != "7" {
				t.Fatalf("before = %s, want 7", before)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			if _, rejected := response["error"]; !rejected {
				t.Fatalf("capture must reject rename: %v; source = %q", response, actual)
			}
			if _, partial := response["result"]; partial || actual != source {
				t.Fatalf("capture returned partial edits: %v", response)
			}
		})
	}
}

func TestRenameDestinationCaptureKeepsQualifiedReferences(t *testing.T) {
	source := "(ns demo)\n(def target 7)\n(let [renamed 2] demo/target)\n"
	actual, response := renameTestSource(t, source, 1, 5)
	want := "(ns demo)\n(def renamed 7)\n(let [renamed 2] demo/renamed)\n"
	if actual != want {
		t.Fatalf("qualified reference rename = %q, want %q; response = %v", actual, want, response)
	}
	if after := runRenameTestSource(t, actual); after != "7" {
		t.Fatalf("fresh VM after = %s, want 7", after)
	}
}

func TestRenameDestinationChecksBindingVisibility(t *testing.T) {
	source := "(ns demo)\n(def target 7)\n(let [x target renamed 2] x)\n"
	actual, response := renameTestSource(t, source, 1, 5)
	want := "(ns demo)\n(def renamed 7)\n(let [x renamed renamed 2] x)\n"
	if actual != want {
		t.Fatalf("later binding captured earlier initializer: %q, want %q; response = %v", actual, want, response)
	}
	if after := runRenameTestSource(t, actual); after != "7" {
		t.Fatalf("fresh VM after = %s, want 7", after)
	}
}

func TestRenameRejectsReferredDestinationCollision(t *testing.T) {
	for _, option := range []string{":refer [renamed]", ":refer :all"} {
		t.Run(option, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "app", "other.dog"), "(ns app.other)\n(def renamed 2)\n")
			uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
			source := "(ns demo (:require [app.other " + option + "]))\n(def target 7)\ntarget\n"
			messages := runServer(t, []map[string]any{
				req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
				notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": source, "version": 1}}),
				req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 5}, "newName": "renamed"}),
				notify(lspMethodExit, map[string]any{}),
			})
			response := findResponseByID(t, messages, 2)
			if _, rejected := response["error"]; !rejected {
				t.Fatalf("referred destination collision accepted: %v", response)
			}
			if _, partial := response["result"]; partial {
				t.Fatalf("referred destination returned partial edits: %v", response)
			}
		})
	}
}

func TestRenameReferredDestinationResolutionAcrossDocuments(t *testing.T) {
	for _, tc := range []struct{ name, require, reference, want string }{
		{"unqualified collision", ":refer [target]", "target", ""},
		{"qualified alias", ":as lib", "lib/target", "lib/renamed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			libPath := filepath.Join(root, "app", "lib.dog")
			libURI := toolindex.PathToFileURI(libPath)
			mainURI := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
			lib := "(ns app.lib)\n(def target 7)\n"
			mustWrite(t, libPath, lib)
			mustWrite(t, filepath.Join(root, "app", "other.dog"), "(ns app.other)\n(def renamed 2)\n")
			main := "(ns demo (:require [app.lib " + tc.require + "] [app.other :refer [renamed]]))\n" + tc.reference + "\n"
			messages := runServer(t, []map[string]any{
				req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
				notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": libURI, "text": lib, "version": 1}}),
				notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": mainURI, "text": main, "version": 1}}),
				req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": libURI}, "position": map[string]any{"line": 1, "character": 5}, "newName": "renamed"}),
				notify(lspMethodExit, map[string]any{}),
			})
			response := findResponseByID(t, messages, 2)
			if tc.want == "" {
				if _, rejected := response["error"]; !rejected {
					t.Fatalf("referred destination collision accepted: %v", response)
				}
				if _, partial := response["result"]; partial {
					t.Fatalf("referred destination returned partial edits: %v", response)
				}
				return
			}
			renamedLib := applyRenameTestEdits(t, lib, libURI, response)
			renamedMain := applyRenameTestEdits(t, main, mainURI, response)
			if renamedLib != strings.ReplaceAll(lib, "target", "renamed") || renamedMain != strings.ReplaceAll(main, tc.reference, tc.want) {
				t.Fatalf("qualified alias changed resolution: %q / %q; response = %v", renamedLib, renamedMain, response)
			}
			mustWrite(t, libPath, renamedLib)
			if after := runRenameTestSourceInProject(t, root, renamedMain); after != "7" {
				t.Fatalf("fresh VM after = %s, want 7", after)
			}
		})
	}
}

func TestRenameUsesEachNamespaceInDocument(t *testing.T) {
	source := "(ns one)\n(def target 7)\n(ns two)\n(def target 2)\n[one/target target]\n"
	if before := runRenameTestSource(t, source); before != "[7 2]" {
		t.Fatalf("before = %s, want [7 2]", before)
	}
	for _, cursor := range []struct{ line, character int }{{1, 5}, {4, 6}} {
		actual, response := renameTestSource(t, source, cursor.line, cursor.character)
		want := "(ns one)\n(def renamed 7)\n(ns two)\n(def target 2)\n[one/renamed target]\n"
		if actual != want {
			t.Fatalf("namespace rename = %q, want %q; response = %v", actual, want, response)
		}
		if after := runRenameTestSource(t, actual); after != "[7 2]" {
			t.Fatalf("fresh VM after = %s, want [7 2]", after)
		}
	}
}

func TestRenameNamespaceBoundariesUseUTF16Columns(t *testing.T) {
	source := "\"😀\" (ns one) (def target 7) (ns two) (def target 2) [one/target target]\n"
	for _, character := range []int{strings.Index(source, "target") - 2, strings.Index(source, "one/target") - 2} {
		actual, response := renameTestSource(t, source, 0, character)
		want := "\"😀\" (ns one) (def renamed 7) (ns two) (def target 2) [one/renamed target]\n"
		if actual != want {
			t.Fatalf("same-line namespace rename = %q, want %q; response = %v", actual, want, response)
		}
		if after := runRenameTestSource(t, actual); after != "[7 2]" {
			t.Fatalf("fresh VM after = %s, want [7 2]", after)
		}
	}
}

func TestRenameEarlierNamespaceImportsAcrossDocuments(t *testing.T) {
	for _, importSpec := range []struct{ name, option, reference, renamed string }{
		{"alias", ":as lib", "lib/target", "lib/renamed"},
		{"refer", ":refer [target]", "target", "renamed"},
		{"refer all", ":refer :all", "target", "renamed"},
	} {
		for _, fromReference := range []bool{false, true} {
			t.Run(importSpec.name+"/cursor="+map[bool]string{false: "declaration", true: "reference"}[fromReference], func(t *testing.T) {
				root := t.TempDir()
				libPath := filepath.Join(root, "app", "lib.dog")
				libURI := toolindex.PathToFileURI(libPath)
				mainURI := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
				lib := "(ns app.lib)\n(def target 7)\n"
				main := "(ns app.early (:require [app.lib " + importSpec.option + "]))\n" + importSpec.reference + "\n(ns app.late)\n2\n"
				mustWrite(t, libPath, lib)
				cursorURI, line, character := libURI, 1, 5
				if fromReference {
					cursorURI, line, character = mainURI, 1, 1
				}
				messages := runServer(t, []map[string]any{
					req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
					notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": libURI, "text": lib, "version": 1}}),
					notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": mainURI, "text": main, "version": 1}}),
					req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": cursorURI}, "position": map[string]any{"line": line, "character": character}, "newName": "renamed"}),
					notify(lspMethodExit, map[string]any{}),
				})
				response := findResponseByID(t, messages, 2)
				renamedLib := applyRenameTestEdits(t, lib, libURI, response)
				renamedMain := applyRenameTestEdits(t, main, mainURI, response)
				wantMain := strings.ReplaceAll(main, "target", "renamed")
				if renamedLib != "(ns app.lib)\n(def renamed 7)\n" || renamedMain != wantMain {
					t.Fatalf("earlier import rename = %q / %q, want main %q; response = %v", renamedLib, renamedMain, wantMain, response)
				}
				before := runRenameTestSourceInProject(t, root, main)
				mustWrite(t, libPath, renamedLib)
				if after := runRenameTestSourceInProject(t, root, renamedMain); after != before {
					t.Fatalf("fresh VM after = %s, want %s", after, before)
				}
			})
		}
	}
}

func TestRenamePreservesRecordTypeAndProtocolBindings(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"record fields", "(defrecord Box [target])\n(:target (->Box 2))", "2"},
		{"type fields", "(deftype Box [target])\n(get-field (->Box 2) :target)", "2"},
		{"record parameter", "(defprotocol Probe (read-val [this target]))\n(defrecord Box [] Probe (read-val [this target] target))\n(read-val (->Box) 2)", "2"},
		{"type parameter", "(defprotocol Probe (read-val [this target]))\n(deftype Box [] Probe (read-val [this target] target))\n(read-val (->Box) 2)", "2"},
		{"type implicit fields", "(defprotocol Probe (read-val [this]))\n(deftype Box [target] Probe (read-val [this] target))\n(read-val (->Box 2))", "2"},
		{"record global body", "(defprotocol Probe (read-val [this]))\n(defrecord Box [target] Probe (read-val [this] target))\n(read-val (->Box 2))", "7"},
		{"type global body", "(defprotocol Probe (read-val [this]))\n(deftype Box [unrelated] Probe (read-val [this] target))\n(read-val (->Box 2))", "7"},
		{"record metadata", "(defrecord ^:probe Box ^:probe [^:probe target])\n(:target (->Box 2))", "2"},
		{"type metadata", "(deftype ^:probe Box ^:probe [^:probe target])\n(get-field (->Box 2) :target)", "2"},
		{"method metadata parameters", "(defprotocol Probe (read-val ^:probe [this ^:probe target]))\n(defrecord Box [] Probe (^:probe read-val ^:probe [this ^:probe target] target))\n(read-val (->Box) 2)", "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.body + "\n"
			if before := runRenameTestSource(t, source); before != tc.want {
				t.Fatalf("before = %s, want %s", before, tc.want)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			wantBody := tc.body
			if tc.name == "record global body" || tc.name == "type global body" {
				wantBody = strings.Replace(wantBody, "[this] target)", "[this] renamed)", 1)
			}
			want := "(ns demo)\n(def renamed 7)\n" + wantBody + "\n"
			if actual != want {
				t.Fatalf("binding rename = %q, want %q; response = %v", actual, want, response)
			}
			if after := runRenameTestSource(t, actual); after != tc.want {
				t.Fatalf("fresh VM after = %s, want %s", after, tc.want)
			}
		})
	}
}

func TestRenameRecordTypeBindingCursorCannotSelectGlobal(t *testing.T) {
	for _, body := range []string{
		"(defrecord Box [target])",
		"(deftype Box [target])",
		"(defprotocol Probe (read-val [this target]))",
		"(defprotocol Probe (read-val [this]))\n(deftype Box [target] Probe (read-val [this] target))",
		"(defprotocol Probe (read-val [this target]))\n(defrecord Box [] Probe (read-val [this target] target))",
	} {
		t.Run(body, func(t *testing.T) {
			lines := strings.Split(body, "\n")
			source := "(ns demo)\n(def target 7)\n" + body + "\n"
			actual, response := renameTestSource(t, source, len(lines)+1, strings.LastIndex(lines[len(lines)-1], "target"))
			if actual != source {
				t.Fatalf("record/type binding cursor changed unrelated global: %q; response = %v", actual, response)
			}
		})
	}
}

func TestRenameCatchDestinationCaptureRejectsAtomically(t *testing.T) {
	for _, body := range []string{
		"(try (throw 2) (catch [renamed] target))",
		"(try (throw 2) (catch [renamed] (do target)))",
		"(try (throw 2) (catch ^:probe [^:probe renamed] (do target)))",
	} {
		t.Run(body, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + body + "\n"
			if before := runRenameTestSource(t, source); before != "7" {
				t.Fatalf("fresh VM before = %s, want 7", before)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			if _, rejected := response["error"]; !rejected {
				t.Fatalf("catch destination capture accepted: %v; source = %q", response, actual)
			}
			if _, partial := response["result"]; partial || actual != source {
				t.Fatalf("catch destination capture returned partial edits: %v", response)
			}
		})
	}
}

func TestRenameCatchBodyPreservesGlobalReference(t *testing.T) {
	for _, body := range []string{
		"(try (throw 2) (catch [caught] target))",
		"(try (throw 2) (catch [caught] (do target)))",
		"(try (throw 2) (catch ^:probe [^:probe caught] target))",
	} {
		t.Run(body, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + body + "\n"
			if before := runRenameTestSource(t, source); before != "7" {
				t.Fatalf("fresh VM before = %s, want 7", before)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			want := strings.ReplaceAll(source, "target", "renamed")
			if actual != want {
				t.Fatalf("catch global rename = %q, want %q; response = %v", actual, want, response)
			}
			if after := runRenameTestSource(t, actual); after != "7" {
				t.Fatalf("fresh VM after = %s, want 7", after)
			}
		})
	}
}

func TestRenameCatchLocalCursorCannotSelectGlobal(t *testing.T) {
	for _, body := range []string{
		"(try (throw 2) (catch [target] target))",
		"(try (throw 2) (catch [target] (do target)))",
		"(try (throw 2) (catch ^:probe [^:probe target] (do target)))",
	} {
		for _, character := range []int{strings.Index(body, "target"), strings.LastIndex(body, "target")} {
			t.Run(body+"/"+body[character:], func(t *testing.T) {
				source := "(ns demo)\n(def target 7)\n" + body + "\n"
				// Validate the local-shadow fixture without assuming the VM's
				// printed representation of the caught exception value.
				runRenameTestSource(t, source)
				actual, response := renameTestSource(t, source, 2, character)
				if actual != source {
					t.Fatalf("catch local cursor changed unrelated global: %q; response = %v", actual, response)
				}
			})
		}
	}
}

func renameAuditSourceTo(t *testing.T, source, destination string) (string, map[string]any) {
	t.Helper()
	root := t.TempDir()
	uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": source, "version": 1}}),
		req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 5}, "newName": destination}),
		notify(lspMethodExit, map[string]any{}),
	})
	response := findResponseByID(t, messages, 2)
	return applyRenameTestEdits(t, source, uri, response), response
}

func TestRenameRejectsCoreEmittedGlobalDestinations(t *testing.T) {
	for _, tc := range []struct{ name, body, destination string }{
		{"record", "(defrecord renamed [])", "renamed"},
		{"type", "(deftype renamed [])", "renamed"},
		{"protocol", "(defprotocol renamed (read-val [this]))", "renamed"},
		{"empty protocol", "(defprotocol renamed)", "renamed"},
		{"record constructor", "(defrecord Box [])", "->Box"},
		{"type constructor", "(deftype Box [])", "->Box"},
		{"record map constructor", "(defrecord Box [])", "map->Box"},
		{"protocol method", "(defprotocol Probe (renamed [this]))", "renamed"},
		{"metadata record", "(defrecord ^:probe renamed ^:probe [])", "renamed"},
		{"metadata type constructor", "(deftype ^:probe Box ^:probe [])", "->Box"},
		{"metadata method parameters", "(defprotocol Probe (renamed ^:probe [^:probe this]))", "renamed"},
		{"do group", "(do (defrecord renamed []))", "renamed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.body + "\ntarget\n"
			if before := runRenameTestSource(t, source); before != "7" {
				t.Fatalf("fresh VM before = %s, want 7", before)
			}
			actual, response := renameAuditSourceTo(t, source, tc.destination)
			if _, rejected := response["error"]; !rejected {
				t.Fatalf("core emitted destination accepted: %v; source = %q", response, actual)
			}
			if _, partial := response["result"]; partial || actual != source {
				t.Fatalf("core emitted destination returned partial edits: %v", response)
			}
		})
	}
}

func TestRenameCoreDestinationIgnoresUnrelatedAndLiteralNames(t *testing.T) {
	for _, tc := range []struct{ name, body, destination string }{
		{"unrelated record", "(defrecord Other [])", "renamed"},
		{"unrelated namespace", "(ns other)\n(defrecord renamed [])\n(ns demo)", "renamed"},
		{"field", "(defrecord Box [renamed])", "renamed"},
		{"protocol parameter", "(defprotocol Probe (read-val [this renamed]))", "renamed"},
		{"type has no map constructor", "(deftype Box [])", "map->Box"},
		{"quoted record", "'(defrecord renamed [])", "renamed"},
		{"quoted type", "(quote (deftype renamed []))", "renamed"},
		{"quoted protocol", "'(defprotocol renamed (read-val [this]))", "renamed"},
		{"metadata literal", "(def ^{:probe (defrecord renamed [])} unrelated 2)", "renamed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.body + "\ntarget\n"
			if before := runRenameTestSource(t, source); before != "7" {
				t.Fatalf("fresh VM before = %s, want 7", before)
			}
			actual, response := renameAuditSourceTo(t, source, tc.destination)
			want := "(ns demo)\n(def " + tc.destination + " 7)\n" + tc.body + "\n" + tc.destination + "\n"
			if actual != want {
				t.Fatalf("unrelated/literal destination rejected or changed: %q, want %q; response = %v", actual, want, response)
			}
			if after := runRenameTestSource(t, actual); after != "7" {
				t.Fatalf("fresh VM after = %s, want 7", after)
			}
		})
	}
}

func TestRenameCoreDestinationAcrossReferredDocuments(t *testing.T) {
	for _, tc := range []struct {
		name, emitted, newName string
		protected, qualified   bool
	}{
		{"record", "(defrecord renamed [])", "renamed", true, false},
		{"type", "(deftype renamed [])", "renamed", true, false},
		{"protocol", "(defprotocol renamed (read-val [this]))", "renamed", false, false},
		{"record constructor", "(defrecord Box [])", "->Box", false, false},
		{"record map constructor", "(defrecord Box [])", "map->Box", false, false},
		{"protocol method", "(defprotocol Probe (renamed [this]))", "renamed", false, false},
		{"qualified protocol method", "(defprotocol Probe (renamed [this]))", "renamed", false, true},
	} {
		for _, importOption := range []string{":refer [" + tc.newName + "]", ":refer :all"} {
			if tc.protected && importOption != ":refer :all" {
				continue // protected type names cannot be explicitly referred
			}
			t.Run(tc.name+"/"+importOption, func(t *testing.T) {
				root := t.TempDir()
				libPath := filepath.Join(root, "app", "lib.dog")
				libURI := toolindex.PathToFileURI(libPath)
				mainURI := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
				lib := "(ns app.lib)\n(def target 7)\n"
				mustWrite(t, libPath, lib)
				mustWrite(t, filepath.Join(root, "app", "other.dog"), "(ns app.other)\n"+tc.emitted+"\n")
				libImport, reference := ":refer [target]", "target"
				if tc.qualified {
					libImport, reference = ":as lib", "lib/target"
				}
				main := "(ns demo (:require [app.lib " + libImport + "] [app.other " + importOption + "]))\n" + reference + "\n"
				if before := runRenameTestSourceInProject(t, root, main); before != "7" {
					t.Fatalf("fresh VM before = %s, want 7", before)
				}
				messages := runServer(t, []map[string]any{
					req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
					notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": libURI, "text": lib, "version": 1}}),
					notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": mainURI, "text": main, "version": 1}}),
					req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": libURI}, "position": map[string]any{"line": 1, "character": 5}, "newName": tc.newName}),
					notify(lspMethodExit, map[string]any{}),
				})
				response := findResponseByID(t, messages, 2)
				if tc.protected || tc.qualified {
					renamedLib := applyRenameTestEdits(t, lib, libURI, response)
					renamedMain := applyRenameTestEdits(t, main, mainURI, response)
					if renamedLib != strings.ReplaceAll(lib, "target", tc.newName) || renamedMain != strings.ReplaceAll(main, "target", tc.newName) {
						t.Fatalf("unrelated protected/qualified name rejected or changed: %q / %q; response = %v", renamedLib, renamedMain, response)
					}
					mustWrite(t, libPath, renamedLib)
					if after := runRenameTestSourceInProject(t, root, renamedMain); after != "7" {
						t.Fatalf("fresh VM after = %s, want 7", after)
					}
					return
				}
				if _, rejected := response["error"]; !rejected {
					t.Fatalf("referred emitted destination accepted: %v", response)
				}
				if _, partial := response["result"]; partial {
					t.Fatalf("referred emitted destination returned partial edits: %v", response)
				}
			})
		}
	}
}
