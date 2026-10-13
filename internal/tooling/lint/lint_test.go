package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
)

func TestAnalyzeWithOptions_UndefinedSymbol(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "defs.dog"), `(ns app.main)
(def known 1)
`)

	idx, err := toolindex.BuildWorkspaceIndex(toolindex.BuildOptions{
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("BuildWorkspaceIndex failed: %v", err)
	}

	source := `(ns app.main)
(known)
(missing)
`
	issues := AnalyzeWithOptions("main.dog", source, AnalyzeOptions{
		Index:                 idx,
		CurrentNamespace:      "app.main",
		EnableUndefinedSymbol: true,
	})

	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "undefined symbol: missing") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected undefined symbol diagnostics, got %+v", issues)
	}
}

func TestAnalyzeWithOptions_UndefinedSymbolNoFalsePositiveForLocals(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "defs.dog"), `(ns app.main)
(def known 1)
`)

	idx, err := toolindex.BuildWorkspaceIndex(toolindex.BuildOptions{
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("BuildWorkspaceIndex failed: %v", err)
	}

	source := `(ns app.main)
(let [x 1]
  (+ x known))
`
	issues := AnalyzeWithOptions("main.dog", source, AnalyzeOptions{
		Index:                 idx,
		CurrentNamespace:      "app.main",
		EnableUndefinedSymbol: true,
	})

	for _, issue := range issues {
		if strings.Contains(issue.Message, "undefined symbol:") {
			t.Fatalf("unexpected undefined symbol diagnostics: %+v", issues)
		}
	}
}

func TestAnalyzeWithOptions_UndefinedSymbolNoFalsePositiveForComparatorHead(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "defs.dog"), `(ns app.main)
`)

	idx, err := toolindex.BuildWorkspaceIndex(toolindex.BuildOptions{
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("BuildWorkspaceIndex failed: %v", err)
	}

	source := `(ns app.main)
(defn fib [n]
  (if (<= n 1)
    n
    (+ n 1)))
`
	issues := AnalyzeWithOptions("main.dog", source, AnalyzeOptions{
		Index:                 idx,
		CurrentNamespace:      "app.main",
		EnableUndefinedSymbol: true,
	})

	for _, issue := range issues {
		if strings.Contains(issue.Message, "undefined symbol: <=") {
			t.Fatalf("unexpected undefined symbol diagnostics: %+v", issues)
		}
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}
