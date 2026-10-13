package project

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomicReplacesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "generated.go")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(path, []byte("new"), 0640); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("content = %q, want new", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	wantMode := os.FileMode(0640)
	if runtime.GOOS == "windows" {
		wantMode = 0666 // Windows chmod only represents the owner-write bit.
	}
	if got := info.Mode().Perm(); got != wantMode {
		t.Fatalf("mode = %o, want %o", got, wantMode)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".generated.go.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func TestWriteFileAtomicClassifiesPreRenameError(t *testing.T) {
	err := WriteFileAtomic(filepath.Join(t.TempDir(), "missing", "file"), []byte("new"), 0644)
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) {
		t.Fatalf("error = %T %v, want *AtomicWriteError", err, err)
	}
	if atomicErr.Renamed {
		t.Fatalf("Renamed = true, want false: %v", err)
	}
}

func TestWriteFileAtomicRejectsTrailingSeparatorWithoutCreatingFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}

	err := WriteFileAtomic(dir+string(os.PathSeparator), []byte("content"), 0644)
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) || atomicErr.Renamed {
		t.Fatalf("WriteFileAtomic error = %T %v, want pre-rename AtomicWriteError", err, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("trailing-separator target created entries: %v", entries)
	}
}

func TestJoinAtomicClosePreservesRenameCommit(t *testing.T) {
	closeErr := errors.New("close verified parent")
	err := joinAtomicClose(nil, closeErr, true)
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) || !atomicErr.Renamed || !errors.Is(err, closeErr) {
		t.Fatalf("joinAtomicClose error = %T %v, want renamed close failure", err, err)
	}
}

func TestWriteFileAtomicPreservesUnpublishedTempReplacement(t *testing.T) {
	dir := t.TempDir()
	want := errors.New("write failed")
	originalWrite := atomicTempWrite
	atomicTempWrite = func(*os.File, []byte) (int, error) { return 0, want }
	var replacement string
	beforeRetainedRemoveForTest = func(parent *os.Root, name string) {
		if replacement != "" || !strings.HasPrefix(name, ".generated.go.tmp-") {
			return
		}
		if err := os.Rename(filepath.Join(dir, name), filepath.Join(dir, name+"-retained")); err != nil {
			t.Fatal(err)
		}
		if err := parent.WriteFile(name, []byte("replacement"), 0644); err != nil {
			t.Fatal(err)
		}
		replacement = filepath.Join(dir, name)
	}
	t.Cleanup(func() {
		atomicTempWrite = originalWrite
		beforeRetainedRemoveForTest = nil
	})

	err := WriteFileAtomic(filepath.Join(dir, "generated.go"), []byte("owned"), 0644)
	if !errors.Is(err, want) || !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("WriteFileAtomic error = %v, want write and ownership failures", err)
	}
	content, readErr := os.ReadFile(replacement)
	if readErr != nil || string(content) != "replacement" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
}

func TestWriteFileAtomicValidatesContainedPaths(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "nested")
	if err := os.Mkdir(parent, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateContainedPath(root, filepath.Join(parent, "generated.go")); err != nil {
		t.Fatalf("valid contained path: %v", err)
	}

	if err := ValidateContainedPath(root, filepath.Join(root, "..", "escape.go")); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("traversal error = %v, want outside-root rejection", err)
	}

	linked := filepath.Join(root, "linked")
	if err := os.Symlink(parent, linked); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := ValidateContainedPath(root, filepath.Join(linked, "generated.go")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink-parent error = %v, want symlink rejection", err)
	}
}

func TestWriteFileAtomicRootStaysConfinedWhenRootPathIsReplaced(t *testing.T) {
	base := t.TempDir()
	rootPath := filepath.Join(base, "root")
	movedRoot := filepath.Join(base, "moved-root")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{rootPath, outside} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	parent, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	root, err := openVerifiedSubroot(parent, "root")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(rootPath, movedRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, rootPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := WriteFileAtomicRoot(root, "generated.go", []byte("safe"), 0644); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(movedRoot, "generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "safe" {
		t.Fatalf("content = %q, want safe", content)
	}
	if _, err := os.Stat(filepath.Join(outside, "generated.go")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists or stat failed unexpectedly: %v", err)
	}
}

func TestWriteFileAtomicRootRejectsTraversalAndSymlinkParents(t *testing.T) {
	base := t.TempDir()
	rootPath := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{rootPath, outside} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := WriteFileAtomicRoot(root, "../escape.go", []byte("bad"), 0644); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("traversal error = %v, want outside-root rejection", err)
	}
	if err := os.Symlink(outside, filepath.Join(rootPath, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := WriteFileAtomicRoot(root, filepath.Join("linked", "escape.go"), []byte("bad"), 0644); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink-parent error = %v, want symlink rejection", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.go")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists or stat failed unexpectedly: %v", err)
	}
}
