package depsnapshot

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotRejectsLocalReplacementModuleMismatch(t *testing.T) {
	for _, fixture := range []struct {
		name, childManifest, actual string
		cached                      bool
	}{
		{name: "different module", childManifest: "go.mod", actual: "module example.invalid/actual\n"},
		{name: "missing Go module", childManifest: "doggo.mod", actual: `{:module "example/child" :deps []}`},
		{name: "cached different module", childManifest: "go.mod", actual: "module example.invalid/actual\n", cached: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "z-parent", "go.mod"), "module example.invalid/parent\n\nreplace example.invalid/requested => ../a-child\n")
			writeFile(t, filepath.Join(root, "a-child", fixture.childManifest), fixture.actual)
			sources := []string{"./z-parent"}
			if fixture.cached {
				sources = append(sources, "./a-child")
			}
			_, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), sources)
			if err == nil {
				t.Fatal("snapshot accepted a local replacement without the requested Go module identity")
			}
			for _, want := range []string{"example.invalid/parent", "example.invalid/requested", "../a-child"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %v, want %q", err, want)
				}
			}
			if fixture.childManifest == "go.mod" && !strings.Contains(err.Error(), "example.invalid/actual") {
				t.Errorf("error = %v, want actual declared Go module", err)
			}
		})
	}
}
