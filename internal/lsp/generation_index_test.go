package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	toolindex "doggo/internal/tooling/index"
)

func writeEditorGeneration(t *testing.T, dependencySource ...string) (string, string) {
	t.Helper()
	root := t.TempDir()
	mod := &project.ModFile{Module: "example/editor", Deps: []string{"./dep"}}
	if err := project.WriteModFile(project.ModPath(root), mod); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(root, ".doggo", "generations", "stage")
	depRoot := filepath.Join(stage, "deps", "pending")
	mustWrite(t, filepath.Join(depRoot, "doggo.mod"), "{:module \"example/dep\" :deps []}\n")
	content := "(ns dep.lib)\n(def target 42)\n"
	if len(dependencySource) > 0 {
		content = dependencySource[0]
	}
	mustWrite(t, filepath.Join(depRoot, "lib.dog"), content)
	depHandle, err := os.OpenRoot(depRoot)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := project.CanonicalRootDigest(depHandle)
	if closeErr := depHandle.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	dep := project.LockDep{Source: "./dep", Kind: "path", ModuleID: "doggo:example/dep", Digest: digest}
	dep.InstallDir = "deps/" + project.DependencyKey(dep)
	if err := os.Rename(depRoot, filepath.Join(stage, filepath.FromSlash(dep.InstallDir))); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(stage, "toolchain", "main.go"), "package main\n")
	lock := &project.LockFile{LockVersion: project.LockVersion, ModLockInputsSHA256: project.HashModLockInputs(mod), Deps: []project.LockDep{dep}}
	lock.Generation, err = project.GenerationDigest(stage, lock)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(stage, ".generation"), lock.Generation+"\n")
	final := filepath.Join(root, ".doggo", "generations", lock.Generation)
	if err := os.Rename(stage, final); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteLockFile(project.LockPath(root), lock); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, ".doggo", "generation.lock"), "")
	mustWrite(t, filepath.Join(root, "main.dog"), "(ns demo (:require [dep.lib :as dep]))\n(dep/target)\n")
	mustWrite(t, filepath.Join(root, ".doggo", "deps", "obsolete", "lib.dog"), "(ns obsolete)\n(def stale 0)\n")
	mustWrite(t, filepath.Join(root, ".doggo", "generations", strings.Repeat("f", 64), "deps", "old", "lib.dog"), "(ns unselected)\n(def stale 0)\n")
	return root, filepath.Join(final, filepath.FromSlash(dep.InstallDir), "lib.dog")
}

func initializedGenerationServer(t *testing.T, root string) (*Server, *bytes.Buffer) {
	t.Helper()
	var input bytes.Buffer
	output := new(bytes.Buffer)
	server := NewServer(&input, output, project.OpenGenerationSession)
	t.Cleanup(func() {
		if server.generationSession != nil {
			if err := server.generationSession.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	response := generationEditorRequest(t, server, output, 1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)})
	if _, ok := response["result"]; !ok {
		t.Fatalf("initialize = %v", response)
	}
	return server, output
}

func generationEditorRequest(t *testing.T, server *Server, output *bytes.Buffer, id int, method string, params map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(req(id, method, params))
	if err != nil {
		t.Fatal(err)
	}
	var decoded message
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := server.handleRequest(decoded); err != nil {
		t.Fatal(err)
	}
	return findResponseByID(t, decodeMessages(t, output.Bytes()), id)
}

func TestRetainedEditorLocationRejectsPathRebinding(t *testing.T) {
	root, dependencyPath := writeEditorGeneration(t)
	server, output := initializedGenerationServer(t, root)
	depRoot := filepath.Dir(dependencyPath)
	if err := os.Rename(depRoot, depRoot+"-retained"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dependencyPath, "(ns compromised)\n(def target 999)\n")
	source := server.generationSession.Config.GenerationSources[0]
	trusted, err := source.ReadFile("lib.dog")
	if err != nil || !strings.Contains(string(trusted), "target 42") {
		t.Fatalf("retained read lost original source: %q, %v", trusted, err)
	}
	if _, err := source.FilePath("lib.dog"); err == nil {
		t.Error("editor file path points at a replacement directory")
	}
	if err := server.state.index.ValidateGenerationSources(); err == nil {
		t.Error("cached index accepted replacement editor location")
	}
	if _, err := toolindex.BuildWorkspaceIndex(toolindex.BuildOptions{ProjectRoot: root, GenerationSources: server.generationSession.Config.GenerationSources}); err == nil {
		t.Error("rebuilt index accepted replacement editor location")
	}
	response := generationEditorRequest(t, server, output, 2, lspMethodDefinition, map[string]any{"textDocument": map[string]any{"uri": toolindex.PathToFileURI(filepath.Join(root, "main.dog"))}, "position": map[string]any{"line": 1, "character": 6}})
	if _, ok := response["error"]; !ok {
		t.Errorf("definition published replacement file location: %v", response)
	}
}

func TestGenerationDependencyRenameIsRejected(t *testing.T) {
	root, dependencyPath := writeEditorGeneration(t)
	server, output := initializedGenerationServer(t, root)
	for i, uri := range []string{toolindex.PathToFileURI(dependencyPath), toolindex.PathToFileURI(filepath.Join(root, "main.dog"))} {
		response := generationEditorRequest(t, server, output, i+2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 6}, "newName": "renamed"})
		if _, ok := response["error"]; !ok {
			t.Errorf("retained dependency rename returned edits: %v", response)
		}
	}
}

func TestProjectRenameIsRejectedWhenRetainedDependencyReferencesSymbol(t *testing.T) {
	root, _ := writeEditorGeneration(t, "(ns dep.lib)\n(def target 42)\n(println demo/owned)\n")
	mainPath := filepath.Join(root, "main.dog")
	mustWrite(t, mainPath, "(ns demo)\n(def owned 1)\n(println owned)\n")
	server, output := initializedGenerationServer(t, root)
	uri := toolindex.PathToFileURI(mainPath)
	response := generationEditorRequest(t, server, output, 2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 5}, "newName": "renamed"})
	errorResponse, ok := response["error"].(map[string]any)
	if !ok || !strings.Contains(errorResponse["message"].(string), "immutable generation") {
		t.Fatalf("project rename did not reject immutable dependency references: %v", response)
	}
	if _, ok := response["result"]; ok {
		t.Fatalf("rejected rename returned partial edits: %v", response)
	}
}

func TestRetainedDependencyFormattingAndCodeActionsDoNotEdit(t *testing.T) {
	root, dependencyPath := writeEditorGeneration(t, "  (ns dep.lib)\n  (def target 42)\n  (def unused 0)\n(import fmt)\n")
	server, output := initializedGenerationServer(t, root)
	uri := toolindex.PathToFileURI(dependencyPath)
	for i, method := range []string{lspMethodFormatting, lspMethodCodeAction} {
		response := generationEditorRequest(t, server, output, i+2, method, map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
			"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 4, "character": 0}},
			"context":      map[string]any{"diagnostics": []any{}},
		})
		if _, ok := response["error"]; ok {
			continue
		}
		if edits := response["result"].([]any); len(edits) != 0 {
			t.Errorf("%s proposed generation edits: %v", method, edits)
		}
	}
}

func TestRetainedDocumentRequestsReportIntegrityFailure(t *testing.T) {
	for _, change := range []string{"closed", "changed"} {
		t.Run(change, func(t *testing.T) {
			root, dependencyPath := writeEditorGeneration(t)
			server, output := initializedGenerationServer(t, root)
			if change == "closed" {
				if err := server.generationSession.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(dependencyPath, []byte("(ns dep.lib)\n(def target 99)\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			for i, method := range []string{lspMethodFormatting, lspMethodSemanticTokensFull, lspMethodCompletion, lspMethodDefinition, lspMethodReferences, lspMethodRename, lspMethodHover, lspMethodCodeAction, lspMethodDocumentSymbol, lspMethodWorkspaceSymbol} {
				response := generationEditorRequest(t, server, output, i+2, method, map[string]any{
					"textDocument": map[string]any{"uri": toolindex.PathToFileURI(dependencyPath)},
					"position":     map[string]any{"line": 1, "character": 6}, "newName": "renamed",
					"options": map[string]any{"tabSize": 2, "insertSpaces": true},
					"range":   map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 2, "character": 0}},
					"context": map[string]any{"diagnostics": []any{}, "includeDeclaration": true},
				})
				errorResponse, ok := response["error"].(map[string]any)
				if !ok || !strings.Contains(errorResponse["message"].(string), "generation source integrity error") {
					t.Errorf("%s hid retained source failure: %v", method, response)
				}
			}
		})
	}
}

func TestServerIndexesOnlyRetainedSelectedGeneration(t *testing.T) {
	root, dependencyPath := writeEditorGeneration(t)
	uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	var input, output bytes.Buffer
	writer := bufio.NewWriter(&input)
	for _, request := range []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, lspMethodDefinition, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 6}}),
		req(3, lspMethodWorkspaceSymbol, map[string]any{"query": "stale"}),
		req(4, lspMethodReferences, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 6}, "context": map[string]any{"includeDeclaration": true}}),
		req(5, lspMethodCompletion, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 5}}),
		notify(lspMethodExit, map[string]any{}),
	} {
		if err := writeMessage(writer, request); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	server := NewServer(&input, &output, project.OpenGenerationSession)
	if err := server.Run(); err != nil {
		t.Fatal(err)
	}
	messages := decodeMessages(t, output.Bytes())
	if _, ok := findResponseByID(t, messages, 1)["result"]; !ok {
		t.Fatalf("initialize failed: %v", messages)
	}
	definitions := findResponseByID(t, messages, 2)["result"].([]any)
	if len(definitions) != 1 {
		t.Fatalf("selected generation definitions = %v", definitions)
	}
	location := definitions[0].(map[string]any)
	if location["uri"] != toolindex.PathToFileURI(dependencyPath) {
		t.Fatalf("definition URI = %v, want usable selected file URI", location["uri"])
	}
	want := lspRange{Start: lspPosition{Line: 1, Character: 5}, End: lspPosition{Line: 1, Character: 11}}
	if actual := decodeEditorRange(t, location["range"]); actual != want {
		t.Fatalf("definition range = %+v", actual)
	}
	if symbols := findResponseByID(t, messages, 3)["result"].([]any); len(symbols) != 0 {
		t.Fatalf("unselected generation or obsolete dependencies indexed: %v", symbols)
	}
	if references := findResponseByID(t, messages, 4)["result"].([]any); len(references) != 2 {
		t.Fatalf("dependency references = %v", references)
	}
	if completions := findResponseByID(t, messages, 5)["result"].([]any); !containsCompletion(completions, "dep/target") {
		t.Fatalf("selected dependency completion = %v", completions)
	}
}

func TestGenerationIndexRejectsClosedOrChangedRetainedSource(t *testing.T) {
	for _, change := range []string{"closed", "changed"} {
		t.Run(change, func(t *testing.T) {
			root, dependencyPath := writeEditorGeneration(t)
			var input, output bytes.Buffer
			server := NewServer(&input, &output, project.OpenGenerationSession)
			msg := req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)})
			var decoded message
			raw, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if err := server.handleRequest(decoded); err != nil {
				t.Fatal(err)
			}
			defer server.generationSession.Close()
			if change == "closed" {
				if err := server.generationSession.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(dependencyPath, []byte("(ns dep.lib)\n(def target 99)\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := server.refreshIndexIfStale(); err == nil {
				t.Fatal("index accepted unavailable or corrupt retained generation source")
			}
		})
	}
}
