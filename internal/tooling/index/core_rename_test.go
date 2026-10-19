package index

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestV7CoreRenameResolutionAndLifecycle(t *testing.T) {
	for _, tc := range []struct{ source, namespace, kind string }{
		{"inc", "core", "builtin"},
		{"mapcat", "doggo.core", "defn"},
		{"for", "doggo.core", "defmacro"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			root := t.TempDir()
			uri := PathToFileURI(filepath.Join(root, "main.dog"))
			idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			if err := idx.IndexDocument(uri, "(ns audit (:refer-clojure :rename {"+tc.source+" localAlias}))"); err != nil {
				t.Fatal(err)
			}
			def, binding, ok := idx.ResolveDefinitionWithBinding("audit", "localAlias")
			if !ok || def.Name != tc.source || def.Namespace != tc.namespace || def.Kind != tc.kind || !binding.ExplicitRename || binding.SourceName != tc.source || binding.Namespace != tc.namespace {
				t.Fatalf("alias resolution = %+v / %+v / %v", def, binding, ok)
			}
			if tc.kind == "builtin" && def.URI != "" {
				t.Fatalf("builtin has fabricated source URI: %+v", def)
			}
			if !idx.HasGlobalBinding("audit", "localAlias") || idx.HasGlobalBinding("audit", tc.source) {
				t.Fatal("alias or original collision binding mismatch")
			}
			if _, ok := idx.ResolveDefinition("audit", "audit/localAlias"); ok {
				t.Fatal("core alias incorrectly exported")
			}
			if !slices.Contains(idx.CompletionCandidates("audit", "local"), "localAlias") {
				t.Fatal("completion missing core alias")
			}
			idx.rebuildNamespaces()
			if !idx.HasGlobalBinding("audit", "localAlias") {
				t.Fatal("alias lost after namespace rebuild")
			}
			if err := idx.IndexDocument(uri, "(ns audit)"); err != nil {
				t.Fatal(err)
			}
			if idx.HasGlobalBinding("audit", "localAlias") {
				t.Fatal("stale alias after replacement")
			}
			if err := idx.IndexDocument(uri, "(ns audit (:refer-clojure :rename {"+tc.source+" localAlias}))"); err != nil {
				t.Fatal(err)
			}
			if err := idx.RemoveDocument(uri); err != nil {
				t.Fatal(err)
			}
			if idx.HasGlobalBinding("audit", "localAlias") {
				t.Fatal("stale alias after removal")
			}
		})
	}
}

func TestV7CoreReferOptionsFollowSourceOrder(t *testing.T) {
	for _, tc := range []struct {
		options         string
		original, alias bool
	}{
		{":exclude [for] :refer [for]", true, false},
		{":refer [for] :exclude [for]", false, false},
		{":exclude [for] :refer :all", true, false},
		{":rename {for collect} :refer [for]", true, true},
		{":refer [for] :rename {for collect}", false, true},
		{":exclude [for] :rename {for collect}", false, true},
		{":rename {for collect} :exclude [collect]", false, true},
	} {
		t.Run(tc.options, func(t *testing.T) {
			idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			uri := PathToFileURI(filepath.Join(t.TempDir(), "main.dog"))
			if err := idx.IndexDocument(uri, "(ns audit (:refer-clojure "+tc.options+"))"); err != nil {
				t.Fatal(err)
			}
			if got := idx.HasGlobalBinding("audit", "for"); got != tc.original {
				t.Errorf("original binding = %v, want %v", got, tc.original)
			}
			if got := idx.HasGlobalBinding("audit", "collect"); got != tc.alias {
				t.Errorf("alias binding = %v, want %v", got, tc.alias)
			}
		})
	}
}

func TestV7CoreRenameUsesCanonicalSource(t *testing.T) {
	root := t.TempDir()
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"other": "(ns other) (def for 9)",
		"main":  "(ns audit (:require [other :as doggo.core]) (:refer-clojure :rename {for collect missing absent}))",
	} {
		if err := idx.IndexDocument(PathToFileURI(filepath.Join(root, name+".dog")), source); err != nil {
			t.Fatal(err)
		}
	}
	if def, ok := idx.ResolveDefinition("audit", "collect"); !ok || def.Qualified != "doggo.core/for" {
		t.Fatalf("core alias used importer namespace alias: %+v / %v", def, ok)
	}
	if def, ok := idx.ResolveDefinition("audit", "doggo.core/for"); !ok || def.Qualified != "other/for" {
		t.Fatalf("qualified source did not use namespace alias: %+v / %v", def, ok)
	}
	if idx.HasGlobalBinding("audit", "absent") {
		t.Fatal("unknown core source introduced a binding")
	}
}

func TestV7CoreReferOptionsAcrossNamespaceDeclarations(t *testing.T) {
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	uri := PathToFileURI(filepath.Join(t.TempDir(), "main.dog"))
	if err := idx.IndexDocument(uri, "(ns audit (:refer-clojure :exclude [for mapcat]))\n(ns audit (:refer-clojure :refer [for]))"); err != nil {
		t.Fatal(err)
	}
	if !idx.HasGlobalBinding("audit", "for") || idx.HasGlobalBinding("audit", "mapcat") {
		t.Fatal("later :refer did not preserve unrelated excludes")
	}
	if err := idx.IndexDocument(uri, "(ns audit (:refer-clojure :exclude [for mapcat]))\n(ns audit (:refer-clojure :refer :all))"); err != nil {
		t.Fatal(err)
	}
	if !idx.HasGlobalBinding("audit", "for") || !idx.HasGlobalBinding("audit", "mapcat") {
		t.Fatal("later :refer :all did not clear excludes")
	}
}

func TestV7DocumentMacroResolverKeepsForeignMetadata(t *testing.T) {
	root := t.TempDir()
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	importsURI := PathToFileURI(filepath.Join(root, "imports.dog"))
	mainURI := PathToFileURI(filepath.Join(root, "main.dog"))
	if err := idx.IndexDocument(importsURI, "(ns audit (:require [doggo.core :as c]) (:refer-clojure :rename {for collect}))"); err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexDocument(mainURI, "(ns audit)\n(c/for [x [1]] x)\n(ns audit (:refer-clojure :exclude [for]))\n(def collect identity)"); err != nil {
		t.Fatal(err)
	}
	resolver := idx.NewDocumentMacroResolver(mainURI)
	position := Position{Line: 2, Column: 2}
	for _, head := range []string{"c/for", "collect"} {
		if got := resolver.CoreMacroName(position, head, ""); got != "for" {
			t.Errorf("foreign metadata for %s resolved to %q", head, got)
		}
	}
	resolver.Define(Position{Line: 4, Column: 6}, "collect", "def")
	if got := resolver.CoreMacroName(Position{Line: 4, Column: 20}, "collect", ""); got != "" {
		t.Errorf("current global binding did not shadow imported macro: %q", got)
	}
	if idx.CoreMacroName("audit", "c/for") != "for" || idx.CoreMacroName("audit", "for") != "" {
		t.Fatal("document resolver mutated workspace metadata")
	}
	if def, ok := idx.ResolveDefinition("audit", "collect"); !ok || def.Kind != "def" {
		t.Fatalf("document resolver mutated workspace declarations: %+v / %v", def, ok)
	}
}
