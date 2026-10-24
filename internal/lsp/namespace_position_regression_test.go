package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
)

func TestPositionHandlersUseActiveNamespace(t *testing.T) {
	for _, tc := range []struct {
		name, source, use, qualified, completion string
		definitionLine                           int
	}{
		{"earlier namespace", "(ns early)\n(def target 1)\n(def earlyOnly 1)\ntarget\n(ns late)\n(def target 2)\n(def lateOnly 2)\ntarget\n", "target\n(ns", "early/target", "earlyOnly", 1},
		{"final namespace", "(ns early)\n(def target 1)\n(def earlyOnly 1)\n(ns late)\n(def target 2)\n(def lateOnly 2)\ntarget\n", "target\n", "late/target", "lateOnly", 4},
		{"UTF16 same line", "\"😀\" (ns early) (def target 1) target (ns late) (def target 2)\n", "target (ns", "early/target", "target", 0},
		{"single namespace", "(ns early)\n(def target 1)\ntarget\n", "target\n", "early/target", "target", 1},
		{"earlier namespace alias", "(ns lib)\n(def target 1)\n(ns early (:require [lib :as x]))\nx/target\n(ns late)\n(def target 2)\n", "x/target", "lib/target", "x/target", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
			position := byteOffsetToLSPPosition(tc.source, strings.Index(tc.source, tc.use))
			completionPosition := position
			// Alias completion must use the namespace at the request position.
			if strings.HasPrefix(tc.use, "x/") {
				completionPosition.Character += 2
			}
			params := map[string]any{"textDocument": map[string]any{"uri": uri}, "position": position}
			messages := runServer(t, []map[string]any{
				req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
				notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": tc.source, "version": 1}}),
				req(2, lspMethodDefinition, params), req(3, lspMethodHover, params),
				req(4, lspMethodCompletion, map[string]any{"textDocument": params["textDocument"], "position": completionPosition}),
				notify(lspMethodExit, map[string]any{}),
			})
			definition := findResponseByID(t, messages, 2)["result"].([]any)
			if len(definition) != 1 {
				t.Errorf("definition=%v", definition)
			} else {
				location := definition[0].(map[string]any)
				start := decodeEditorRange(t, location["range"]).Start
				if location["uri"] != uri || start.Line != tc.definitionLine {
					t.Errorf("definition=%v want line=%d", definition, tc.definitionLine)
				}
				if tc.name == "UTF16 same line" && start.Character != byteOffsetToLSPPosition(tc.source, strings.Index(tc.source, "target")).Character {
					t.Errorf("UTF16 definition=%v", definition)
				}
			}
			hover, ok := findResponseByID(t, messages, 3)["result"].(map[string]any)
			if !ok || !strings.Contains(hover["contents"].(map[string]any)["value"].(string), "`"+tc.qualified+"`") {
				t.Errorf("hover=%v want=%s", hover, tc.qualified)
			}
			items := findResponseByID(t, messages, 4)["result"].([]any)
			found := false
			for _, item := range items {
				if item.(map[string]any)["label"] == tc.completion {
					found = true
				}
			}
			if !found {
				t.Errorf("completion missing %s: %v", tc.completion, items)
			}
		})
	}
}
