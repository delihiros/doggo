package lsp

import (
	"net/url"
	"path/filepath"
	"testing"

	toolindex "doggo/internal/tooling/index"
	"doggo/internal/vm"
)

func TestRenameNormalizesCursorDocumentURI(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main file.dog")
	uri := toolindex.PathToFileURI(path)
	requestURI, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	requestURI.Host = "localhost"
	source := "(ns demo)\n(def target 7)\ntarget\n"
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": source, "version": 1}}),
		req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": requestURI.String()}, "position": map[string]any{"line": 1, "character": 5}, "newName": "renamed"}),
		notify(lspMethodExit, map[string]any{}),
	})
	response := findResponseByID(t, messages, 2)
	if actual := applyRenameTestEdits(t, source, uri, response); actual != "(ns demo)\n(def renamed 7)\nrenamed\n" {
		t.Fatalf("equivalent cursor URI failed to rename: %q; response = %v", actual, response)
	}
}

func renameTestSource(t *testing.T, source string, line, character int) (string, map[string]any) {
	t.Helper()
	root := t.TempDir()
	uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": source, "version": 1}}),
		req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": line, "character": character}, "newName": "renamed"}),
		notify(lspMethodExit, map[string]any{}),
	})
	response := findResponseByID(t, messages, 2)
	return applyRenameTestEdits(t, source, uri, response), response
}

func applyRenameTestEdits(t *testing.T, source, uri string, response map[string]any) string {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	if !ok {
		return source
	}
	changes, ok := result["changes"].(map[string]any)
	if !ok {
		t.Fatalf("rename changes missing: %v", response)
	}
	edits, _ := changes[uri].([]any)
	for n := len(edits) - 1; n >= 0; n-- {
		edit := edits[n].(map[string]any)
		var err error
		source, err = applyRangeChange(source, decodeEditorRange(t, edit["range"]), edit["newText"].(string))
		if err != nil {
			t.Fatal(err)
		}
	}
	return source
}

func runRenameTestSource(t *testing.T, source string) string {
	return runRenameTestSourceInProject(t, "", source)
}

func runRenameTestSourceInProject(t *testing.T, root, source string) string {
	t.Helper()
	runner, err := vm.NewRunnerWithConfig(vm.RunnerConfig{ProjectRoot: root, BootstrapCore: true})
	if err != nil {
		t.Fatal(err)
	}
	value, err := runner.Run(source)
	if err != nil {
		t.Fatalf("fresh VM failed for %q: %v", source, err)
	}
	return value.String()
}

func TestRenameCursorMustSelectGlobalOccurrence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		character  int
	}{
		{"let binding", "(let [target 2] target)", 6},
		{"let reference", "(let [target 2] target)", 16},
		{"fn parameter", "((fn [target] target) 2)", 6},
		{"fn reference", "((fn [target] target) 2)", 14},
		{"named fn label", "(fn target [] target)", 4},
		{"letfn binding", "(letfn [(target [] 2)] (target))", 9},
		{"letfn reference", "(letfn [(target [] 2)] (target))", 24},
		{"literal quote", "'target", 1},
		{"explicit literal quote", "(quote target)", 7},
		{"quasiquote data", "`(target)", 2},
		{"nested inactive unquote", "`(`(~target))", 5},
		{"comment", "; target", 2},
		{"string", "\"target\"", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.body + "\ntarget\n"
			actual, response := renameTestSource(t, source, 2, tc.character)
			if actual != source {
				t.Fatalf("unsupported cursor renamed unrelated global: %q; response = %v", actual, response)
			}
		})
	}
}

func TestRenameCanStartAtEvaluatedReaderReference(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		character        int
	}{
		{"Var abbreviation", "(deref #'target)", "(deref #'renamed)", 9},
		{"explicit Var", "(deref (var target))", "(deref (var renamed))", 12},
		{"unquote abbreviation", "`(~target)", "`(~renamed)", 3},
		{"explicit unquote", "(quasiquote ((unquote target)))", "(quasiquote ((unquote renamed)))", 22},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.body + "\n"
			actual, response := renameTestSource(t, source, 2, tc.character)
			want := "(ns demo)\n(def renamed 7)\n" + tc.want + "\n"
			if actual != want {
				t.Fatalf("reader reference cursor failed to rename: %q, want %q; response = %v", actual, want, response)
			}
		})
	}
}

func TestRenameAliasReaderReferences(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	lib := "(ns app.lib)\n(def target 7)\n"
	main := "(ns app.main (:require [app.lib :as lib]))\n(let [target 2] [(deref #'lib/target) `(~lib/target) target])\n"
	mustWrite(t, libPath, lib)
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": libURI, "text": lib, "version": 1}}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": mainURI, "text": main, "version": 1}}),
		req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": mainURI}, "position": map[string]any{"line": 1, "character": 32}, "newName": "renamed"}),
		notify(lspMethodExit, map[string]any{}),
	})
	response := findResponseByID(t, messages, 2)
	renamedLib := applyRenameTestEdits(t, lib, libURI, response)
	renamedMain := applyRenameTestEdits(t, main, mainURI, response)
	wantMain := "(ns app.main (:require [app.lib :as lib]))\n(let [target 2] [(deref #'lib/renamed) `(~lib/renamed) target])\n"
	if renamedLib != "(ns app.lib)\n(def renamed 7)\n" || renamedMain != wantMain {
		t.Fatalf("alias reader rename = %q / %q; response = %v", renamedLib, renamedMain, response)
	}
	before := runRenameTestSourceInProject(t, root, main)
	mustWrite(t, libPath, renamedLib)
	if after := runRenameTestSourceInProject(t, root, renamedMain); after != before {
		t.Fatalf("fresh VM result = %s, want %s", after, before)
	}
}

func TestRenameRejectsImmutableEvaluatedReaderReferences(t *testing.T) {
	for _, body := range []string{"(deref #'demo/owned)", "`(~demo/owned)", "(quasiquote ((unquote-splicing [demo/owned])))"} {
		t.Run(body, func(t *testing.T) {
			root, _ := writeEditorGeneration(t, "(ns dep.lib)\n(def target 42)\n"+body+"\n")
			mainPath := filepath.Join(root, "main.dog")
			mustWrite(t, mainPath, "(ns demo)\n(def owned 1)\nowned\n")
			server, output := initializedGenerationServer(t, root)
			response := generationEditorRequest(t, server, output, 2, lspMethodRename, map[string]any{
				"textDocument": map[string]any{"uri": toolindex.PathToFileURI(mainPath)},
				"position":     map[string]any{"line": 1, "character": 5}, "newName": "renamed",
			})
			if _, rejected := response["error"]; !rejected {
				t.Fatalf("immutable reader reference did not reject whole rename: %v", response)
			}
			if _, partial := response["result"]; partial {
				t.Fatalf("immutable reader reference returned partial edits: %v", response)
			}
		})
	}
}

func TestRenamePreservesEvaluatedReaderReferences(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"Var abbreviation", "(deref #'target)", "(deref #'renamed)"},
		{"explicit Var", "(deref (var target))", "(deref (var renamed))"},
		{"unquote abbreviation", "`(~target)", "`(~renamed)"},
		{"explicit unquote", "(quasiquote ((unquote target)))", "(quasiquote ((unquote renamed)))"},
		{"splice abbreviation", "`(~@[target])", "`(~@[renamed])"},
		{"explicit unquote-splicing", "(quasiquote ((unquote-splicing [target])))", "(quasiquote ((unquote-splicing [renamed])))"},
		{"explicit splice-unquote", "(quasiquote ((splice-unquote [target])))", "(quasiquote ((splice-unquote [renamed])))"},
		{"literal quote outside quasiquote", "'[target ~target #'target]", "'[target ~target #'target]"},
		{"explicit quote outside quasiquote", "(quote [(unquote target) (var target)])", "(quote [(unquote target) (var target)])"},
		{"quasiquote data", "`(target)", "`(target)"},
		{"quote inside active quasiquote", "`('~target)", "`('~renamed)"},
		{"explicit quote inside active quasiquote", "(quasiquote ((quote (unquote target))))", "(quasiquote ((quote (unquote renamed))))"},
		{"quote containing splice", "`('(target ~@[target]))", "`('(target ~@[renamed]))"},
		{"nested inactive and active unquote", "`(target `(~target) ~target)", "`(target `(~target) ~renamed)"},
		{"nested active unquotes", "`(`(~~target))", "`(`(~~renamed))"},
		{"explicit nested active unquotes", "(quasiquote ((quasiquote ((unquote (unquote target))))))", "(quasiquote ((quasiquote ((unquote (unquote renamed))))))"},
		{"nested inactive splice", "`(`(~@[target]))", "`(`(~@[target]))"},
		{"nested active splice", "`(`(~@~[target]))", "`(`(~@~[renamed]))"},
		{"nested explicit active splice", "(quasiquote ((quasiquote ((unquote-splicing (unquote [target]))))))", "(quasiquote ((quasiquote ((unquote-splicing (unquote [renamed]))))))"},
		{"local shadow in active unquote", "(let [target 2] `(~target))", "(let [target 2] `(~target))"},
		{"binding scope in active unquote", "`(~(let [target target] target))", "`(~(let [target renamed] target))"},
		{"qualified Var and unquote", "[(deref #'demo/target) `(~demo/target)]", "[(deref #'demo/renamed) `(~demo/renamed)]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.body + "\n"
			before := runRenameTestSource(t, source)
			actual, response := renameTestSource(t, source, 1, 5)
			if _, rejected := response["error"]; rejected {
				t.Fatalf("valid global rename rejected: %v", response)
			}
			want := "(ns demo)\n(def renamed 7)\n" + tc.want + "\n"
			if actual != want {
				t.Fatalf("renamed source = %q, want %q", actual, want)
			}
			if after := runRenameTestSource(t, actual); after != before {
				t.Fatalf("fresh VM result = %s, want original %s", after, before)
			}
			t.Logf("fresh VM before/after = %s", before)
		})
	}
}
