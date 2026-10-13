//go:build linux || darwin

package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveRetainedFilePreservesReplacementAtFinalIdentityCheck(t *testing.T) {
	parentPath := t.TempDir()
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := parent.WriteFile("owned", []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}
	expected, err := parent.Lstat("owned")
	if err != nil {
		t.Fatal(err)
	}

	beforeRetainedRemoveForTest = func(gotParent *os.Root, name string) {
		if gotParent != parent || name != "owned" {
			return
		}
		if err := os.Rename(filepath.Join(parentPath, name), filepath.Join(parentPath, name+"-retained")); err != nil {
			t.Fatal(err)
		}
		if err := parent.WriteFile(name, []byte("replacement"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeRetainedRemoveForTest = nil })

	err = removeRetainedFile(parent, "owned", expected)
	if !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("removeRetainedFile error = %v, want ownership failure", err)
	}
	content, readErr := parent.ReadFile("owned")
	if readErr != nil || string(content) != "replacement" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
}

func TestRemoveRetainedSymlinkPreservesReplacementAtFinalIdentityCheck(t *testing.T) {
	parentPath := t.TempDir()
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := os.Symlink("first", filepath.Join(parentPath, "owned")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	expected, err := parent.Lstat("owned")
	if err != nil {
		t.Fatal(err)
	}

	beforeRetainedRemoveForTest = func(gotParent *os.Root, name string) {
		if gotParent != parent || name != "owned" {
			return
		}
		if err := os.Rename(filepath.Join(parentPath, name), filepath.Join(parentPath, name+"-retained")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("replacement", filepath.Join(parentPath, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeRetainedRemoveForTest = nil })

	err = removeRetainedFile(parent, "owned", expected)
	if !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("removeRetainedFile error = %v, want ownership failure", err)
	}
	target, readErr := os.Readlink(filepath.Join(parentPath, "owned"))
	if readErr != nil || target != "replacement" {
		t.Fatalf("replacement changed: target=%q err=%v", target, readErr)
	}
}

func TestRemoveRetainedRootPreservesReplacementAtFinalIdentityCheck(t *testing.T) {
	parentPath := t.TempDir()
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := parent.Mkdir("owned", 0755); err != nil {
		t.Fatal(err)
	}
	root, err := OpenVerifiedSubroot(parent, "owned")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	beforeRetainedRemoveForTest = func(gotParent *os.Root, name string) {
		if gotParent != parent || name != "owned" {
			return
		}
		if err := os.Rename(filepath.Join(parentPath, name), filepath.Join(parentPath, name+"-retained")); err != nil {
			t.Fatal(err)
		}
		if err := parent.Mkdir(name, 0755); err != nil {
			t.Fatal(err)
		}
		if err := parent.WriteFile(name+"/replacement", []byte("replacement"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeRetainedRemoveForTest = nil })

	err = RemoveRetainedRoot(parent, "owned", root)
	if !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("RemoveRetainedRoot error = %v, want ownership failure", err)
	}
	content, readErr := parent.ReadFile("owned/replacement")
	if readErr != nil || string(content) != "replacement" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
}
