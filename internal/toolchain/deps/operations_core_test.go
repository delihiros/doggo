package deps

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"doggo/internal/project"
	"doggo/internal/testutil"
)

func TestDependencyMutationsWaitForGenerationReadersAndLeaveLockStale(t *testing.T) {
	tests := []struct {
		name    string
		initial []string
		mutate  func(string) error
		want    []string
	}{
		{name: "add", mutate: func(root string) error { return AddDependency(root, "./dep") }, want: []string{"./dep"}},
		{name: "remove", initial: []string{"./dep"}, mutate: func(root string) error { return RemoveDependency(root, "./dep") }, want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: test.initial}); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, ".doggo"), 0755); err != nil {
				t.Fatal(err)
			}
			owner, err := project.AcquireGenerationLease(root, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			lockBytes := []byte("prior-lock\n")
			if err := os.WriteFile(project.LockPath(root), lockBytes, 0644); err != nil {
				t.Fatal(err)
			}
			reader, err := project.AcquireGenerationLease(root, false)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- test.mutate(root) }()
			select {
			case err := <-done:
				t.Fatalf("mutation completed while reader held lease: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("mutation remained blocked after reader closed")
			}
			mod, err := project.LoadModFile(project.ModPath(root))
			if err != nil {
				t.Fatal(err)
			}
			if len(mod.Deps) != len(test.want) || (len(test.want) > 0 && mod.Deps[0] != test.want[0]) {
				t.Fatalf("deps = %#v, want %#v", mod.Deps, test.want)
			}
			got, err := os.ReadFile(project.LockPath(root))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(lockBytes) {
				t.Fatalf("mutation rewrote lock: %q", got)
			}
		})
	}
}

func TestDependencyMutationsRejectProjectDirectoryReplacement(t *testing.T) {
	tests := []struct {
		name    string
		initial []string
		mutate  func(string) error
	}{
		{name: "add", mutate: func(root string) error { return AddDependency(root, "./dep") }},
		{name: "remove", initial: []string{"./dep"}, mutate: func(root string) error { return RemoveDependency(root, "./dep") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "project")
			moved := filepath.Join(base, "project-original")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/original", Deps: test.initial}); err != nil {
				t.Fatal(err)
			}
			replacement := &project.ModFile{Module: "example/replacement", Deps: []string{"./outside"}}
			replaced := false
			afterDependencyLeaseForTest = func() {
				replaced = testutil.RenameRetained(t, root, moved)
				if !replaced {
					return
				}
				if err := os.Mkdir(root, 0755); err != nil {
					t.Fatal(err)
				}
				if err := project.WriteModFile(project.ModPath(root), replacement); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { afterDependencyLeaseForTest = nil })

			err := test.mutate(root)
			if !replaced {
				if err != nil {
					t.Fatalf("mutation after blocked project replacement: %v", err)
				}
				got, err := project.LoadModFile(project.ModPath(root))
				if err != nil || got.Module != "example/original" || (test.name == "add" && !reflect.DeepEqual(got.Deps, []string{"./dep"})) || (test.name == "remove" && len(got.Deps) != 0) {
					t.Fatalf("protected mod = %#v, %v", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "root path changed") {
				t.Fatalf("mutation error = %v, want root replacement rejection", err)
			}
			got, err := project.LoadModFile(project.ModPath(root))
			if err != nil {
				t.Fatal(err)
			}
			if got.Module != replacement.Module || !reflect.DeepEqual(got.Deps, replacement.Deps) {
				t.Fatalf("replacement mod was mutated: %#v", got)
			}
		})
	}
}

func TestAddDependencyStoresCanonicalDeclaration(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project"}); err != nil {
		t.Fatal(err)
	}
	if err := AddDependency(root, "./dep/../math"); err != nil {
		t.Fatal(err)
	}
	mod, err := project.LoadModFile(project.ModPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mod.Deps, []string{"./math"}) {
		t.Fatalf("deps = %#v, want canonical declaration", mod.Deps)
	}
}

func TestAddDependencyRejectsInvalidSourceWithoutChangingMod(t *testing.T) {
	for _, source := range []string{"github.com/acme/math@main", "./deps\\math"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			modPath := project.ModPath(root)
			if err := project.WriteModFile(modPath, &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(modPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := AddDependency(root, source); err == nil {
				t.Fatalf("AddDependency accepted invalid source %q", source)
			}
			after, err := os.ReadFile(modPath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatal("AddDependency changed doggo.mod after rejecting the source")
			}
		})
	}
}

func TestRemoveDependencyUsesCanonicalDeclaration(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project"}); err != nil {
		t.Fatal(err)
	}
	if err := AddDependency(root, "./dep/../math"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveDependency(root, "./dep/../math"); err != nil {
		t.Fatal(err)
	}
	mod, err := project.LoadModFile(project.ModPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(mod.Deps) != 0 {
		t.Fatalf("deps = %#v, want none", mod.Deps)
	}
}

func TestRemoveDependencyRejectsInvalidSourceWithoutChangingProjectFiles(t *testing.T) {
	root := t.TempDir()
	modPath := project.ModPath(root)
	if err := project.WriteModFile(modPath, &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".doggo"), 0755); err != nil {
		t.Fatal(err)
	}
	lockPath := project.LockPath(root)
	if err := os.WriteFile(lockPath, []byte("prior-lock\n"), 0644); err != nil {
		t.Fatal(err)
	}
	beforeMod, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveDependency(root, "github.com/acme/math@main"); err == nil {
		t.Fatal("RemoveDependency accepted an invalid source")
	}
	afterMod, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatal(err)
	}
	afterLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterMod, beforeMod) || !reflect.DeepEqual(afterLock, beforeLock) {
		t.Fatal("RemoveDependency changed project files after rejecting the source")
	}
}
