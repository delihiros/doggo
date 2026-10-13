package project

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCloneVerifiedRootHasIndependentLifetime(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, err := root.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	clone, err := CloneVerifiedRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := clone.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("clone lost retained identity after source close: %v", err)
	}
}

func TestCloneVerifiedRootJoinsStatAndCloseFailures(t *testing.T) {
	closeFailure := errors.New("close clone")
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	afterVerifiedRootCloneForTest = func(clone *os.Root) { _ = clone.Close() }
	closeVerifiedRootClone = func(*os.Root) error { return closeFailure }
	t.Cleanup(func() {
		afterVerifiedRootCloneForTest = nil
		closeVerifiedRootClone = (*os.Root).Close
	})

	clone, err := CloneVerifiedRoot(root)
	if clone != nil || !errors.Is(err, os.ErrClosed) || !errors.Is(err, closeFailure) {
		t.Fatalf("CloneVerifiedRoot = (%v, %v), want closed stat and close errors", clone, err)
	}
}

func TestRootVerificationPreservesFilesystemErrors(t *testing.T) {
	parentPath := t.TempDir()
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := parent.Mkdir("child", 0755); err != nil {
		t.Fatal(err)
	}
	child, err := OpenVerifiedSubroot(parent, "child")
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"root":  VerifyRootPath(filepath.Join(parentPath, "child"), child),
		"child": VerifySubroot(parent, "child", child),
	} {
		if !errors.Is(err, ErrOwnershipChanged) || !errors.Is(err, os.ErrClosed) {
			t.Errorf("%s verification = %v, want ownership and closed-root errors", name, err)
		}
	}
}

func TestRuntimeLoadPathsContainOnlyLiveProjectDirectories(t *testing.T) {
	root := t.TempDir()
	retained, _, err := openVerifiedRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer retained.Close()
	paths, err := liveProjectLoadPathsRoot(retained, root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{root}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("load paths = %v, want %v", paths, want)
	}

	if err := os.Mkdir(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	paths, err = liveProjectLoadPathsRoot(retained, root)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{root, filepath.Join(root, "src")}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("load paths with src = %v, want %v", paths, want)
	}
}
