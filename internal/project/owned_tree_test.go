package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedTreePublishRootPreservesOwnership(t *testing.T) {
	for _, outcome := range []string{"success", "collision", "sync failure", "unrecorded entry"} {
		t.Run(outcome, func(t *testing.T) {
			parent, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer parent.Close()
			if err := parent.Mkdir("stage", 0755); err != nil {
				t.Fatal(err)
			}
			root, err := OpenVerifiedSubroot(parent, "stage")
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			tree := NewOwnedTree(root)
			defer tree.Close()
			if err := tree.CreateAll("a/b", 0755); err != nil {
				t.Fatal(err)
			}
			if err := tree.WriteFile("a/b/value", []byte("owned"), 0644); err != nil {
				t.Fatal(err)
			}
			var want error
			renamed := outcome == "success" || outcome == "sync failure"
			switch outcome {
			case "collision":
				want = fs.ErrExist
				if err := parent.Mkdir("published", 0755); err != nil {
					t.Fatal(err)
				}
			case "sync failure":
				want = errors.New("parent sync failed")
				original := atomicParentSync
				atomicParentSync = func(*os.File) error { return want }
				t.Cleanup(func() { atomicParentSync = original })
			case "unrecorded entry":
				want = ErrOwnershipChanged
				if err := root.WriteFile("unrecorded", []byte("racer"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			err = tree.PublishRoot(parent, "stage", "published")
			if !errors.Is(err, want) {
				t.Fatalf("PublishRoot = %v, want %v", err, want)
			}
			var atomicErr *AtomicWriteError
			if err != nil && (!errors.As(err, &atomicErr) || atomicErr.Renamed != renamed) {
				t.Fatalf("PublishRoot = %v, want Renamed=%v", err, renamed)
			}
			name := "stage"
			if renamed {
				name = "published"
			}
			if err := VerifySubroot(parent, name, root); err != nil {
				t.Fatal(err)
			}
			if content, err := tree.ReadFile("a/b/value"); err != nil || string(content) != "owned" {
				t.Fatalf("retained content = %q, %v", content, err)
			}
			if err := tree.VerifyComplete(); (err != nil) != (outcome == "unrecorded entry") {
				t.Fatalf("retained ledger verification = %v", err)
			}
		})
	}
}

func TestOwnedTreeRenameRootTransfersLedgerAtRenameCommitPoint(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := NewOwnedTree(root)
	defer tree.Close()
	stage, err := tree.CreateRoot("stage", 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}

	want := errors.New("publisher failed after rename")
	original := generationRenameNoReplace
	generationRenameNoReplace = func(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
		renamed, err := original(parent, retained, oldName, newName)
		if err != nil {
			return renamed, err
		}
		return true, want
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	err = tree.RenameRoot("stage", "published")
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) || !atomicErr.Renamed || !errors.Is(err, want) {
		t.Fatalf("RenameRoot error = %T %v, want renamed publisher failure", err, err)
	}
	if err := tree.VerifyComplete(); err != nil {
		t.Fatalf("renamed tree ownership was not transferred: %v", err)
	}
	published, err := tree.OpenRoot("published")
	if err != nil {
		t.Fatalf("published root is not owned: %v", err)
	}
	if err := published.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.OpenRoot("stage"); err == nil {
		t.Fatal("old stage root remains recorded after rename")
	}
	if err := tree.Cleanup(OwnedTreeCleanupHooks{}); err != nil {
		t.Fatalf("cleanup of renamed root: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(rootPath, "published")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cleanup left published root: %v", err)
	}
}

func TestOwnedTreeAbsentWriteRecordsCommittedIdentityBeforeVerification(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := NewOwnedTree(root)
	defer tree.Close()

	value := filepath.Join(rootPath, "value")
	published := filepath.Join(rootPath, "value-published")
	originalSync := atomicParentSync
	atomicParentSync = func(parent *os.File) error {
		if err := originalSync(parent); err != nil {
			return err
		}
		if err := os.Rename(value, published); err != nil {
			return err
		}
		return os.WriteFile(value, []byte("racer"), 0644)
	}
	t.Cleanup(func() { atomicParentSync = originalSync })

	err = tree.WriteOwnedFile("value", []byte("owned"), 0644)
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) || !atomicErr.Renamed {
		t.Fatalf("WriteOwnedFile error = %T %v, want renamed identity uncertainty", err, err)
	}
	content, readErr := os.ReadFile(value)
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
	if err := os.Remove(value); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(published, value); err != nil {
		t.Fatal(err)
	}
	if err := tree.VerifyComplete(); err != nil {
		t.Fatalf("restored committed identity was not recorded: %v", err)
	}
	if err := tree.Cleanup(OwnedTreeCleanupHooks{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(value); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cleanup left committed file: %v", err)
	}
}

func TestOwnedTreeWriteOwnedFileExistingNeverReplacesRacingFile(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := NewOwnedTree(root)
	defer tree.Close()
	if err := tree.WriteFile("value", []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}

	originalSync := atomicTempSync
	atomicTempSync = func(file *os.File) error {
		if err := originalSync(file); err != nil {
			return err
		}
		path := filepath.Join(rootPath, "value")
		if err := os.Rename(path, path+"-owned"); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("racer"), 0644)
	}
	t.Cleanup(func() { atomicTempSync = originalSync })

	if err := tree.WriteOwnedFile("value", []byte("new"), 0644); err == nil {
		t.Fatal("WriteOwnedFile replaced a racing file")
	}
	content, err := os.ReadFile(filepath.Join(rootPath, "value"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "racer" {
		t.Fatalf("racing file = %q, want racer", content)
	}
}

func TestOwnedTreeWriteOwnedFileAbsentNeverReplacesRacingFile(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := NewOwnedTree(root)
	defer tree.Close()

	originalSync := atomicTempSync
	atomicTempSync = func(file *os.File) error {
		if err := originalSync(file); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(rootPath, "value"), []byte("racer"), 0644)
	}
	t.Cleanup(func() { atomicTempSync = originalSync })

	if err := tree.WriteOwnedFile("value", []byte("new"), 0644); err == nil {
		t.Fatal("WriteOwnedFile replaced a racing file")
	}
	content, err := os.ReadFile(filepath.Join(rootPath, "value"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "racer" {
		t.Fatalf("racing file = %q, want racer", content)
	}
}

func TestOwnedTreeWriteFileRetainsPartialWriteForCleanup(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := NewOwnedTree(root)
	defer tree.Close()
	stage, err := tree.CreateRoot("stage", 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}

	want := errors.New("write failed")
	originalWrite := atomicTempWrite
	atomicTempWrite = func(file *os.File, content []byte) (int, error) {
		written, err := file.Write(content[:1])
		if err != nil {
			return written, err
		}
		return written, want
	}
	t.Cleanup(func() { atomicTempWrite = originalWrite })

	if err := tree.WriteFile("stage/partial", []byte("partial"), 0644); !errors.Is(err, want) {
		t.Fatalf("WriteFile error = %v, want write failure", err)
	}
	if err := tree.RemoveSubtree("stage"); err != nil {
		t.Fatalf("RemoveSubtree after partial write: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(rootPath, "stage")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("partial stage remains after cleanup: %v", err)
	}
}

func TestOwnedTreeWriteFileRejectsReplacementDuringWrite(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := NewOwnedTree(root)
	defer tree.Close()
	stage, err := tree.CreateRoot("stage", 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(rootPath, "stage", "value")
	originalWrite := atomicTempWrite
	atomicTempWrite = func(file *os.File, content []byte) (int, error) {
		if err := os.Rename(path, path+"-retained"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("replacement"), 0644); err != nil {
			t.Fatal(err)
		}
		return file.Write(content)
	}
	t.Cleanup(func() { atomicTempWrite = originalWrite })

	if err := tree.WriteFile("stage/value", []byte("owned"), 0644); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("WriteFile error = %v, want ownership failure", err)
	}
	if err := tree.VerifyComplete(); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("VerifyComplete error = %v, want replaced stage rejection", err)
	}
	if err := tree.RemoveSubtree("stage"); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("RemoveSubtree error = %v, want fail-closed ownership failure", err)
	}
	if content, err := os.ReadFile(path + "-retained"); err != nil || string(content) != "owned" {
		t.Fatalf("retained handle content = %q err=%v, want owned", content, err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "replacement" {
		t.Fatalf("replacement changed: content=%q err=%v", content, err)
	}
}

func TestOwnedTreeReadFileReadsRetainedHandleAcrossABAReplacement(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := NewOwnedTree(root)
	defer tree.Close()
	if err := tree.WriteFile("value", []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}

	originalHook := afterOwnedTreeSourceOpenForTest
	called := false
	afterOwnedTreeSourceOpenForTest = func(path string) {
		if path != "value" {
			return
		}
		called = true
		value := filepath.Join(rootPath, "value")
		retained := value + "-retained"
		if err := os.Rename(value, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(value, []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(value); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(retained, value); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterOwnedTreeSourceOpenForTest = originalHook })

	content, err := tree.ReadFile("value")
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("ReadFile did not reach the deterministic ABA interleaving")
	}
	if string(content) != "owned" {
		t.Fatalf("ReadFile content = %q, want owned", content)
	}
}
