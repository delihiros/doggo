package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
)

func TestServerInitializeCapabilities(t *testing.T) {
	root := t.TempDir()
	rootURI := toolindex.PathToFileURI(root)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": rootURI}),
		req(2, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	initResp := findResponseByID(t, msgs, 1)
	result, ok := initResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize result missing: %#v", initResp)
	}
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities missing: %#v", result)
	}
	syncCfg, ok := caps["textDocumentSync"].(map[string]any)
	if !ok {
		t.Fatalf("textDocumentSync must be object: %#v", caps["textDocumentSync"])
	}
	if got := int(syncCfg["change"].(float64)); got != lspTextDocumentSyncKindIncremental {
		t.Fatalf("textDocumentSync.change mismatch: got=%d want=%d", got, lspTextDocumentSyncKindIncremental)
	}
	if openClose, ok := syncCfg["openClose"].(bool); !ok || !openClose {
		t.Fatalf("textDocumentSync.openClose should be true: %#v", syncCfg["openClose"])
	}
	if save, ok := syncCfg["save"].(map[string]any); !ok {
		t.Fatalf("textDocumentSync.save must be object: %#v", syncCfg["save"])
	} else if includeText, ok := save["includeText"].(bool); !ok || includeText {
		t.Fatalf("textDocumentSync.save.includeText should be false: %#v", save["includeText"])
	}
	if def, ok := caps["definitionProvider"].(bool); !ok || !def {
		t.Fatalf("definitionProvider should be true: %#v", caps["definitionProvider"])
	}
	if hover, ok := caps["hoverProvider"].(bool); !ok || !hover {
		t.Fatalf("hoverProvider should be true: %#v", caps["hoverProvider"])
	}
	if docSymbol, ok := caps["documentSymbolProvider"].(bool); !ok || !docSymbol {
		t.Fatalf("documentSymbolProvider should be true: %#v", caps["documentSymbolProvider"])
	}
	if references, ok := caps["referencesProvider"].(bool); !ok || !references {
		t.Fatalf("referencesProvider should be true: %#v", caps["referencesProvider"])
	}
	if rename, ok := caps["renameProvider"].(bool); !ok || !rename {
		t.Fatalf("renameProvider should be true: %#v", caps["renameProvider"])
	}
	if formatting, ok := caps["documentFormattingProvider"].(bool); !ok || !formatting {
		t.Fatalf("documentFormattingProvider should be true: %#v", caps["documentFormattingProvider"])
	}
	if workspaceSymbol, ok := caps["workspaceSymbolProvider"].(bool); !ok || !workspaceSymbol {
		t.Fatalf("workspaceSymbolProvider should be true: %#v", caps["workspaceSymbolProvider"])
	}
	if codeAction, ok := caps["codeActionProvider"].(bool); !ok || !codeAction {
		t.Fatalf("codeActionProvider should be true: %#v", caps["codeActionProvider"])
	}
	semanticProvider, ok := caps["semanticTokensProvider"].(map[string]any)
	if !ok {
		t.Fatalf("semanticTokensProvider should be object: %#v", caps["semanticTokensProvider"])
	}
	if full, ok := semanticProvider["full"].(bool); !ok || !full {
		t.Fatalf("semanticTokensProvider.full should be true: %#v", semanticProvider["full"])
	}
	legend, ok := semanticProvider["legend"].(map[string]any)
	if !ok {
		t.Fatalf("semanticTokensProvider.legend should be object: %#v", semanticProvider["legend"])
	}
	tokenTypes, ok := legend["tokenTypes"].([]any)
	if !ok {
		t.Fatalf("semanticTokensProvider.legend.tokenTypes should be array: %#v", legend["tokenTypes"])
	}
	typeNames := make([]string, 0, len(tokenTypes))
	for _, item := range tokenTypes {
		typeNames = append(typeNames, item.(string))
	}
	if !slices.Contains(typeNames, "keyword") || !slices.Contains(typeNames, "function") {
		t.Fatalf("semantic token legend should include keyword/function: %v", typeNames)
	}
}

func TestServerDidOpenPublishesDiagnostics(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    "(require (+ 1 2))",
			},
		}),
		req(2, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	diag := findNotification(t, msgs, lspMethodPublishDiagnostics)
	params, ok := diag["params"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostic params missing: %#v", diag)
	}
	if params["uri"] != mainURI {
		t.Fatalf("diagnostic uri mismatch: %v", params["uri"])
	}
	items, ok := params["diagnostics"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected diagnostics: %#v", params["diagnostics"])
	}
	first := items[0].(map[string]any)
	msg := first["message"].(string)
	if !strings.Contains(msg, "require argument must be") {
		t.Fatalf("unexpected diagnostic message: %s", msg)
	}
}

func TestServerDidChangeIncrementalUpdatesDocument(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	mustWrite(t, mainPath, "(def foo 1)\n")

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    "(def foo 1)\n",
			},
		}),
		notify(lspMethodDidChange, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 2,
			},
			"contentChanges": []map[string]any{
				{
					"range": map[string]any{
						"start": map[string]any{"line": 0, "character": 5},
						"end":   map[string]any{"line": 0, "character": 8},
					},
					"text": "bar",
				},
			},
		}),
		req(2, lspMethodDefinition, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 0, "character": 6},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	defResp := findResponseByID(t, msgs, 2)
	locations, ok := defResp["result"].([]any)
	if !ok || len(locations) == 0 {
		t.Fatalf("definition result should include updated symbol location: %#v", defResp["result"])
	}
	loc := locations[0].(map[string]any)
	if loc["uri"] != mainURI {
		t.Fatalf("definition uri mismatch: got=%v want=%s", loc["uri"], mainURI)
	}
}

func TestServerDidChangeIncrementalUTF16CharacterPositions(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	source := `(def s "😀")
(def v 1)
`
	mustWrite(t, mainPath, source)

	s := NewServer(bytes.NewReader(nil), io.Discard, nil)
	if err := s.handleInitialize(message{
		ID:     rawID(t, 1),
		Method: lspMethodInitialize,
		Params: mustMarshalRaw(initializeParams{
			RootURI: toolindex.PathToFileURI(root),
		}),
	}); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	if err := s.handleDidOpen(mustMarshalRaw(didOpenParams{
		TextDocument: textDocumentItem{
			URI:     mainURI,
			Version: 1,
			Text:    source,
		},
	})); err != nil {
		t.Fatalf("didOpen failed: %v", err)
	}

	if err := s.handleDidChange(mustMarshalRaw(didChangeParams{
		TextDocument: versionedTextDocumentIdentifier{
			URI:     mainURI,
			Version: 2,
		},
		ContentChanges: []textDocumentContentChangeEvent{
			{
				Range: &lspRange{
					Start: lspPosition{Line: 0, Character: 8},
					End:   lspPosition{Line: 0, Character: 10},
				},
				Text: "😺",
			},
		},
	})); err != nil {
		t.Fatalf("didChange failed: %v", err)
	}

	doc, ok := s.docs.Get(mainURI)
	if !ok {
		t.Fatalf("updated document missing from store")
	}
	want := `(def s "😺")
(def v 1)
`
	if doc.Text != want {
		t.Fatalf("UTF-16 incremental update mismatch:\nwant:\n%s\ngot:\n%s", want, doc.Text)
	}
}

func TestServerDidChangeInvalidRangePublishesDiagnosticAndKeepsRunning(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	mustWrite(t, mainPath, "(def foo 1)\n")

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    "(def foo 1)\n",
			},
		}),
		notify(lspMethodDidChange, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 2,
			},
			"contentChanges": []map[string]any{
				{
					"range": map[string]any{
						"start": map[string]any{"line": 99, "character": 0},
						"end":   map[string]any{"line": 99, "character": 1},
					},
					"text": "bar",
				},
			},
		}),
		req(2, lspMethodDefinition, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 0, "character": 6},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	diagFound := false
	for _, msg := range msgs {
		if msg["method"] != lspMethodPublishDiagnostics {
			continue
		}
		params := msg["params"].(map[string]any)
		if params["uri"] != mainURI {
			continue
		}
		items := params["diagnostics"].([]any)
		for _, item := range items {
			d := item.(map[string]any)
			if strings.Contains(d["message"].(string), "incremental sync failed") {
				diagFound = true
			}
		}
	}
	if !diagFound {
		t.Fatalf("expected incremental sync failure diagnostic, msgs=%#v", msgs)
	}

	defResp := findResponseByID(t, msgs, 2)
	locations, ok := defResp["result"].([]any)
	if !ok || len(locations) == 0 {
		t.Fatalf("server should remain responsive after invalid didChange: %#v", defResp["result"])
	}
}

func TestApplyRangeChangeSupportsCRLFLineEndings(t *testing.T) {
	source := "(def x 1)\r\n(def y 2)\r\n"
	updated, err := applyRangeChange(source, lspRange{
		Start: lspPosition{Line: 1, Character: 5},
		End:   lspPosition{Line: 1, Character: 6},
	}, "z")
	if err != nil {
		t.Fatalf("applyRangeChange failed: %v", err)
	}
	want := "(def x 1)\r\n(def z 2)\r\n"
	if updated != want {
		t.Fatalf("CRLF incremental update mismatch:\nwant=%q\ngot=%q", want, updated)
	}
}

func TestServerDidCloseClearsDiagnostics(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    "(import 123)",
			},
		}),
		notify(lspMethodDidClose, map[string]any{
			"textDocument": map[string]any{
				"uri": mainURI,
			},
		}),
		req(2, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	foundEmpty := false
	foundNonEmpty := false
	for _, msg := range msgs {
		if msg["method"] != lspMethodPublishDiagnostics {
			continue
		}
		params := msg["params"].(map[string]any)
		items := params["diagnostics"].([]any)
		if len(items) == 0 {
			foundEmpty = true
		} else {
			foundNonEmpty = true
		}
	}
	if !foundNonEmpty || !foundEmpty {
		t.Fatalf("expected both non-empty and empty diagnostics. messages=%#v", msgs)
	}
}

func TestServerCompletionAndDefinitionProject(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "app", "lib.dog"), `(ns app.lib)
(def bar 42)
`)
	mustWrite(t, filepath.Join(root, "app", "ref.dog"), `(ns app.ref)
(def foo 1)
`)
	main := `(ns app.main
  (:require [app.lib :as lib]
            [app.ref :refer [foo]])
  (:refer-clojure :exclude [map]))
(def local 1)
(lib/bar)
fo
ma
`
	mainPath := filepath.Join(root, "app", "main.dog")
	mustWrite(t, mainPath, main)
	mainURI := toolindex.PathToFileURI(mainPath)
	libURI := toolindex.PathToFileURI(filepath.Join(root, "app", "lib.dog"))

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, lspMethodCompletion, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 6, "character": 2},
		}),
		req(3, lspMethodDefinition, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 5, "character": 6},
		}),
		req(4, lspMethodCompletion, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 7, "character": 2},
		}),
		req(5, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	compResp := findResponseByID(t, msgs, 2)
	compItems := compResp["result"].([]any)
	if !containsCompletion(compItems, "foo") {
		t.Fatalf("completion should include foo: %#v", compItems)
	}

	compExcluded := findResponseByID(t, msgs, 4)
	excludedItems := compExcluded["result"].([]any)
	if containsCompletion(excludedItems, "map") {
		t.Fatalf("completion should exclude map: %#v", excludedItems)
	}

	defResp := findResponseByID(t, msgs, 3)
	locations := defResp["result"].([]any)
	if len(locations) == 0 {
		t.Fatalf("definition result is empty: %#v", defResp)
	}
	loc := locations[0].(map[string]any)
	if loc["uri"] != libURI {
		t.Fatalf("definition uri mismatch: got=%v want=%s", loc["uri"], libURI)
	}
}

func TestServerReferences(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	mainPath := filepath.Join(root, "app", "main.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(mainPath)

	mustWrite(t, libPath, `(ns app.lib)
(def target 42)
`)
	mustWrite(t, mainPath, `(ns app.main
  (:require [app.lib :as lib]
            [app.lib :refer [target]]))
(lib/target)
target
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/references", map[string]any{
			"textDocument": map[string]any{"uri": libURI},
			"position":     map[string]any{"line": 1, "character": 6},
			"context":      map[string]any{"includeDeclaration": false},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	locations, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("references result is not location array: %#v", resp["result"])
	}
	if len(locations) != 3 {
		t.Fatalf("expected 3 references without declaration, got %#v", locations)
	}
	for _, loc := range locations {
		item := loc.(map[string]any)
		if item["uri"] != mainURI {
			t.Fatalf("reference should point to main file: %#v", item)
		}
	}
}

func TestServerRename(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	mainPath := filepath.Join(root, "app", "main.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(mainPath)

	mustWrite(t, libPath, `(ns app.lib)
(def target 42)
`)
	mustWrite(t, mainPath, `(ns app.main
  (:require [app.lib :as lib]
            [app.lib :refer [target]]))
(lib/target)
target
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/rename", map[string]any{
			"textDocument": map[string]any{"uri": libURI},
			"position":     map[string]any{"line": 1, "character": 6},
			"newName":      "renamed",
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("rename result missing: %#v", resp)
	}
	changes, ok := result["changes"].(map[string]any)
	if !ok {
		t.Fatalf("rename changes missing: %#v", result)
	}
	libEdits, ok := changes[libURI].([]any)
	if !ok || len(libEdits) == 0 {
		t.Fatalf("expected edits for declaration file: %#v", changes)
	}
	mainEdits, ok := changes[mainURI].([]any)
	if !ok || len(mainEdits) < 2 {
		t.Fatalf("expected edits for usages in main file: %#v", changes)
	}

	for _, edits := range []any{libEdits, mainEdits} {
		for _, edit := range edits.([]any) {
			item := edit.(map[string]any)
			if item["newText"] == "" {
				t.Fatalf("rename edit has empty newText: %#v", item)
			}
		}
	}
}

func TestServerRenameDoesNotTouchLocalShadowing(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	mainPath := filepath.Join(root, "app", "main.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(mainPath)

	mustWrite(t, libPath, `(ns app.lib)
(def target 42)
`)
	mustWrite(t, mainPath, `(ns app.main
  (:require [app.lib :as lib]
            [app.lib :refer [target]]))
(let [target 1]
  target)
(lib/target)
target
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/rename", map[string]any{
			"textDocument": map[string]any{"uri": libURI},
			"position":     map[string]any{"line": 1, "character": 6},
			"newName":      "renamed",
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("rename result missing: %#v", resp)
	}
	changes := result["changes"].(map[string]any)
	mainEdits := changes[mainURI].([]any)
	for _, edit := range mainEdits {
		item := edit.(map[string]any)
		rng := item["range"].(map[string]any)
		start := rng["start"].(map[string]any)
		line := int(start["line"].(float64))
		if line == 3 || line == 4 {
			t.Fatalf("rename should not edit let-local shadowing on lines 3/4: %#v", mainEdits)
		}
	}
}

func TestServerReferencesDoNotIncludeLocalShadowing(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	mainPath := filepath.Join(root, "app", "main.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(mainPath)

	mustWrite(t, libPath, `(ns app.lib)
(def target 42)
`)
	mustWrite(t, mainPath, `(ns app.main
  (:require [app.lib :as lib]
            [app.lib :refer [target]]))
(let [target 1]
  target)
(lib/target)
target
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/references", map[string]any{
			"textDocument": map[string]any{"uri": libURI},
			"position":     map[string]any{"line": 1, "character": 6},
			"context":      map[string]any{"includeDeclaration": false},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	locations := resp["result"].([]any)
	for _, loc := range locations {
		item := loc.(map[string]any)
		if item["uri"] != mainURI {
			continue
		}
		rng := item["range"].(map[string]any)
		start := rng["start"].(map[string]any)
		line := int(start["line"].(float64))
		if line == 3 || line == 4 {
			t.Fatalf("references should not include let-local shadowing on lines 3/4: %#v", locations)
		}
	}
}

func TestServerReferencesDoNotIncludeLetfnLocalShadowing(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	mainPath := filepath.Join(root, "app", "main.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(mainPath)

	mustWrite(t, libPath, `(ns app.lib)
(def target 42)
`)
	mustWrite(t, mainPath, `(ns app.main
  (:require [app.lib :refer [target]]))
(letfn [(target [x] x)]
  (target 1))
target
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/references", map[string]any{
			"textDocument": map[string]any{"uri": libURI},
			"position":     map[string]any{"line": 1, "character": 6},
			"context":      map[string]any{"includeDeclaration": false},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	locations := resp["result"].([]any)
	for _, loc := range locations {
		item := loc.(map[string]any)
		if item["uri"] != mainURI {
			continue
		}
		rng := item["range"].(map[string]any)
		start := rng["start"].(map[string]any)
		line := int(start["line"].(float64))
		if line == 2 || line == 3 {
			t.Fatalf("references should not include letfn-local shadowing on lines 2/3: %#v", locations)
		}
	}
}

func TestServerReferencesDoNotIncludeLetfnMetadataLocalShadowing(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	mainPath := filepath.Join(root, "app", "main.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(mainPath)

	mustWrite(t, libPath, `(ns app.lib)
(def target 42)
`)
	mustWrite(t, mainPath, `(ns app.main
  (:require [app.lib :refer [target]]))
(letfn [(^:private target [x] x)]
  (target 1))
target
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/references", map[string]any{
			"textDocument": map[string]any{"uri": libURI},
			"position":     map[string]any{"line": 1, "character": 6},
			"context":      map[string]any{"includeDeclaration": false},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	locations := resp["result"].([]any)
	for _, loc := range locations {
		item := loc.(map[string]any)
		if item["uri"] != mainURI {
			continue
		}
		rng := item["range"].(map[string]any)
		start := rng["start"].(map[string]any)
		line := int(start["line"].(float64))
		if line == 2 || line == 3 {
			t.Fatalf("references should not include metadata letfn-local shadowing on lines 2/3: %#v", locations)
		}
	}
}

func TestServerFormatting(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	mustWrite(t, mainPath, "(def x 1)\n")

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    "\t(def x 1)   ",
			},
		}),
		req(2, "textDocument/formatting", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"options": map[string]any{
				"tabSize":      2,
				"insertSpaces": true,
			},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	edits, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("formatting result is not edit array: %#v", resp["result"])
	}
	if len(edits) != 1 {
		t.Fatalf("expected one full-document edit, got %#v", edits)
	}
	edit := edits[0].(map[string]any)
	newText, ok := edit["newText"].(string)
	if !ok {
		t.Fatalf("newText missing: %#v", edit)
	}
	if strings.Contains(newText, "\t") {
		t.Fatalf("formatted text should not contain leading tab: %q", newText)
	}
	if strings.Contains(newText, "   \n") {
		t.Fatalf("formatted text should trim trailing spaces: %q", newText)
	}
	if !strings.HasSuffix(newText, "\n") {
		t.Fatalf("formatted text should end with newline: %q", newText)
	}
}

func TestServerSemanticTokens(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	mustWrite(t, mainPath, `(ns app.main)
(defn fib [n] (if (<= n 1) "x" 42))
; comment
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/semanticTokens/full", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("semantic tokens result missing: %#v", resp)
	}
	data, ok := result["data"].([]any)
	if !ok {
		t.Fatalf("semantic tokens data missing: %#v", result)
	}
	if len(data) == 0 || len(data)%5 != 0 {
		t.Fatalf("semantic token payload must be non-empty and divisible by 5: %#v", data)
	}
}

func TestServerCodeActionImportFix(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	source := `(ns app.main)
(import fmt)
`
	mustWrite(t, mainPath, source)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    source,
			},
		}),
		req(2, "textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"range": map[string]any{
				"start": map[string]any{"line": 1, "character": 0},
				"end":   map[string]any{"line": 1, "character": 12},
			},
			"context": map[string]any{
				"diagnostics": []any{},
			},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	actions, ok := resp["result"].([]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("codeAction result should include import fix: %#v", resp["result"])
	}
	found := false
	for _, item := range actions {
		action := item.(map[string]any)
		title := strings.ToLower(action["title"].(string))
		if strings.Contains(title, "import") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected import-related code action, got %#v", actions)
	}

	action := actions[0].(map[string]any)
	edit := action["edit"].(map[string]any)
	changes := edit["changes"].(map[string]any)
	uriChanges := changes[mainURI].([]any)
	first := uriChanges[0].(map[string]any)
	newText := first["newText"].(string)
	if newText != "\"fmt\"" {
		t.Fatalf("import fix should replace only symbol token, got %q", newText)
	}
}

func TestServerCodeActionImportFixSupportsSlashPackagePath(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	source := `(ns app.main)
(import net/http)
`
	mustWrite(t, mainPath, source)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    source,
			},
		}),
		req(2, "textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"range": map[string]any{
				"start": map[string]any{"line": 1, "character": 0},
				"end":   map[string]any{"line": 1, "character": 17},
			},
			"context": map[string]any{
				"diagnostics": []any{},
			},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	actions, ok := resp["result"].([]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("codeAction result should include import fix: %#v", resp["result"])
	}

	action := actions[0].(map[string]any)
	edit := action["edit"].(map[string]any)
	changes := edit["changes"].(map[string]any)
	uriChanges := changes[mainURI].([]any)
	first := uriChanges[0].(map[string]any)
	newText := first["newText"].(string)
	if newText != "\"net/http\"" {
		t.Fatalf("import fix should quote full package path, got %q", newText)
	}
}

func TestServerCodeActionImportFixPreservesTrailingComment(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	source := `(ns app.main)
(import fmt) ; keep-me
`
	mustWrite(t, mainPath, source)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    source,
			},
		}),
		req(2, "textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"range": map[string]any{
				"start": map[string]any{"line": 1, "character": 0},
				"end":   map[string]any{"line": 1, "character": 24},
			},
			"context": map[string]any{
				"diagnostics": []any{},
			},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	actions := resp["result"].([]any)
	if len(actions) == 0 {
		t.Fatalf("expected import fix action")
	}

	action := actions[0].(map[string]any)
	edit := action["edit"].(map[string]any)
	changes := edit["changes"].(map[string]any)
	uriChanges := changes[mainURI].([]any)
	first := uriChanges[0].(map[string]any)
	editRange := first["range"].(map[string]any)
	start := editRange["start"].(map[string]any)
	if int(start["character"].(float64)) == 0 {
		t.Fatalf("import fix should not replace full line (to preserve trailing comment): %#v", first)
	}
}

func TestServerCodeActionImportFixIgnoresQuotedImport(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	source := `(ns app.main)
'(import fmt)
`
	mustWrite(t, mainPath, source)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    source,
			},
		}),
		req(2, "textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"range": map[string]any{
				"start": map[string]any{"line": 0, "character": 0},
				"end":   map[string]any{"line": 2, "character": 0},
			},
			"context": map[string]any{
				"diagnostics": []any{},
			},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	actions, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("codeAction result should be array: %#v", resp["result"])
	}
	for _, item := range actions {
		title := strings.ToLower(item.(map[string]any)["title"].(string))
		if strings.Contains(title, "import") {
			t.Fatalf("quoted import should not produce import fix action: %#v", actions)
		}
	}
}

func TestServerCodeActionImportFixIgnoresDiscardedImport(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	source := `(ns app.main)
#_(import fmt)
`
	mustWrite(t, mainPath, source)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    source,
			},
		}),
		req(2, "textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"range": map[string]any{
				"start": map[string]any{"line": 0, "character": 0},
				"end":   map[string]any{"line": 2, "character": 0},
			},
			"context": map[string]any{
				"diagnostics": []any{},
			},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	actions, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("codeAction result should be array: %#v", resp["result"])
	}
	for _, item := range actions {
		title := strings.ToLower(item.(map[string]any)["title"].(string))
		if strings.Contains(title, "import") {
			t.Fatalf("discarded import should not produce import fix action: %#v", actions)
		}
	}
}

func TestServerCodeActionUnusedDefinitionCleanup(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	source := `(ns app.main)
(def unused 1)
(def used 2)
used
`
	mustWrite(t, mainPath, source)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    source,
			},
		}),
		req(2, "textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"range": map[string]any{
				"start": map[string]any{"line": 1, "character": 0},
				"end":   map[string]any{"line": 3, "character": 4},
			},
			"context": map[string]any{
				"diagnostics": []any{},
			},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	actions, ok := resp["result"].([]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("codeAction result should include cleanup: %#v", resp["result"])
	}

	found := false
	for _, item := range actions {
		action := item.(map[string]any)
		title := strings.ToLower(action["title"].(string))
		if strings.Contains(title, "unused") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unused-definition cleanup action, got %#v", actions)
	}
}

func TestServerRenameDoesNotTouchLetfnLocalShadowing(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	mainPath := filepath.Join(root, "app", "main.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(mainPath)

	mustWrite(t, libPath, `(ns app.lib)
(def target 42)
`)
	mustWrite(t, mainPath, `(ns app.main
  (:require [app.lib :refer [target]]))
(letfn [(target [x] x)]
  (target 1))
target
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/rename", map[string]any{
			"textDocument": map[string]any{"uri": libURI},
			"position":     map[string]any{"line": 1, "character": 6},
			"newName":      "renamed",
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	result := resp["result"].(map[string]any)
	changes := result["changes"].(map[string]any)
	mainEdits := changes[mainURI].([]any)
	for _, edit := range mainEdits {
		item := edit.(map[string]any)
		rng := item["range"].(map[string]any)
		start := rng["start"].(map[string]any)
		line := int(start["line"].(float64))
		if line == 2 || line == 3 {
			t.Fatalf("rename should not edit letfn-local shadowing on lines 2/3: %#v", mainEdits)
		}
	}
}

func TestServerRenameDoesNotTouchLetfnMetadataLocalShadowing(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "app", "lib.dog")
	mainPath := filepath.Join(root, "app", "main.dog")
	libURI := toolindex.PathToFileURI(libPath)
	mainURI := toolindex.PathToFileURI(mainPath)

	mustWrite(t, libPath, `(ns app.lib)
(def target 42)
`)
	mustWrite(t, mainPath, `(ns app.main
  (:require [app.lib :refer [target]]))
(letfn [(^:private target [x] x)]
  (target 1))
target
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/rename", map[string]any{
			"textDocument": map[string]any{"uri": libURI},
			"position":     map[string]any{"line": 1, "character": 6},
			"newName":      "renamed",
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	result := resp["result"].(map[string]any)
	changes := result["changes"].(map[string]any)
	mainEdits := changes[mainURI].([]any)
	for _, edit := range mainEdits {
		item := edit.(map[string]any)
		rng := item["range"].(map[string]any)
		start := rng["start"].(map[string]any)
		line := int(start["line"].(float64))
		if line == 2 || line == 3 {
			t.Fatalf("rename should not edit metadata letfn-local shadowing on lines 2/3: %#v", mainEdits)
		}
	}
}

func TestServerReferencesExcludeDeclarationForDefnWithMetadata(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)

	mustWrite(t, mainPath, `(ns app.main)
(defn ^:private foo [x] x)
(foo 1)
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/references", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 2, "character": 2},
			"context":      map[string]any{"includeDeclaration": false},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	locations := resp["result"].([]any)
	for _, loc := range locations {
		item := loc.(map[string]any)
		rng := item["range"].(map[string]any)
		start := rng["start"].(map[string]any)
		line := int(start["line"].(float64))
		if line == 1 {
			t.Fatalf("declaration should be excluded for defn metadata when includeDeclaration=false: %#v", locations)
		}
	}
}

func TestServerFormattingPreservesCRLF(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	mustWrite(t, mainPath, "(def x 1)\r\n")

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text":    "\t(def x 1)   \r\n",
			},
		}),
		req(2, "textDocument/formatting", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"options": map[string]any{
				"tabSize":      2,
				"insertSpaces": true,
			},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	edits := resp["result"].([]any)
	if len(edits) != 1 {
		t.Fatalf("expected one edit, got %#v", edits)
	}
	edit := edits[0].(map[string]any)
	newText := edit["newText"].(string)
	if !strings.Contains(newText, "\r\n") {
		t.Fatalf("formatted text should preserve CRLF, got %q", newText)
	}
}

func TestServerQualifiedCompletion(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "app", "lib.dog"), `(ns app.lib)
(def bar 1)
(def baz 2)
`)
	mainPath := filepath.Join(root, "app", "main.dog")
	mustWrite(t, mainPath, `(ns app.main
  (:require [app.lib :as lib]))
(lib/ba)
`)
	mainURI := toolindex.PathToFileURI(mainPath)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, lspMethodCompletion, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 2, "character": 7},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	comp := findResponseByID(t, msgs, 2)["result"].([]any)
	if !containsCompletion(comp, "lib/bar") || !containsCompletion(comp, "lib/baz") {
		t.Fatalf("qualified completion mismatch: %#v", comp)
	}
}

func TestServerHoverAndDocumentSymbol(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	mustWrite(t, mainPath, `(ns app.main)
(defn greet "hello doc" [x] x)
(def a 1)
(greet 1)
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 3, "character": 2},
		}),
		req(3, "textDocument/documentSymbol", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
		}),
		req(4, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	hover := findResponseByID(t, msgs, 2)["result"].(map[string]any)
	contents := hover["contents"].(map[string]any)
	value := contents["value"].(string)
	if !strings.Contains(value, "app.main/greet") || !strings.Contains(value, "hello doc") || !strings.Contains(value, "defn") {
		t.Fatalf("hover content mismatch: %s", value)
	}

	docSymbols := findResponseByID(t, msgs, 3)["result"].([]any)
	if len(docSymbols) != 2 {
		t.Fatalf("expected 2 document symbols, got %#v", docSymbols)
	}

	names := make([]string, 0, len(docSymbols))
	for _, item := range docSymbols {
		sym := item.(map[string]any)
		names = append(names, sym["name"].(string))
	}
	sort.Strings(names)
	if names[0] != "a" || names[1] != "greet" {
		t.Fatalf("document symbols mismatch: %v", names)
	}
}

func TestServerWorkspaceSymbol(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	mustWrite(t, mainPath, `(ns app.main)
(defn greet "hello doc" [x] x)
(def answer 42)
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, "workspace/symbol", map[string]any{
			"query": "greet",
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	resp := findResponseByID(t, msgs, 2)
	items, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("workspace/symbol result must be an array: %#v", resp["result"])
	}
	if len(items) != 1 {
		t.Fatalf("expected one workspace symbol match, got %#v", items)
	}
	sym := items[0].(map[string]any)
	if sym["name"] != "greet" {
		t.Fatalf("workspace symbol name mismatch: %#v", sym)
	}
	if sym["containerName"] != "app.main" {
		t.Fatalf("workspace symbol container mismatch: %#v", sym)
	}
	location, ok := sym["location"].(map[string]any)
	if !ok {
		t.Fatalf("workspace symbol location missing: %#v", sym)
	}
	if location["uri"] != mainURI {
		t.Fatalf("workspace symbol uri mismatch: got=%v want=%s", location["uri"], mainURI)
	}
}

func TestServerHoverUnresolvedReturnsNullResult(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	mustWrite(t, mainPath, `(ns app.main)
missing-symbol
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, lspMethodHover, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 1, "character": 2},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	hoverResp := findResponseByID(t, msgs, 2)
	result, exists := hoverResp["result"]
	if !exists {
		t.Fatalf("hover response must include result field: %#v", hoverResp)
	}
	if result != nil {
		t.Fatalf("hover unresolved result should be null, got %#v", result)
	}
}

func TestServerDidSaveRefreshesWorkspace(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	defPath := filepath.Join(root, "defs.dog")
	defURI := toolindex.PathToFileURI(defPath)

	mustWrite(t, mainPath, `(ns app.main)
p23
`)
	mustWrite(t, defPath, `(ns app.defs)
(def foo 1)
`)

	s := NewServer(bytes.NewReader(nil), io.Discard, nil)
	if err := s.handleInitialize(message{
		ID:     rawID(t, 1),
		Method: lspMethodInitialize,
		Params: mustMarshalRaw(initializeParams{
			RootURI: toolindex.PathToFileURI(root),
		}),
	}); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	mainURI := toolindex.PathToFileURI(mainPath)
	before := s.state.index.CompletionCandidates(s.state.index.NamespaceForURI(mainURI), "p23")
	if slices.Contains(before, "p23saved") {
		t.Fatalf("p23saved should not exist before didSave: %#v", before)
	}

	mustWrite(t, defPath, `(ns app.defs)
(def p23saved 1)
`)
	if err := s.handleDidSave(mustMarshalRaw(didSaveParams{
		TextDocument: textDocumentIdentifier{URI: defURI},
	})); err != nil {
		t.Fatalf("didSave failed: %v", err)
	}

	after := s.state.index.CompletionCandidates(s.state.index.NamespaceForURI(mainURI), "p23")
	if !slices.Contains(after, "p23saved") {
		t.Fatalf("p23saved should exist after didSave: %#v", after)
	}
}

func TestServerDidSavePublishesTargetedDiagnostics(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	brokenPath := filepath.Join(root, "broken.dog")
	mainURI := toolindex.PathToFileURI(mainPath)
	brokenURI := toolindex.PathToFileURI(brokenPath)
	mustWrite(t, mainPath, `(ns app.main)
(def ok 1)
`)
	mustWrite(t, brokenPath, `(ns app.broken)
(missing-symbol 1)
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidSave, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
		}),
		req(2, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	foundMainDiag := false
	foundBrokenDiag := false
	for _, msg := range msgs {
		if msg["method"] != lspMethodPublishDiagnostics {
			continue
		}
		params := msg["params"].(map[string]any)
		if params["uri"] == mainURI {
			foundMainDiag = true
		}
		if params["uri"] == brokenURI {
			foundBrokenDiag = true
		}
	}
	if !foundMainDiag {
		t.Fatalf("save should publish diagnostics for saved file at minimum: %#v", msgs)
	}
	if foundBrokenDiag {
		t.Fatalf("save should not republish unrelated diagnostics: %#v", msgs)
	}
}

func TestServerDidSavePublishesDiagnosticsForTransitiveDependents(t *testing.T) {
	root := t.TempDir()
	aPath := filepath.Join(root, "a.dog")
	bPath := filepath.Join(root, "b.dog")
	cPath := filepath.Join(root, "c.dog")
	aURI := toolindex.PathToFileURI(aPath)
	cURI := toolindex.PathToFileURI(cPath)

	mustWrite(t, aPath, `(ns app.a)
(def base 1)
`)
	mustWrite(t, bPath, `(ns app.b
  (:require [app.a :refer [base]]))
(defn via-b [] base)
`)
	mustWrite(t, cPath, `(ns app.c
  (:require [app.b :refer [via-b]]))
(missing-in-c (via-b))
`)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidSave, map[string]any{
			"textDocument": map[string]any{"uri": aURI},
		}),
		req(2, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	foundCDiag := false
	for _, msg := range msgs {
		if msg["method"] != lspMethodPublishDiagnostics {
			continue
		}
		params := msg["params"].(map[string]any)
		if params["uri"] == cURI {
			foundCDiag = true
			break
		}
	}
	if !foundCDiag {
		t.Fatalf("didSave should publish diagnostics for transitive dependents: %#v", msgs)
	}
}

func TestServerUndefinedSymbolDiagnostics(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text": `(ns app.main)
(missing 1)
`,
			},
		}),
		req(2, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	diag := findNotification(t, msgs, lspMethodPublishDiagnostics)
	params := diag["params"].(map[string]any)
	items := params["diagnostics"].([]any)
	found := false
	for _, item := range items {
		msg := item.(map[string]any)["message"].(string)
		if strings.Contains(msg, "undefined symbol: missing") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("undefined symbol diagnostic was not published: %#v", items)
	}
}

func TestServerRefreshIndexIfStaleFallsBackOnManyChanges(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "main.dog"), `(ns app.main)
`)
	for i := 0; i < 33; i++ {
		mustWrite(t, filepath.Join(root, "d", fmt.Sprintf("f%02d.dog", i)), fmt.Sprintf(`(ns app.main)
(def oldsym%02d 1)
`, i))
	}

	s := NewServer(bytes.NewReader(nil), io.Discard, nil)
	if err := s.handleInitialize(message{
		ID:     rawID(t, 1),
		Method: lspMethodInitialize,
		Params: mustMarshalRaw(initializeParams{
			RootURI: toolindex.PathToFileURI(root),
		}),
	}); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	for i := 0; i < 33; i++ {
		mustWrite(t, filepath.Join(root, "d", fmt.Sprintf("f%02d.dog", i)), fmt.Sprintf(`(ns app.main)
(def newsymbol_changed_%02d 100)
`, i))
	}

	if err := s.refreshIndexIfStale(); err != nil {
		t.Fatalf("refreshIndexIfStale failed: %v", err)
	}

	candidates := s.state.index.CompletionCandidates("app.main", "newsymbol_changed_")
	if !slices.Contains(candidates, "newsymbol_changed_00") || !slices.Contains(candidates, "newsymbol_changed_32") {
		t.Fatalf("expected fallback rebuild to include updated symbols: %v", candidates)
	}
	if slices.Contains(candidates, "oldsym00") {
		t.Fatalf("old symbols should be removed after fallback rebuild: %v", candidates)
	}
}

func TestServerUndefinedSymbolNoFalsePositive(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text": `(ns app.main)
(def x 1)
(let [y 2] (+ x y))
`,
			},
		}),
		req(2, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	diag := findNotification(t, msgs, lspMethodPublishDiagnostics)
	params := diag["params"].(map[string]any)
	items := params["diagnostics"].([]any)
	for _, item := range items {
		msg := item.(map[string]any)["message"].(string)
		if strings.Contains(msg, "undefined symbol:") {
			t.Fatalf("unexpected undefined symbol diagnostic: %#v", items)
		}
	}
}

func TestServerUndefinedSymbolNoFalsePositiveForComparator(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mainURI := toolindex.PathToFileURI(mainPath)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{
			"textDocument": map[string]any{
				"uri":     mainURI,
				"version": 1,
				"text": `(ns app.main)
(defn fib [n]
  (if (<= n 1)
    n
    (+ n 1)))
`,
			},
		}),
		req(2, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	diag := findNotification(t, msgs, lspMethodPublishDiagnostics)
	params := diag["params"].(map[string]any)
	items := params["diagnostics"].([]any)
	for _, item := range items {
		msg := item.(map[string]any)["message"].(string)
		if strings.Contains(msg, "undefined symbol: <=") {
			t.Fatalf("unexpected undefined symbol diagnostic for <=: %#v", items)
		}
	}
}

func TestServerDefinitionStdlib(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mustWrite(t, mainPath, `(ns app.main)
(when true 1)
`)
	mainURI := toolindex.PathToFileURI(mainPath)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, lspMethodDefinition, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 1, "character": 2},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	defResp := findResponseByID(t, msgs, 2)
	locations := defResp["result"].([]any)
	if len(locations) == 0 {
		t.Fatalf("definition result is empty: %#v", defResp)
	}
	loc := locations[0].(map[string]any)
	uri := loc["uri"].(string)
	if !strings.HasPrefix(uri, "stdlib://doggo/") {
		t.Fatalf("expected stdlib uri, got %s", uri)
	}
}

func TestServerCompletionIgnoresDepsWithoutRetainedGeneration(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mustWrite(t, mainPath, `(ns app.main)
dep
`)
	mustWrite(t, filepath.Join(root, "doggo.mod"), `{:deps []}`)
	mustWrite(t, filepath.Join(root, ".doggo", "deps", "sample", "lib.dog"), `(ns dep.lib)
(def depfn 1)
`)
	mainURI := toolindex.PathToFileURI(mainPath)

	msgs := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, lspMethodCompletion, map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": 1, "character": 3},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	})

	compResp := findResponseByID(t, msgs, 2)
	compItems := compResp["result"].([]any)
	if containsCompletion(compItems, "depfn") {
		t.Fatalf("completion included obsolete dependencies without a retained generation: %#v", compItems)
	}
}

func runServer(t *testing.T, reqs []map[string]any, hooks ...func()) []map[string]any {
	t.Helper()
	var in bytes.Buffer
	inWriter := bufio.NewWriter(&in)
	for i, req := range reqs {
		if err := writeMessage(inWriter, req); err != nil {
			t.Fatalf("write request failed: %v", err)
		}
		if i < len(hooks) && hooks[i] != nil {
			hooks[i]()
		}
	}
	if err := inWriter.Flush(); err != nil {
		t.Fatalf("flush input failed: %v", err)
	}

	var out bytes.Buffer
	s := NewServer(bytes.NewReader(in.Bytes()), &out, nil)
	if err := s.Run(); err != nil {
		t.Fatalf("server run failed: %v", err)
	}

	return decodeMessages(t, out.Bytes())
}

func TestRouterDispatchesCompletion(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.dog")
	mustWrite(t, mainPath, "(ns demo.core)")

	resp := findResponseByID(t, runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(2, lspMethodCompletion, map[string]any{
			"textDocument": map[string]any{"uri": toolindex.PathToFileURI(mainPath)},
			"position":     map[string]any{"line": 0, "character": 0},
		}),
		req(3, lspMethodShutdown, map[string]any{}),
		notify(lspMethodExit, map[string]any{}),
	}), 2)
	if resp == nil {
		t.Fatal("nil response")
	}
}

func TestWorkspaceDirtyTrackingIsolatedFromTransport(t *testing.T) {
	ws := NewWorkspaceState()
	ws.MarkDirty("demo.dg")
	if !ws.IsDirty("demo.dg") {
		t.Fatal("workspace state not updated")
	}
}

func decodeMessages(t *testing.T, wire []byte) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0)
	reader := bufio.NewReader(bytes.NewReader(wire))
	for {
		payload, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read output message failed: %v", err)
		}
		decoded := make(map[string]any)
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("invalid output json: %v", err)
		}
		out = append(out, decoded)
	}
	return out
}

func findResponseByID(t *testing.T, msgs []map[string]any, id int) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		rawID, ok := msg["id"]
		if !ok {
			continue
		}
		if int(rawID.(float64)) == id {
			return msg
		}
	}
	t.Fatalf("response id=%d not found in %#v", id, msgs)
	return nil
}

func findNotification(t *testing.T, msgs []map[string]any, method string) map[string]any {
	t.Helper()
	for _, msg := range msgs {
		if msg["method"] == method {
			return msg
		}
	}
	t.Fatalf("notification %s not found in %#v", method, msgs)
	return nil
}

func rawID(t *testing.T, id int) *json.RawMessage {
	t.Helper()
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("marshal id failed: %v", err)
	}
	raw := json.RawMessage(data)
	return &raw
}

func req(id int, method string, params map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
}

func notify(method string, params map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
}

func containsCompletion(items []any, label string) bool {
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if entry["label"] == label {
			return true
		}
	}
	return false
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}
