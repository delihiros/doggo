package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewOwnedTempRootRejectsReplacementBetweenCreateAndOpen(t *testing.T) {
	var replacement, moved string
	beforeOwnedTempRootOpenForTest = func(parent, name string) {
		replacement = filepath.Join(parent, name)
		moved = replacement + "-owned"
		if err := os.Rename(replacement, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(replacement, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(replacement, "replacement"), []byte("racer"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		beforeOwnedTempRootOpenForTest = nil
		_ = os.RemoveAll(replacement)
		_ = os.RemoveAll(moved)
	})

	root, err := NewOwnedTempRoot(".doggo-owned-test-")
	if root != nil {
		_ = root.Close()
	}
	if err == nil {
		t.Fatal("NewOwnedTempRoot accepted a replacement directory")
	}
	content, readErr := os.ReadFile(filepath.Join(replacement, "replacement"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
	if _, statErr := os.Lstat(moved); !os.IsNotExist(statErr) {
		t.Fatalf("created temporary root was not cleaned: %v", statErr)
	}
}

func TestOwnedTempRootCleanupPreservesNestedReplacement(t *testing.T) {
	for _, kind := range []string{"directory", "file"} {
		t.Run(kind, func(t *testing.T) {
			root, err := NewOwnedTempRoot(".doggo-owned-nested-")
			if err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				child, err := root.Tree().CreateRoot("entry", 0755, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := child.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err := root.Tree().WriteFile("entry", []byte("owned"), 0644); err != nil {
				t.Fatal(err)
			}
			entry := filepath.Join(root.Path(), "entry")
			if err := os.Rename(entry, entry+"-owned"); err != nil {
				t.Fatal(err)
			}
			marker := entry
			if kind == "directory" {
				if err := os.Mkdir(entry, 0755); err != nil {
					t.Fatal(err)
				}
				marker = filepath.Join(entry, "replacement")
			}
			if err := os.WriteFile(marker, []byte("racer"), 0644); err != nil {
				t.Fatal(err)
			}

			if err := root.Close(); !errors.Is(err, ErrOwnershipChanged) {
				t.Fatalf("Close error = %v, want ownership failure", err)
			}
			if content, err := os.ReadFile(marker); err != nil || string(content) != "racer" {
				t.Fatalf("replacement changed: content=%q err=%v", content, err)
			}
			_ = os.RemoveAll(root.Path())
			_ = os.RemoveAll(root.Path() + "-owned")
		})
	}
}

func TestOwnedTempRootClosePreservesFinalBoundaryReplacement(t *testing.T) {
	root, err := NewOwnedTempRoot(".doggo-owned-final-")
	if err != nil {
		t.Fatal(err)
	}
	parent, name := root.parent, root.name
	path := root.Path()
	beforeRetainedRemoveForTest = func(gotParent *os.Root, gotName string) {
		if gotParent != parent || gotName != name {
			return
		}
		if err := os.Rename(path, path+"-retained"); err != nil {
			t.Fatal(err)
		}
		if err := gotParent.Mkdir(gotName, 0700); err != nil {
			t.Fatal(err)
		}
		if err := gotParent.WriteFile(gotName+"/replacement", []byte("racer"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		beforeRetainedRemoveForTest = nil
		_ = os.RemoveAll(path)
		_ = os.RemoveAll(path + "-retained")
	})

	if err := root.Close(); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("Close error = %v, want ownership failure", err)
	}
	content, readErr := os.ReadFile(filepath.Join(path, "replacement"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
}

func TestOwnedTempRootCapturesSubprocessOutput(t *testing.T) {
	root, err := NewOwnedTempRoot(".doggo-owned-subprocess-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root.Path(), "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Path(), "nested", "value"), []byte("value"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root.Path(), "link")); err != nil {
		_ = root.Close()
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := root.CaptureSubprocessOutput(); err != nil {
		t.Fatal(err)
	}
	if err := root.Tree().VerifyComplete(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Tree().ReadFile("link"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ReadFile error = %v, want symlink rejection", err)
	}
	if err := root.Tree().WriteOwnedFile("link", []byte("replacement"), 0600); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("WriteOwnedFile error = %v, want symlink rejection", err)
	}
	if err := root.Tree().RefreshFile("link"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("RefreshFile error = %v, want symlink rejection", err)
	}
	destination, err := NewOwnedTempRoot(".doggo-owned-copy-")
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Tree().CopyTo(destination.Tree(), nil); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("CopyTo error = %v, want symlink rejection", err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
	path := root.Path()
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("subprocess root remains: %v", err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "target" {
		t.Fatalf("symlink target changed: content=%q err=%v", content, err)
	}
}

func TestOwnedTempRootCapturedSymlinkReplacementSurvivesCleanup(t *testing.T) {
	root, err := NewOwnedTempRoot(".doggo-owned-symlink-")
	if err != nil {
		t.Fatal(err)
	}
	path := root.Path()
	if err := os.Symlink("original", filepath.Join(path, "link")); err != nil {
		_ = root.Close()
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := root.CaptureSubprocessOutput(); err != nil {
		t.Fatal(err)
	}
	beforeRetainedRemoveForTest = func(parent *os.Root, name string) {
		if parent != root.root || name != "link" {
			return
		}
		if err := os.Rename(filepath.Join(path, name), filepath.Join(path, name+"-retained")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("replacement", filepath.Join(path, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		beforeRetainedRemoveForTest = nil
		_ = os.RemoveAll(path)
	})

	if err := root.Close(); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("Close error = %v, want ownership failure", err)
	}
	if target, err := os.Readlink(filepath.Join(path, "link")); err != nil || target != "replacement" {
		t.Fatalf("replacement symlink changed: target=%q err=%v", target, err)
	}
}
