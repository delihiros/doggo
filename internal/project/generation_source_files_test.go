package project

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestGenerationSourceFilesUseRetainedManifest(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	source := session.Config.GenerationSources[0]
	depRoot := filepath.Join(generationPath(root, lock.Generation), filepath.FromSlash(lock.Deps[0].InstallDir))
	if err := os.WriteFile(filepath.Join(depRoot, "unselected.dog"), []byte("(def stale 1)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files, err := source.Files()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"doggo.mod", "lib/core.dog"}
	if !slices.Equal(files, want) {
		t.Fatalf("source files = %v, want retained manifest %v", files, want)
	}
	files[0] = "modified"
	again, err := source.Files()
	if err != nil || !slices.Equal(again, want) {
		t.Fatalf("caller modified retained enumeration: %v, %v", again, err)
	}
	filePath, err := source.FilePath("lib/core.dog")
	if err != nil {
		t.Fatal(err)
	}
	if filePath != filepath.Join(depRoot, "lib", "core.dog") {
		t.Fatalf("file path = %q", filePath)
	}
	for _, name := range []string{"../escape.dog", "/absolute.dog", "unselected.dog", "lib"} {
		if _, err := source.FilePath(name); err == nil {
			t.Fatalf("accepted non-file or unselected path %q", name)
		}
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Files(); err == nil {
		t.Fatal("closed source enumerated files")
	}
	if _, err := source.FilePath("lib/core.dog"); err == nil {
		t.Fatal("closed source exposed a file path")
	}
}
