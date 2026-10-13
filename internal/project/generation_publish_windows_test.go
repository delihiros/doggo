//go:build windows

package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestGenerationRenameWindowsInformationDoesNotRequestReplacement(t *testing.T) {
	buffer, err := generationRenameInformation(1, "destination")
	if err != nil {
		t.Fatal(err)
	}
	info := (*legacyFileRenameInformation)(unsafe.Pointer(&buffer[0]))
	if info.ReplaceIfExists || buffer[0] != 0 {
		t.Fatalf("ReplaceIfExists encoding = %v/%#x, want false/0", info.ReplaceIfExists, buffer[0])
	}
	if info.RootDirectory != windows.Handle(1) {
		t.Fatalf("RootDirectory = %v, want 1", info.RootDirectory)
	}
}

func TestGenerationRenameWindowsNTStatusMapsCollisionToExist(t *testing.T) {
	if err := generationRenameNTStatus(windows.STATUS_OBJECT_NAME_COLLISION); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("collision error = %v, want fs.ErrExist", err)
	}
}

func TestGenerationRenameWindowsWithRetainedDestinationDirectory(t *testing.T) {
	parent, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := parent.Mkdir("stage", 0755); err != nil {
		t.Fatal(err)
	}
	stage, err := openVerifiedSubroot(parent, "stage")
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	parentFile, err := parent.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer parentFile.Close()

	renamed, err := renameGenerationNoReplace(parentFile, stage, "stage", "published")
	if err != nil || !renamed {
		t.Fatalf("renameGenerationNoReplace = (%v, %v), want (true, nil)", renamed, err)
	}
	if err := revalidateSubroot(parent, "published", stage); err != nil {
		t.Fatalf("retained root does not match published: %v", err)
	}
}

func TestOwnedTreeGenerationRenameWindowsReopensRetainedDescendants(t *testing.T) {
	rootPath, tree := newGenerationRenameWindowsTree(t)

	if err := tree.RenameRoot("stage", "published"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(rootPath, "published", "a", "b", "value"))
	if err != nil || string(content) != "owned" {
		t.Fatalf("published value = %q, %v", content, err)
	}
	if err := tree.VerifyComplete(); err != nil {
		t.Fatal(err)
	}
	if err := tree.Cleanup(OwnedTreeCleanupHooks{}); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedTreeGenerationRenameWindowsRestoresDescendantsAfterPublisherCollision(t *testing.T) {
	_, tree := newGenerationRenameWindowsTree(t)
	called := false
	original := generationRenameNoReplace
	generationRenameNoReplace = func(*os.File, *os.Root, string, string) (bool, error) {
		called = true
		return false, fs.ErrExist
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	err := tree.RenameRoot("stage", "published")
	var atomicErr *AtomicWriteError
	if !called || !errors.Is(err, fs.ErrExist) || !errors.As(err, &atomicErr) || atomicErr.Renamed {
		t.Fatalf("RenameRoot error = %T %v, want non-renamed collision", err, err)
	}
	if err := tree.VerifyComplete(); err != nil {
		t.Fatalf("restored tree verification: %v", err)
	}
	content, err := tree.ReadFile("stage/a/b/value")
	if err != nil || string(content) != "owned" {
		t.Fatalf("restored value = %q, %v", content, err)
	}
	if err := tree.Cleanup(OwnedTreeCleanupHooks{}); err != nil {
		t.Fatalf("restored tree cleanup: %v", err)
	}
}

func TestOwnedTreeGenerationRenameWindowsRejectsCommittedChildReplacement(t *testing.T) {
	rootPath, tree := newGenerationRenameWindowsTree(t)
	replacement := filepath.Join(rootPath, "stage", "a")
	retained := filepath.Join(rootPath, "retained-a")
	original := generationRenameNoReplace
	generationRenameNoReplace = func(parent *os.File, root *os.Root, oldName, newName string) (bool, error) {
		if err := os.Rename(replacement, retained); err != nil {
			return false, err
		}
		if err := os.Mkdir(replacement, 0755); err != nil {
			return false, err
		}
		if err := os.WriteFile(filepath.Join(replacement, "value"), []byte("racer"), 0644); err != nil {
			return false, err
		}
		return original(parent, root, oldName, newName)
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	err := tree.RenameRoot("stage", "published")
	var atomicErr *AtomicWriteError
	if !errors.Is(err, ErrOwnershipChanged) || !errors.As(err, &atomicErr) || !atomicErr.Renamed {
		t.Fatalf("RenameRoot error = %T %v, want renamed ownership failure", err, err)
	}
	if err := tree.Cleanup(OwnedTreeCleanupHooks{}); err == nil {
		t.Fatal("Cleanup accepted a closed replacement entry")
	}
	content, readErr := os.ReadFile(filepath.Join(rootPath, "published", "a", "value"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement value = %q, %v", content, readErr)
	}
}

func TestOwnedTreeGenerationRenameWindowsRejectsIncompleteTreeBeforePublisher(t *testing.T) {
	rootPath, tree := newGenerationRenameWindowsTree(t)
	unrecorded := filepath.Join(rootPath, "stage", "a", "b", "unrecorded")
	if err := os.WriteFile(unrecorded, []byte("racer"), 0644); err != nil {
		t.Fatal(err)
	}
	called := false
	original := generationRenameNoReplace
	generationRenameNoReplace = func(*os.File, *os.Root, string, string) (bool, error) {
		called = true
		return false, nil
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	if err := tree.RenameRoot("stage", "published"); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("RenameRoot error = %v, want incomplete-tree ownership failure", err)
	}
	if called {
		t.Fatal("publisher called for incomplete tree")
	}
	if err := os.Remove(unrecorded); err != nil {
		t.Fatal(err)
	}
	if err := tree.Cleanup(OwnedTreeCleanupHooks{}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationOwnerGenerationRenameWindowsPublishesRetainedDescendants(t *testing.T) {
	projectPath := t.TempDir()
	owner, err := BeginGenerationOwner(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	tree, err := owner.StageTree()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"a", "a/b"} {
		created, err := tree.CreateRoot(path, 0755, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := created.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := tree.WriteFile("a/b/value", []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}

	generation := strings.Repeat("a", 64)
	if renamed, err := owner.PublishStage(generation); err != nil || !renamed {
		t.Fatalf("PublishStage = (%v, %v), want (true, nil)", renamed, err)
	}
	content, err := os.ReadFile(filepath.Join(projectPath, ".doggo", "generations", generation, "a", "b", "value"))
	if err != nil || string(content) != "owned" {
		t.Fatalf("published value = %q, %v", content, err)
	}
	if err := owner.VerifyStage(); err != nil {
		t.Fatal(err)
	}
	if err := owner.RemoveStage(); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationRenameWindowsNoReplacePreservesExistingDestination(t *testing.T) {
	parentPath := t.TempDir()
	parentRoot, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parentRoot.Close()
	stage := filepath.Join(parentPath, "stage")
	destination := filepath.Join(parentPath, "destination")
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := parentRoot.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	// Match production: relative handles allow rename while retaining identity.
	stageRoot, err := openVerifiedSubroot(parentRoot, "stage")
	if err != nil {
		t.Fatal(err)
	}
	defer stageRoot.Close()

	renamed, err := renameGenerationNoReplace(parent, stageRoot, "stage", "destination")
	if !errors.Is(err, fs.ErrExist) || renamed {
		t.Fatalf("renameGenerationNoReplace error = %v, want fs.ErrExist", err)
	}
	after, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("existing destination directory was replaced")
	}
	if info, err := os.Stat(stage); err != nil || !info.IsDir() {
		t.Fatalf("stage missing after failed rename: %v", err)
	}
}

func TestGenerationRenameWindowsNoReplaceRejectsReplacedSource(t *testing.T) {
	parentPath := t.TempDir()
	parentRoot, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parentRoot.Close()
	stage := filepath.Join(parentPath, "stage")
	moved := filepath.Join(parentPath, "stage-retained")
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "original"), []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}
	stageRoot, err := openVerifiedSubroot(parentRoot, "stage")
	if err != nil {
		t.Fatal(err)
	}
	defer stageRoot.Close()
	if err := os.Rename(stage, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "replacement"), []byte("racer"), 0644); err != nil {
		t.Fatal(err)
	}
	parent, err := parentRoot.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()

	renamed, err := renameGenerationNoReplace(parent, stageRoot, "stage", "destination")
	if !errors.Is(err, ErrOwnershipChanged) || renamed {
		t.Fatalf("renameGenerationNoReplace = (%v, %v), want pre-rename ownership error", renamed, err)
	}
	if content, readErr := os.ReadFile(filepath.Join(stage, "replacement")); readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement source = %q, %v", content, readErr)
	}
	if content, readErr := os.ReadFile(filepath.Join(moved, "original")); readErr != nil || string(content) != "owned" {
		t.Fatalf("retained source = %q, %v", content, readErr)
	}
}

func TestGenerationRenameWindowsNoReplaceReportsCloseFailureAfterRename(t *testing.T) {
	parentPath := t.TempDir()
	parentRoot, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parentRoot.Close()
	stage := filepath.Join(parentPath, "stage")
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	stageRoot, err := openVerifiedSubroot(parentRoot, "stage")
	if err != nil {
		t.Fatal(err)
	}
	defer stageRoot.Close()
	parent, err := parentRoot.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	want := errors.New("close generation publication source")
	originalClose := closeGenerationPublishSource
	closeGenerationPublishSource = func(file *os.File) error {
		return errors.Join(file.Close(), want)
	}
	t.Cleanup(func() { closeGenerationPublishSource = originalClose })

	renamed, err := renameGenerationNoReplace(parent, stageRoot, "stage", "destination")
	if !renamed || !errors.Is(err, want) {
		t.Fatalf("renameGenerationNoReplace = (%v, %v), want renamed close failure", renamed, err)
	}
	if _, statErr := os.Stat(filepath.Join(parentPath, "destination")); statErr != nil {
		t.Fatalf("destination missing after rename: %v", statErr)
	}
}

func TestAtomicWindowsDirectorySyncHasWriteAccess(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	parent, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	syncHandle, err := openDirectoryForSync(parent)
	if err != nil {
		t.Fatalf("openDirectoryForSync: %v", err)
	}
	defer syncHandle.Close()
	if err := windows.FlushFileBuffers(windows.Handle(syncHandle.Fd())); err != nil {
		t.Fatalf("windows.FlushFileBuffers: %v", err)
	}
}

func TestAtomicWindowsSyncRootTreeFileHasWriteAccess(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := NewOwnedTree(root)
	defer tree.Close()
	if err := tree.WriteFile("value", []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := syncRootTree(root); err != nil {
		t.Fatalf("syncRootTree: %v", err)
	}
	if err := tree.Cleanup(OwnedTreeCleanupHooks{}); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicWindowsReopenedFileSyncLeavesOriginalHandleOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "value")
	if err := os.WriteFile(path, []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if err := syncFile(file); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Stat(); err != nil {
		t.Fatalf("original handle was closed: %v", err)
	}
}

func newGenerationRenameWindowsTree(t *testing.T) (string, *OwnedTree) {
	t.Helper()
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	tree := NewOwnedTree(root)
	t.Cleanup(func() { _ = tree.Close() })
	for _, path := range []string{"stage", "stage/a", "stage/a/b"} {
		created, err := tree.CreateRoot(path, 0755, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := created.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := tree.WriteFile("stage/a/b/value", []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}
	return rootPath, tree
}
