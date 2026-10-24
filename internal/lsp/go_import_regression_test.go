package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
)

func TestServerGoImportDiagnosticsRefresh(t *testing.T) {
	root := t.TempDir()
	uri := toolindex.PathToFileURI(filepath.Join(root, "hello.dog"))
	for _, prefix := range []string{"", "(ns app.main)\n"} {
		t.Run(prefix, func(t *testing.T) {
			messages := runServer(t, []map[string]any{
				req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
				notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{
					"uri": uri, "version": 1, "text": prefix + "(import \"strings\")\n(println (strings/ToUpper \"doggo\"))\n",
				}}),
				notify(lspMethodDidChange, map[string]any{
					"textDocument":   map[string]any{"uri": uri, "version": 2},
					"contentChanges": []map[string]any{{"text": prefix + "(println (strings/ToUpper \"doggo\"))\n"}},
				}),
				req(2, lspMethodShutdown, map[string]any{}),
				notify(lspMethodExit, map[string]any{}),
			})
			var diagnostics [][]any
			for _, msg := range messages {
				if msg["method"] == lspMethodPublishDiagnostics {
					params := msg["params"].(map[string]any)
					if params["uri"] == uri {
						diagnostics = append(diagnostics, params["diagnostics"].([]any))
					}
				}
			}
			if len(diagnostics) != 2 || len(diagnostics[0]) != 0 {
				t.Fatalf("valid import must be diagnostic-free: %#v", diagnostics)
			}
			if len(diagnostics[1]) != 1 || !strings.Contains(diagnostics[1][0].(map[string]any)["message"].(string), "undefined symbol: strings/ToUpper") {
				t.Fatalf("removing import must restore undefined-symbol checking: %#v", diagnostics)
			}
		})
	}
}
