package tester

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveTargetFilesDoesNotExcludeTraversalRoot(t *testing.T) {
	for _, name := range []string{"ordinary", "docs", "examples", "vendor", "testdata"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), name)
			target := filepath.Join(root, "test", "failing_test.dog")
			files := []string{target, filepath.Join(root, "non_test_source.dog")}
			for _, child := range []string{".doggo", ".git", "docs", "examples", "testdata", "vendor"} {
				files = append(files, filepath.Join(root, child, "ignored_test.dog"))
			}
			for _, path := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("(ns sample)\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			for _, paths := range [][]string{nil, {root}, {filepath.Join(root, "test")}, {target}} {
				got, err := ResolveTargetFiles(root, paths)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, []string{target}) {
					t.Fatalf("paths %v found %v, want only %s", paths, got, target)
				}
			}
			explicit := filepath.Join(root, "docs", "ignored_test.dog")
			got, err := ResolveTargetFiles(root, []string{explicit})
			if err != nil || !reflect.DeepEqual(got, []string{explicit}) {
				t.Fatalf("explicit file found %v (%v), want %s", got, err, explicit)
			}
		})
	}
}
