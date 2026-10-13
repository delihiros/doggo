package index

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestResolveDefinitionAliasAndExclude(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "app", "lib.dog"), `(ns app.lib)
(def bar 42)
`)
	mustWriteFile(t, filepath.Join(root, "app", "ref.dog"), `(ns app.ref)
(def foo 1)
`)
	mustWriteFile(t, filepath.Join(root, "app", "main.dog"), `(ns app.main
  (:require [app.lib :as lib]
            [app.ref :refer [foo]])
  (:refer-clojure :exclude [map]))
`)

	idx, err := BuildWorkspaceIndex(BuildOptions{
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("BuildWorkspaceIndex failed: %v", err)
	}

	def, ok := idx.ResolveDefinition("app.main", "lib/bar")
	if !ok {
		t.Fatalf("definition for lib/bar not found")
	}
	if def.Namespace != "app.lib" || def.Name != "bar" {
		t.Fatalf("unexpected definition: %+v", def)
	}

	if _, ok := idx.ResolveDefinition("app.main", "map"); ok {
		t.Fatalf("map should be excluded by :refer-clojure :exclude")
	}

	candidates := idx.CompletionCandidates("app.main", "fo")
	foundFoo := false
	for _, c := range candidates {
		if c == "foo" {
			foundFoo = true
			break
		}
	}
	if !foundFoo {
		t.Fatalf("completion should include foo: %v", candidates)
	}
}

func TestBuildWorkspaceIndexIgnoresUnselectedDeps(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "doggo.mod"), `{:deps []}`)
	mustWriteFile(t, filepath.Join(root, ".doggo", "deps", "sample", "lib.dog"), `(ns dep.lib)
(def depfn 1)
`)

	idx, err := BuildWorkspaceIndex(BuildOptions{
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("BuildWorkspaceIndex failed: %v", err)
	}

	if _, ok := idx.ResolveDefinition("user", "dep.lib/depfn"); ok {
		t.Fatalf("obsolete dependencies were indexed without a retained generation source")
	}
}

func TestCollectWorkspaceFiles(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "app", "main.dog")
	depPath := filepath.Join(root, ".doggo", "deps", "sample", "lib.dog")
	mustWriteFile(t, mainPath, `(ns app.main)`)
	mustWriteFile(t, filepath.Join(root, ".git", "ignored.dog"), `(ns ignored.git)`)
	mustWriteFile(t, filepath.Join(root, ".doggo", "ignored.dog"), `(ns ignored.doggo)`)
	mustWriteFile(t, depPath, `(ns dep.lib)`)

	got, err := CollectWorkspaceFiles(root)
	if err != nil {
		t.Fatalf("CollectWorkspaceFiles failed: %v", err)
	}
	if len(got) != 1 || got[0] != mainPath {
		t.Fatalf("CollectWorkspaceFiles without deps = %v, want [%s]", got, mainPath)
	}

}

func TestCompletionCandidatesQualifiedAlias(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "app", "lib.dog"), `(ns app.lib)
(def bar 1)
(def baz 2)
`)
	mustWriteFile(t, filepath.Join(root, "app", "main.dog"), `(ns app.main
  (:require [app.lib :as lib]))
`)

	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("BuildWorkspaceIndex failed: %v", err)
	}

	got := idx.CompletionCandidates("app.main", "lib/ba")
	if !slices.Contains(got, "lib/bar") || !slices.Contains(got, "lib/baz") {
		t.Fatalf("qualified completion mismatch: %v", got)
	}
}

func TestIndexDocumentAndDocumentSymbols(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "app", "main.dog")
	mustWriteFile(t, mainPath, `(ns app.main)
(def one 1)
`)

	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("BuildWorkspaceIndex failed: %v", err)
	}

	uri := PathToFileURI(mainPath)
	if err := idx.IndexDocument(uri, `(ns app.main)
(defn two "two doc" [x] x)
`); err != nil {
		t.Fatalf("IndexDocument failed: %v", err)
	}

	if _, ok := idx.ResolveDefinition("app.main", "one"); ok {
		t.Fatalf("old symbol should be removed after IndexDocument")
	}
	def, ok := idx.ResolveDefinition("app.main", "two")
	if !ok {
		t.Fatalf("new symbol should exist after IndexDocument")
	}
	if def.Kind != "defn" {
		t.Fatalf("unexpected def kind: %s", def.Kind)
	}

	symbols := idx.DocumentSymbols(uri)
	if len(symbols) != 1 || symbols[0].Name != "two" {
		t.Fatalf("document symbols mismatch: %+v", symbols)
	}

	if err := idx.RemoveDocument(uri); err != nil {
		t.Fatalf("RemoveDocument failed: %v", err)
	}
	if _, ok := idx.ResolveDefinition("app.main", "two"); ok {
		t.Fatalf("symbol should be removed after RemoveDocument")
	}
}

func TestResolveHover(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "main.dog"), `(ns app.main)
(defn greet "hello doc" [x] x)
`)

	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("BuildWorkspaceIndex failed: %v", err)
	}

	hover, ok := idx.ResolveHover("app.main", "greet")
	if !ok {
		t.Fatalf("hover not found")
	}
	if hover.Qualified != "app.main/greet" {
		t.Fatalf("qualified mismatch: %+v", hover)
	}
	if hover.Kind != "defn" {
		t.Fatalf("kind mismatch: %+v", hover)
	}
	if hover.Doc != "hello doc" {
		t.Fatalf("doc mismatch: %+v", hover)
	}
}

func TestWorkspaceSymbols(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "main.dog"), `(ns app.main)
(defn greet [x] x)
(def answer 42)
`)

	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("BuildWorkspaceIndex failed: %v", err)
	}

	symbols := idx.WorkspaceSymbols("greet")
	if len(symbols) != 1 {
		t.Fatalf("expected one workspace symbol, got %+v", symbols)
	}
	if symbols[0].Qualified != "app.main/greet" {
		t.Fatalf("workspace symbol mismatch: %+v", symbols[0])
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
