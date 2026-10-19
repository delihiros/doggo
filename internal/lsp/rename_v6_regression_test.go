package lsp

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
)

func TestRenameV6FnLabelsAreMetadata(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"single", "(def invoke (fn target [] target))\n(invoke)", "(def invoke (fn target [] renamed))\n(invoke)"},
		{"multi", "(def invoke (fn target ([] target) ([x] (+ target x))))\n(invoke)", "(def invoke (fn target ([] renamed) ([x] (+ renamed x))))\n(invoke)"},
		{"method", "(defmulti choose (fn [x] x))\n(defmethod choose :a target [x] target)\n(choose :a)", "(defmulti choose (fn [x] x))\n(defmethod choose :a target [x] renamed)\n(choose :a)"},
		{"destination label", "(def invoke (fn renamed [] target))\n(invoke)", "(def invoke (fn renamed [] renamed))\n(invoke)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns audit)\n(def target 7)\n" + tc.body + "\n"
			want := "(ns audit)\n(def renamed 7)\n" + tc.want + "\n"
			actual, response := renameTestSource(t, source, 1, 5)
			if actual != want {
				t.Fatalf("rename = %q, want %q; response = %v", actual, want, response)
			}
			before, after := runRenameTestSource(t, source), runRenameTestSource(t, actual)
			t.Logf("fresh VM before/after = %s/%s", before, after)
			if before != "7" || after != before {
				t.Fatalf("VM before/after = %s/%s", before, after)
			}
		})
	}
	// The evaluated body can initiate a rename; the metadata label cannot.
	source := "(ns audit)\n(def target 7)\n(fn target [] target)\n"
	actual, response := renameTestSource(t, source, 2, 14)
	if want := "(ns audit)\n(def renamed 7)\n(fn target [] renamed)\n"; actual != want {
		t.Fatalf("body cursor = %q; response = %v", actual, response)
	}
}

func TestRenameV6FnLabelDoesNotHideCapture(t *testing.T) {
	for _, body := range []string{
		"(fn target [renamed] target)",
		"(fn target ([] target) ([renamed] target))",
		"(defmulti choose (fn [x] x))\n(defmethod choose :a target [renamed] target)",
	} {
		source := "(ns audit)\n(def target 7)\n" + body + "\n"
		actual, response := renameTestSource(t, source, 1, 5)
		assertRenameV6Rejected(t, source, actual, response)
	}
}

func assertRenameV6Rejected(t *testing.T, source, actual string, response map[string]any) {
	t.Helper()
	if _, rejected := response["error"]; !rejected {
		t.Fatalf("unsafe rename accepted: %v", response)
	}
	if _, partial := response["result"]; partial || actual != source {
		t.Fatalf("unsafe rename returned partial edits: %v", response)
	}
}

func renameV6Project(t *testing.T, lib, main string, aliasCursor bool) (string, string, map[string]any) {
	t.Helper()
	var cursor *lspPosition
	if aliasCursor {
		cursor = &lspPosition{Line: strings.Count(strings.TrimSuffix(main, "\n"), "\n")}
	}
	return renameV6ProjectAt(t, lib, main, cursor)
}

func renameV6ProjectAt(t *testing.T, lib, main string, cursor *lspPosition) (string, string, map[string]any) {
	t.Helper()
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	mustWrite(t, libPath, lib)
	uri, line, character := libURI, 1, 5
	if cursor != nil {
		uri, line, character = mainURI, cursor.Line, cursor.Character
	}
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": libURI, "text": lib, "version": 1}}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": mainURI, "text": main, "version": 1}}),
		req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": line, "character": character}, "newName": "renamed"}),
		notify(lspMethodExit, map[string]any{}),
	})
	response := findResponseByID(t, messages, 2)
	afterLib, afterMain := applyRenameTestEdits(t, lib, libURI, response), applyRenameTestEdits(t, main, mainURI, response)
	if _, rejected := response["error"]; !rejected {
		before := runRenameTestSourceInProject(t, root, main)
		mustWrite(t, libPath, afterLib)
		after := runRenameTestSourceInProject(t, root, afterMain)
		t.Logf("fresh VM before/after = %s/%s", before, after)
		if after != before {
			t.Fatalf("VM result changed: %s -> %s; sources %q / %q", before, after, afterLib, afterMain)
		}
	}
	return afterLib, afterMain, response
}

func TestRenameV6RequireNamespaceAndSourceAlias(t *testing.T) {
	lib := "(ns app.lib)\n(def target 7)\n"
	for _, ns := range []string{"app.lib", ":app.lib"} {
		for _, refer := range []string{"[target]", ":all"} {
			for _, alias := range []string{"localAlias", "target"} {
				for _, order := range []bool{false, true} {
					for _, cursor := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/%s/order=%t/cursor=%t", ns, refer, alias, order, cursor), func(t *testing.T) {
							options := ":refer " + refer + " :rename {target " + alias + "}"
							if order {
								options = ":rename {target " + alias + "} :refer " + refer
							}
							main := "(ns app.main (:require [" + ns + " " + options + "]))\n" + alias + "\n"
							want := strings.Replace(main, "{target ", "{renamed ", 1)
							want = strings.Replace(want, "[target]", "[renamed]", 1)
							afterLib, afterMain, response := renameV6Project(t, lib, main, cursor)
							if afterLib != "(ns app.lib)\n(def renamed 7)\n" || afterMain != want {
								t.Fatalf("rename = %q / %q, want main %q; response = %v", afterLib, afterMain, want, response)
							}
							changes := response["result"].(map[string]any)["changes"].(map[string]any)
							for uri, value := range changes {
								wantEdits := 1
								if strings.HasSuffix(uri, "/main.dog") && refer == "[target]" {
									wantEdits = 2
								}
								if edits := value.([]any); len(edits) != wantEdits {
									t.Fatalf("edits for %s = %v, want %d", uri, edits, wantEdits)
								}
							}
						})
					}
				}
			}
		}
		for _, options := range []string{":refer [target]", ":as lib"} {
			use, wantUse := "target", "renamed"
			if options == ":as lib" {
				use, wantUse = "lib/target", "lib/renamed"
			}
			main := "(ns app.main (:require [" + ns + " " + options + "]))\n" + use + "\n"
			want := strings.Replace(main, "[target]", "[renamed]", 1)
			want = strings.Replace(want, "\n"+use+"\n", "\n"+wantUse+"\n", 1)
			for _, cursor := range []bool{false, true} {
				afterLib, afterMain, response := renameV6Project(t, lib, main, cursor)
				if afterLib != "(ns app.lib)\n(def renamed 7)\n" || afterMain != want {
					t.Fatalf("namespace rename = %q / %q; response = %v", afterLib, afterMain, response)
				}
			}
		}
	}
}

func TestRenameV6FixedAliasAvoidsFalseCapture(t *testing.T) {
	for _, refer := range []string{"[target]", ":all"} {
		lib := "(ns app.lib)\n(def target 7)\n"
		main := "(ns app.main (:require [:app.lib :refer " + refer + " :rename {target localAlias}]))\n(def renamed 9)\n((fn [renamed] localAlias) 2)\n"
		afterLib, afterMain, response := renameV6Project(t, lib, main, false)
		want := strings.Replace(strings.Replace(main, "{target ", "{renamed ", 1), "[target]", "[renamed]", 1)
		if afterLib != "(ns app.lib)\n(def renamed 7)\n" || afterMain != want {
			t.Fatalf("fixed alias falsely captured: %q / %q; response = %v", afterLib, afterMain, response)
		}
	}
}

func TestRenameV6RejectsSourceCollisionAndRenameMapDuplicate(t *testing.T) {
	for _, tc := range []struct{ lib, options string }{
		{"(ns app.lib)\n(def target 7)\n(def renamed 8)\n", ":refer [target] :rename {target localAlias}"},
		{"(ns app.lib)\n(def target 7)\n", ":refer [target] :rename {target localAlias renamed otherAlias}"},
	} {
		main := "(ns app.main (:require [app.lib " + tc.options + "]))\nlocalAlias\n"
		afterLib, afterMain, response := renameV6Project(t, tc.lib, main, false)
		assertRenameV6Rejected(t, tc.lib, afterLib, response)
		if afterMain != main {
			t.Fatalf("partial importer edit: %q", afterMain)
		}
	}
}

func TestRenameV6NamedFnKeepsActualShadowing(t *testing.T) {
	for _, body := range []string{"((fn target [target] target) 2)", "(letfn [(target [] 2)] (target))"} {
		source := "(ns audit)\n(def target 7)\n" + body + "\ntarget\n"
		actual, response := renameTestSource(t, source, 1, 5)
		want := "(ns audit)\n(def renamed 7)\n" + body + "\nrenamed\n"
		if actual != want {
			t.Fatalf("local shadow changed: %q; response = %v", actual, response)
		}
		if before, after := runRenameTestSource(t, source), runRenameTestSource(t, actual); before != "7" || after != before {
			t.Fatalf("VM before/after = %s/%s", before, after)
		}
	}
}

func TestRenameV6KeywordRequireCaptureAndLiteralControls(t *testing.T) {
	lib := "(ns app.lib)\n(def target 7)\n"
	for _, main := range []string{
		"(ns app.main (:require [:app.lib :refer [target]]))\n((fn [renamed] target) 2)\n",
		"(ns app.main (:require [:app.lib :refer [target]]))\n(def renamed 9)\ntarget\n",
	} {
		afterLib, afterMain, response := renameV6Project(t, lib, main, false)
		assertRenameV6Rejected(t, lib, afterLib, response)
		if afterMain != main {
			t.Fatalf("partial importer edit: %q", afterMain)
		}
	}
	main := "(ns app.main (:require [:app.lib :as lib]))\n((fn [renamed] [:target lib/target]) 2)\n"
	afterLib, afterMain, response := renameV6Project(t, lib, main, false)
	if afterLib != strings.Replace(lib, "target", "renamed", 1) || afterMain != strings.Replace(main, "lib/target", "lib/renamed", 1) {
		t.Fatalf("qualified/literal rename = %q / %q; response = %v", afterLib, afterMain, response)
	}
}

func TestRenameV6AliasSourceOperandsAndLabelsCannotInitiate(t *testing.T) {
	lib := "(ns app.lib)\n(def target 7)\n"
	for _, alias := range []string{"localAlias", "target"} {
		main := "(ns app.main (:require [:app.lib :refer [target] :rename {target " + alias + "} :as lib]))\n" + alias + "\n"
		for _, character := range []int{strings.Index(main, ":app.lib") + 1, strings.Index(main, "[target]") + 1, strings.Index(main, "{target") + 1, strings.Index(main, " "+alias+"}") + 1, strings.Index(main, "lib]")} {
			afterLib, afterMain, response := renameV6ProjectAt(t, lib, main, &lspPosition{Character: character})
			if afterLib != lib || afterMain != main {
				t.Fatalf("literal import cursor %d changed source: %v", character, response)
			}
		}
	}
	main := "(ns app.main (:require [:app.lib :refer [target] :rename {target localAlias}]))\n(let [localAlias 2] localAlias)\n"
	position := lspPosition{Line: 1, Character: 20}
	afterLib, afterMain, response := renameV6ProjectAt(t, lib, main, &position)
	if afterLib != lib || afterMain != main {
		t.Fatalf("local alias shadow initiated rename: %v", response)
	}
}

func TestRenameV6MultipleEntriesAndReferences(t *testing.T) {
	lib := "(ns app.lib)\n(def target 7)\n(def other 8)\n"
	for _, refer := range []string{"[target other]", ":all"} {
		main := "(ns app.main (:require [:app.lib :rename {target localAlias other otherAlias} :refer " + refer + " :as lib]))\n[localAlias otherAlias lib/target app.lib/target :target]\n"
		want := strings.Replace(strings.ReplaceAll(main, "/target", "/renamed"), "{target ", "{renamed ", 1)
		want = strings.Replace(want, "[target other]", "[renamed other]", 1)
		afterLib, afterMain, response := renameV6Project(t, lib, main, false)
		if afterLib != strings.Replace(lib, "target", "renamed", 1) || afterMain != want {
			t.Fatalf("multiple imports = %q / %q; response = %v", afterLib, afterMain, response)
		}

		root := t.TempDir()
		libURI := toolindex.PathToFileURI(filepath.Join(root, "app", "lib.dog"))
		mainURI := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
		mustWrite(t, filepath.Join(root, "app", "lib.dog"), lib)
		messages := runServer(t, []map[string]any{
			req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
			notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": mainURI, "text": main, "version": 1}}),
			req(2, lspMethodReferences, map[string]any{"textDocument": map[string]any{"uri": mainURI}, "position": map[string]any{"line": 1, "character": 1}, "context": map[string]any{"includeDeclaration": false}}),
			req(3, lspMethodReferences, map[string]any{"textDocument": map[string]any{"uri": libURI}, "position": map[string]any{"line": 1, "character": 5}, "context": map[string]any{"includeDeclaration": true}}),
			notify(lspMethodExit, map[string]any{}),
		})
		for _, id := range []int{2, 3} {
			response := findResponseByID(t, messages, id)
			locations, ok := response["result"].([]any)
			wantCount := 4 // source map key, fixed alias use, two qualified uses
			if refer != ":all" {
				wantCount++
			}
			if id == 3 {
				wantCount++
			}
			if !ok || len(locations) != wantCount {
				t.Fatalf("references lost aliases/source operands: %v, want %d", response, wantCount)
			}
			for _, value := range locations {
				location := value.(map[string]any)
				r := decodeEditorRange(t, location["range"])
				if location["uri"] == mainURI && r.Start.Line == 0 && r.Start.Character == strings.Index(main, " localAlias ")+1 {
					t.Fatalf("map value is a reference: %v", location)
				}
			}
		}
	}
}

func TestRenameV6KeywordRequireRejectsReferredDestination(t *testing.T) {
	root := t.TempDir()
	libURI := toolindex.PathToFileURI(filepath.Join(root, "app", "lib.dog"))
	mainURI := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	lib := "(ns app.lib)\n(def target 7)\n"
	main := "(ns app.main (:require [:app.lib :refer [target]] [:app.other :refer [renamed]]))\ntarget\n"
	mustWrite(t, filepath.Join(root, "app", "lib.dog"), lib)
	mustWrite(t, filepath.Join(root, "app", "other.dog"), "(ns app.other)\n(def renamed 8)\n")
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": mainURI, "text": main, "version": 1}}),
		req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": libURI}, "position": map[string]any{"line": 1, "character": 5}, "newName": "renamed"}),
		notify(lspMethodExit, map[string]any{}),
	})
	response := findResponseByID(t, messages, 2)
	assertRenameV6Rejected(t, lib, applyRenameTestEdits(t, lib, libURI, response), response)
	if after := applyRenameTestEdits(t, main, mainURI, response); after != main {
		t.Fatalf("partial importer edit: %q", after)
	}
}

func TestRenameV6UnusedRenameKeyDoesNotHideUnrenamedSource(t *testing.T) {
	lib := "(ns app.lib)\n(def target 7)\n"
	main := "(ns app.main (:require [:app.lib :refer :all :rename {missing target}]))\ntarget\n"
	afterLib, afterMain, response := renameV6Project(t, lib, main, true)
	if afterLib != strings.Replace(lib, "target", "renamed", 1) || afterMain != strings.Replace(main, "\ntarget\n", "\nrenamed\n", 1) {
		t.Fatalf("unused rename key hid source: %q / %q; response = %v", afterLib, afterMain, response)
	}
}

func TestRenameV6RejectsImmutableAliasImports(t *testing.T) {
	for _, refer := range []string{"[owned]", ":all"} {
		root, _ := writeEditorGeneration(t, "(ns dep.lib (:require [:demo :refer "+refer+" :rename {owned localAlias}]))\nlocalAlias\n")
		mainPath := filepath.Join(root, "main.dog")
		mustWrite(t, mainPath, "(ns demo)\n(def owned 1)\nowned\n")
		server, output := initializedGenerationServer(t, root)
		response := generationEditorRequest(t, server, output, 2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": toolindex.PathToFileURI(mainPath)}, "position": map[string]any{"line": 1, "character": 5}, "newName": "renamed"})
		assertRenameV6Rejected(t, "", "", response)
	}
}
