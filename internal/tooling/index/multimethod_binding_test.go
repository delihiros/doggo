package index

import (
	"path/filepath"
	"testing"
)

func TestMultimethodBindingsPreventDestinationCollisions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lib.dog")
	uri := PathToFileURI(path)
	source := "(ns app.lib)\n(defmulti direct (fn [x] x))\n(do (defmulti ^:probe grouped (fn [x] x)))\n'(defmulti literal (fn [x] x))\n(ns other)\n(defmulti elsewhere (fn [x] x))\n"
	main := "(ns app.main (:require [app.lib :as lib :refer [direct]]))\n(ns app.all (:require [app.lib :refer :all]))\n"
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root, Overlay: map[string]string{path: source, filepath.Join(root, "main.dog"): main}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ ns, name string }{{"app.lib", "direct"}, {"app.lib", "grouped"}, {"other", "elsewhere"}, {"app.main", "direct"}, {"app.main", "lib/grouped"}, {"app.all", "grouped"}} {
		if !idx.HasGlobalBinding(tc.ns, tc.name) {
			t.Errorf("multimethod binding missing: %s %s", tc.ns, tc.name)
		}
	}
	for _, name := range []string{"literal", "elsewhere", "x"} {
		if idx.HasGlobalBinding("app.lib", name) {
			t.Errorf("literal/unrelated/local became binding: %s", name)
		}
	}
	if err := idx.IndexDocument(uri, "(ns app.lib)\n2\n"); err != nil {
		t.Fatal(err)
	}
	if idx.HasGlobalBinding("app.lib", "direct") || idx.HasGlobalBinding("other", "elsewhere") {
		t.Fatal("document replacement retained multimethod bindings")
	}
}
