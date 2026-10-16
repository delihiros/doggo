package index

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryNamespaceDeclarationRetainsImports(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "lib.dog")
	mainPath := filepath.Join(root, "main.dog")
	mainURI := PathToFileURI(mainPath)
	mustWriteFile(t, libPath, "(ns app.lib)\n(def target 7)\n")
	source := "(ns app.early (:require [app.lib :as lib :refer [target]]))\n(def own 1)\n(ns app.late)\n(def own 2)\n"
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root, Overlay: map[string]string{mainPath: source}})
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range []string{"lib/target", "target"} {
		if def, ok := idx.ResolveDefinition("app.early", symbol); !ok || def.Qualified != "app.lib/target" {
			t.Errorf("earlier namespace import %s = %+v, found %t", symbol, def, ok)
		}
	}
	if err := idx.IndexDocument(mainURI, "(ns replacement)\n(def own 3)\n"); err != nil {
		t.Fatal(err)
	}
	if info := idx.NamespaceInfo("app.early"); info != nil {
		t.Fatalf("replaced document retained old namespace metadata: %+v", info)
	}
}

func TestNamespaceAtPositionUsesUTF16Columns(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.dog")
	uri := PathToFileURI(path)
	source := "\"😀\" (ns one) (def first 1) (ns two) (def second 2)\n"
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root, Overlay: map[string]string{path: source}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ token, want string }{{"\"😀\"", "user"}, {"first", "one"}, {"second", "two"}} {
		position := PositionFromOffset(source, strings.Index(source, tc.token))
		if actual := idx.NamespaceAtPosition(uri, position); actual != tc.want {
			t.Errorf("namespace at %s (%+v) = %s, want %s", tc.token, position, actual, tc.want)
		}
	}
}

func TestCoreEmittedBindingsOnlyPreventDestinationCollisions(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "lib.dog")
	libURI := PathToFileURI(libPath)
	mainPath := filepath.Join(root, "main.dog")
	lib := "(ns app.lib)\n(def plain 1)\n(defrecord ^:probe Box [])\n(deftype Type [])\n(defprotocol Probe (read-val [this]))\n(do (defrecord Grouped []))\n'(defrecord Ghost [])\n"
	main := "(ns app.main (:require [app.lib :as lib :refer [->Box Probe read-val]]))\n(ns app.all (:require [app.lib :refer :all]))\n"
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root, Overlay: map[string]string{libPath: lib, mainPath: main}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ns, name string
		want     bool
	}{
		{"app.lib", "plain", true},
		{"app.lib", "Box", true},
		{"app.lib", "->Box", true},
		{"app.lib", "map->Box", true},
		{"app.lib", "Type", true},
		{"app.lib", "->Type", true},
		{"app.lib", "map->Type", false},
		{"app.lib", "Probe", true},
		{"app.lib", "read-val", true},
		{"app.lib", "Grouped", true},
		{"app.lib", "Ghost", false},
		{"app.main", "lib/Box", true},
		{"app.main", "Box", false},
		{"app.main", "->Box", true},
		{"app.main", "map->Box", false},
		{"app.main", "Probe", true},
		{"app.main", "read-val", true},
		{"app.all", "Box", false},
		{"app.all", "Type", false},
		{"app.all", "map->Box", true},
		{"app.all", "->Type", true},
	} {
		if actual := idx.HasGlobalBinding(tc.ns, tc.name); actual != tc.want {
			t.Errorf("HasGlobalBinding(%s, %s) = %t, want %t", tc.ns, tc.name, actual, tc.want)
		}
	}
	for _, name := range []string{"Box", "->Box", "map->Box", "Type", "Probe", "read-val"} {
		if def, found := idx.ResolveDefinition("app.lib", name); found {
			t.Errorf("collision-only name became a rename target: %+v", def)
		}
	}
	if err := idx.IndexDocument(libURI, "(ns replacement)\n(def plain 2)\n"); err != nil {
		t.Fatal(err)
	}
	if idx.HasGlobalBinding("app.lib", "Box") || idx.HasGlobalBinding("app.main", "->Box") {
		t.Fatal("document replacement retained emitted bindings")
	}
	if err := idx.IndexDocument(libURI, lib); err != nil {
		t.Fatal(err)
	}
	if err := idx.RemoveDocument(libURI); err != nil {
		t.Fatal(err)
	}
	if idx.HasGlobalBinding("app.lib", "Box") || idx.HasGlobalBinding("app.main", "->Box") {
		t.Fatal("document removal retained emitted bindings")
	}
}

func TestDoOrdinaryDeclarationsOnlyPreventDestinationCollisions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.dog")
	uri := PathToFileURI(path)
	source := `(ns demo)
(do (def value 2) (defonce once 3) (defn read-value [parameter] parameter)
    (defmacro read-form [] 5) (do (def nested 6))
    '(def quoted 7) (def ^{:probe (def metadata-only 8)} unrelated 9))
(ns other)
(do (def elsewhere 10))
`
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root, Overlay: map[string]string{path: source}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"value", "once", "read-value", "read-form", "nested", "unrelated"} {
		if !idx.HasGlobalBinding("demo", name) {
			t.Errorf("do declaration %s missing from collision bindings", name)
		}
		if def, found := idx.ResolveDefinition("demo", name); found {
			t.Errorf("do declaration became a rename target: %+v", def)
		}
	}
	for _, name := range []string{"parameter", "quoted", "metadata-only", "elsewhere"} {
		if idx.HasGlobalBinding("demo", name) {
			t.Errorf("unrelated/literal name %s became a collision binding", name)
		}
	}
	if !idx.HasGlobalBinding("other", "elsewhere") {
		t.Error("other namespace do declaration missing from collision bindings")
	}
	if err := idx.IndexDocument(uri, "(ns demo)\n2\n"); err != nil {
		t.Fatal(err)
	}
	if idx.HasGlobalBinding("demo", "value") || idx.HasGlobalBinding("other", "elsewhere") {
		t.Fatal("document replacement retained do collision bindings")
	}
}
