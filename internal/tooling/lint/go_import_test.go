package lint

import (
	"path/filepath"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
)

func TestUndefinedSymbolsRecognizesRegisteredGoImports(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		undefined    []string
	}{
		{"README", `(import "strings") (println (strings/ToUpper "doggo"))`, nil},
		{"namespace", `(ns example) (import "strings") (defn upper [s] (strings/ToUpper s))`, nil},
		{"namespace transition", `(import "strings") (ns example) (strings/ToUpper "doggo")`, nil},
		{"short package name", `(import "net/http") (println http/StatusOK)`, nil},
		{"nested global import", `(defn load [] (import "strings")) (load) (strings/ToUpper "doggo")`, nil},
		{"unknown member", `(import "strings") (strings/NotAnExport "doggo")`, []string{"strings/NotAnExport"}},
		{"unimported", `(strings/ToUpper "doggo")`, []string{"strings/ToUpper"}},
		{"quoted import", `'(import "strings") (strings/ToUpper "doggo")`, []string{"strings/ToUpper"}},
		{"source order", `(strings/ToUpper "before") (import "strings") (strings/ToUpper "after")`, []string{"strings/ToUpper"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "hello.dog")
			idx, err := toolindex.BuildWorkspaceIndex(toolindex.BuildOptions{ProjectRoot: root, Overlay: map[string]string{path: tc.source}})
			if err != nil {
				t.Fatal(err)
			}
			issues := AnalyzeWithOptions(path, tc.source, AnalyzeOptions{Index: idx, EnableUndefinedSymbol: true})
			var got []string
			for _, issue := range issues {
				if strings.HasPrefix(issue.Message, "undefined symbol: ") {
					got = append(got, strings.TrimPrefix(issue.Message, "undefined symbol: "))
				} else {
					t.Errorf("unexpected diagnostic: %+v", issue)
				}
			}
			if strings.Join(got, "\n") != strings.Join(tc.undefined, "\n") {
				t.Fatalf("undefined symbols = %v, want %v", got, tc.undefined)
			}
		})
	}
}
