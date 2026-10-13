package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetainedDirectoryHoldsAndVerifiesEveryComponent(t *testing.T) {
	projectPath := t.TempDir()
	original := filepath.Join(projectPath, "one", "two")
	if err := os.MkdirAll(original, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "value"), []byte("trusted"), 0644); err != nil {
		t.Fatal(err)
	}
	root, _, err := OpenVerifiedRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	directory, err := OpenRetainedDirectory(root, "one/two", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	replaced := renameRetainedForTest(t, filepath.Join(projectPath, "one"), filepath.Join(projectPath, "one-owned"))
	if !replaced {
		if err := directory.Verify(); err != nil {
			t.Fatalf("blocked ancestor replacement changed ownership: %v", err)
		}
		// A retained leaf remains renameable even when Windows protects its
		// ancestor because of the open descendant handle.
		if err := os.Rename(original, original+"-owned"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(original, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "value"), []byte("attacker"), 0644); err != nil {
		t.Fatal(err)
	}

	trusted, err := directory.Root().Open("value")
	if err != nil {
		t.Fatal(err)
	}
	content := make([]byte, len("trusted"))
	if _, err := trusted.Read(content); err != nil {
		t.Fatal(err)
	}
	if err := trusted.Close(); err != nil {
		t.Fatal(err)
	}
	if string(content) != "trusted" {
		t.Fatalf("retained content = %q", content)
	}
	if err := directory.Verify(); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("Verify error = %v, want ownership change", err)
	}
}

func TestRetainedDirectoryRejectsForbiddenComponentBeforeCreatingChild(t *testing.T) {
	projectPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectPath, ".doggo"), 0755); err != nil {
		t.Fatal(err)
	}
	root, _, err := OpenVerifiedRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	doggo, err := OpenRetainedDirectory(root, ".doggo", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer doggo.Close()
	if err := os.Rename(filepath.Join(projectPath, ".doggo"), filepath.Join(projectPath, "dist")); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenRetainedDirectory(root, "dist/release", 0755, doggo.Identity()); err == nil {
		t.Fatal("OpenRetainedDirectory accepted a forbidden ancestor")
	}
	if _, err := os.Lstat(filepath.Join(projectPath, "dist", "release")); !os.IsNotExist(err) {
		t.Fatalf("child created in forbidden directory: %v", err)
	}
}

func TestRetainedDirectoryCreationDoesNotAdoptTargetAppearingAtPublish(t *testing.T) {
	projectPath := t.TempDir()
	root, _, err := OpenVerifiedRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	original := generationRenameNoReplace
	generationRenameNoReplace = func(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
		if err := os.Mkdir(filepath.Join(projectPath, newName), 0755); err != nil {
			return false, err
		}
		if err := os.WriteFile(filepath.Join(projectPath, newName, "marker"), []byte("racer"), 0644); err != nil {
			return false, err
		}
		return original(parent, retained, oldName, newName)
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	if _, err := OpenRetainedDirectory(root, "created", 0755); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("OpenRetainedDirectory error = %v, want fs.ErrExist", err)
	}
	if content, err := os.ReadFile(filepath.Join(projectPath, "created", "marker")); err != nil || string(content) != "racer" {
		t.Fatalf("racing target changed: content=%q err=%v", content, err)
	}
	entries, err := os.ReadDir(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "created" {
		t.Fatalf("private creation residue = %v", entries)
	}
}

func TestRetainedDirectoryCreationRejectsPrivateSiblingReplacementBeforeOpen(t *testing.T) {
	projectPath := t.TempDir()
	root, _, err := OpenVerifiedRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var privateName string
	afterRetainedDirectoryPrivateIdentityForTest = func(name string) {
		privateName = name
		if err := os.Rename(filepath.Join(projectPath, name), filepath.Join(projectPath, name+"-retained")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(projectPath, name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projectPath, name, "marker"), []byte("replacement"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterRetainedDirectoryPrivateIdentityForTest = nil })

	if _, err := OpenRetainedDirectory(root, "created", 0755); err == nil || !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("OpenRetainedDirectory error = %v, want ownership change", err)
	}
	if content, err := os.ReadFile(filepath.Join(projectPath, privateName, "marker")); err != nil || string(content) != "replacement" {
		t.Fatalf("private replacement changed: content=%q err=%v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(projectPath, "created")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("requested target was created: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(projectPath, privateName+"-retained")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("exact private sibling was not cleaned: %v", err)
	}
}

func TestRetainedDirectoryCreationRejectsPrivateSiblingReplacement(t *testing.T) {
	projectPath := t.TempDir()
	root, _, err := OpenVerifiedRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	original := generationRenameNoReplace
	var retainedOriginal string
	var replacement string
	generationRenameNoReplace = func(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
		retainedOriginal = filepath.Join(projectPath, oldName+"-retained")
		if err := os.Rename(filepath.Join(projectPath, oldName), retainedOriginal); err != nil {
			return false, err
		}
		if err := os.Mkdir(filepath.Join(projectPath, oldName), 0755); err != nil {
			return false, err
		}
		if err := os.WriteFile(filepath.Join(projectPath, oldName, "marker"), []byte("replacement"), 0644); err != nil {
			return false, err
		}
		renamed, err := original(parent, retained, oldName, newName)
		replacement = filepath.Join(projectPath, oldName)
		if renamed {
			replacement = filepath.Join(projectPath, newName)
		}
		return renamed, err
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	if _, err := OpenRetainedDirectory(root, "created", 0755); err == nil || !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("OpenRetainedDirectory error = %v, want ownership change", err)
	}
	if content, err := os.ReadFile(filepath.Join(replacement, "marker")); err != nil || string(content) != "replacement" {
		t.Fatalf("replacement target changed: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(retainedOriginal); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("exact private sibling was not cleaned: %v", err)
	}
}

func TestRetainedDirectoryCreationPreservesReplacementAfterRenameError(t *testing.T) {
	projectPath := t.TempDir()
	root, _, err := OpenVerifiedRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	want := errors.New("post-rename failure")
	original := generationRenameNoReplace
	generationRenameNoReplace = func(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
		renamed, err := original(parent, retained, oldName, newName)
		if err != nil || !renamed {
			return renamed, err
		}
		if err := os.Rename(filepath.Join(projectPath, newName), filepath.Join(projectPath, newName+"-retained")); err != nil {
			return true, err
		}
		if err := os.Mkdir(filepath.Join(projectPath, newName), 0755); err != nil {
			return true, err
		}
		if err := os.WriteFile(filepath.Join(projectPath, newName, "marker"), []byte("replacement"), 0644); err != nil {
			return true, err
		}
		return true, want
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	if _, err := OpenRetainedDirectory(root, "created", 0755); !errors.Is(err, want) {
		t.Fatalf("OpenRetainedDirectory error = %v, want post-rename failure", err)
	}
	if content, err := os.ReadFile(filepath.Join(projectPath, "created", "marker")); err != nil || string(content) != "replacement" {
		t.Fatalf("replacement target changed: content=%q err=%v", content, err)
	}
}

func TestRetainedDirectoryCreationCleansCommittedComponentAfterSyncFailure(t *testing.T) {
	projectPath := t.TempDir()
	root, _, err := OpenVerifiedRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	want := errors.New("parent sync failed")
	original := atomicParentSync
	atomicParentSync = func(*os.File) error { return want }
	t.Cleanup(func() { atomicParentSync = original })

	directory, err := OpenRetainedDirectory(root, "created", 0755)
	var atomicErr *AtomicWriteError
	if directory != nil || !errors.Is(err, want) || !errors.As(err, &atomicErr) || !atomicErr.Renamed {
		t.Fatalf("OpenRetainedDirectory = (%v, %v), want committed sync failure", directory, err)
	}
	entries, err := os.ReadDir(projectPath)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed creation left owned directories: %v, %v", entries, err)
	}
}

func TestRetainedDirectoryCloseIsIdempotentAndRejectsUseAfterClose(t *testing.T) {
	projectPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectPath, "one", "two"), 0755); err != nil {
		t.Fatal(err)
	}
	root, _, err := OpenVerifiedRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	directory, err := OpenRetainedDirectory(root, "one/two", 0)
	if err != nil {
		t.Fatal(err)
	}
	retainedRoots := []*os.Root{directory.components[0].root, directory.components[1].root}
	if err := directory.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := directory.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
	for i, retained := range retainedRoots {
		if _, err := retained.Stat("."); err == nil {
			t.Fatalf("retained component %d remains open", i)
		}
	}
	if directory.Root() != nil || directory.Identity() != nil || directory.LeafCreated() {
		t.Fatal("closed RetainedDirectory still exposes retained state")
	}
	if err := directory.Verify(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Verify after Close = %v", err)
	}
}
