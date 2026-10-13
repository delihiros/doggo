//go:build windows

package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidRetainedNameWindowsRejectsSeparatorsAndVolumes(t *testing.T) {
	for _, name := range []string{`\`, `C:\`, `dir\entry`, filepath.VolumeName(t.TempDir()) + `\`} {
		if validRetainedName(name) {
			t.Errorf("validRetainedName(%q) = true, want false", name)
		}
	}
}

func TestRemoveRetainedFileWindowsDispositionPreservesReplacement(t *testing.T) {
	parent, err := os.OpenRoot(t.TempDir())
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
		if err := parent.Rename(name, name+"-retained"); err != nil {
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

func TestRemoveRetainedFileWindowsRemovesSymlinkLeafOnly(t *testing.T) {
	parent, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := parent.WriteFile("target", []byte("target"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := parent.Symlink("target", "owned"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	expected, err := parent.Lstat("owned")
	if err != nil {
		t.Fatal(err)
	}

	if err := removeRetainedFile(parent, "owned", expected); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Lstat("owned"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink leaf remains: %v", err)
	}
	content, err := parent.ReadFile("target")
	if err != nil || string(content) != "target" {
		t.Fatalf("symlink target changed: content=%q err=%v", content, err)
	}
}

func TestRemoveRetainedFileWindowsRemovesOwnedReadOnlyFile(t *testing.T) {
	parent, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := parent.WriteFile("owned", []byte("owned"), 0444); err != nil {
		t.Fatal(err)
	}
	expected, err := parent.Lstat("owned")
	if err != nil {
		t.Fatal(err)
	}
	if expected.Mode().Perm()&0200 != 0 {
		t.Fatal("fixture is not read-only")
	}
	if err := removeRetainedFile(parent, "owned", expected); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Lstat("owned"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only owned file remains: %v", err)
	}
}
