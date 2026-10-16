package index

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestRequireRenameResolutionAndLifecycle(t *testing.T) {
	for _, ns := range []string{"app.lib", ":app.lib"} {
		for _, refer := range []string{"[target ->Thing]", ":all"} {
			root := t.TempDir()
			libURI := PathToFileURI(filepath.Join(root, "lib.dog"))
			mainURI := PathToFileURI(filepath.Join(root, "main.dog"))
			idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			if err := idx.IndexDocument(mainURI, "(ns app.main (:require ["+ns+" :rename {target localAlias ->Thing makeThing} :refer "+refer+"]))"); err != nil {
				t.Fatal(err)
			}
			if _, ok := idx.ResolveDefinition("app.main", "localAlias"); ok {
				t.Fatal("resolved before source indexed")
			}
			if err := idx.IndexDocument(libURI, "(ns app.lib)\n(def target 7)\n(defrecord Thing [x])"); err != nil {
				t.Fatal(err)
			}
			if def, ok := idx.ResolveDefinition("app.main", "localAlias"); !ok || def.Qualified != "app.lib/target" {
				t.Fatalf("alias resolution = %+v %v", def, ok)
			}
			if _, ok := idx.ResolveDefinition("app.main", "target"); ok {
				t.Fatal("renamed source remains referred")
			}
			if _, ok := idx.ResolveDefinition("app.main", "app.main/localAlias"); ok {
				t.Fatal("alias incorrectly exported")
			}
			if !idx.HasGlobalBinding("app.main", "makeThing") || idx.HasGlobalBinding("app.main", "->Thing") {
				t.Fatal("emitted constructor alias mismatch")
			}
			if !slices.Contains(idx.CompletionCandidates("app.main", "local"), "localAlias") {
				t.Fatal("completion missing alias")
			}
			copy := cloneNamespaceInfo(idx.NamespaceInfo("app.main"))
			if len(copy.ReferAll) > 0 {
				copy.ReferAll[0].Renames["target"] = "corrupted"
				if idx.NamespaceInfo("app.main").ReferAll[0].Renames["target"] != "localAlias" {
					t.Fatal("clone shares alias maps")
				}
				idx.NamespaceInfo("app.main").ReferAll[0].Renames["target"] = "corrupted"
				idx.rebuildNamespaces()
				if idx.NamespaceInfo("app.main").ReferAll[0].Renames["target"] != "localAlias" {
					t.Fatal("merge shares alias maps with stored document")
				}
			}
			if err := idx.RemoveDocument(libURI); err != nil {
				t.Fatal(err)
			}
			if idx.HasGlobalBinding("app.main", "localAlias") || idx.HasGlobalBinding("app.main", "makeThing") {
				t.Fatal("stale alias after source removal")
			}
			if err := idx.IndexDocument(mainURI, "(ns app.main)"); err != nil {
				t.Fatal(err)
			}
			if idx.HasGlobalBinding("app.main", "localAlias") {
				t.Fatal("stale alias after document replacement")
			}
		}
	}
}

func TestRequireKeywordAcceptanceIsNamespaceOnly(t *testing.T) {
	root := t.TempDir()
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexDocument(PathToFileURI(filepath.Join(root, "lib.dog")), "(ns app.lib) (def target 7)"); err != nil {
		t.Fatal(err)
	}
	for _, options := range []string{":as :lib", ":refer [:target]", ":refer [target] :rename {:target localAlias}", ":refer [target] :rename {target :localAlias}"} {
		if err := idx.IndexDocument(PathToFileURI(filepath.Join(root, "main.dog")), "(ns app.main (:require [:app.lib "+options+"]))"); err != nil {
			t.Fatal(err)
		}
		for _, symbol := range []string{"lib/target", "localAlias"} {
			if def, ok := idx.ResolveDefinition("app.main", symbol); ok {
				t.Fatalf("keyword widened symbol operands %q: %+v", options, def)
			}
		}
	}
}

func TestRequireRenameResolvesCanonicalSourceWithoutImporterAliases(t *testing.T) {
	root := t.TempDir()
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"lib":   "(ns app.lib) (def target 7)",
		"other": "(ns app.other) (def target 8)",
		"main":  "(ns app.main (:require [:app.lib :refer [target] :rename {target localAlias}] [app.other :as app.lib]))",
	} {
		if err := idx.IndexDocument(PathToFileURI(filepath.Join(root, name+".dog")), source); err != nil {
			t.Fatal(err)
		}
	}
	for symbol, qualified := range map[string]string{"localAlias": "app.lib/target", "app.lib/target": "app.other/target"} {
		if def, ok := idx.ResolveDefinition("app.main", symbol); !ok || def.Qualified != qualified {
			t.Fatalf("%s resolved through importer aliases: %+v %v", symbol, def, ok)
		}
	}
}

func TestRequireAllIgnoresUnusedRenameAliases(t *testing.T) {
	root := t.TempDir()
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexDocument(PathToFileURI(filepath.Join(root, "lib.dog")), "(ns app.lib) (def target 7) (defrecord Thing [x])"); err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexDocument(PathToFileURI(filepath.Join(root, "main.dog")), "(ns app.main (:require [:app.lib :refer :all :rename {missing target alsoMissing ->Thing}]))"); err != nil {
		t.Fatal(err)
	}
	if def, ok := idx.ResolveDefinition("app.main", "target"); !ok || def.Qualified != "app.lib/target" {
		t.Fatalf("unused rename hid target: %+v %v", def, ok)
	}
	if !idx.HasGlobalBinding("app.main", "->Thing") {
		t.Fatal("unused rename hid emitted constructor")
	}
}

func TestRequireAllKeepsSeparateSpecs(t *testing.T) {
	root := t.TempDir()
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexDocument(PathToFileURI(filepath.Join(root, "lib.dog")), "(ns app.lib) (def target 7)"); err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexDocument(PathToFileURI(filepath.Join(root, "main.dog")), "(ns app.main (:require [app.lib :refer :all :rename {target localAlias}] [app.lib :refer :all]))"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"target", "localAlias"} {
		if def, ok := idx.ResolveDefinition("app.main", name); !ok || def.Qualified != "app.lib/target" {
			t.Fatalf("%s = %+v %v", name, def, ok)
		}
	}
}
