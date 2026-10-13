package testutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// RenameRetained attempts replacement while retained handles remain open.
// Windows can prevent the rename; verify that neither path changed in that case.
func RenameRetained(t *testing.T, oldPath, newPath string) bool {
	t.Helper()
	before, err := os.Lstat(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, newPath); err == nil {
		return true
	} else if runtime.GOOS != "windows" || (!errors.Is(err, fs.ErrPermission) && !errors.Is(err, syscall.Errno(32))) {
		t.Fatal(err)
	}
	after, err := os.Lstat(oldPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("blocked rename changed retained source: %v", err)
	}
	if _, err := os.Lstat(newPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("blocked rename created destination: %v", err)
	}
	return false
}

// ReplaceOwnedFile substitutes a racer for a recorded file so cleanup must
// report lost ownership and preserve the replacement on every platform.
func ReplaceOwnedFile(t *testing.T, path string) {
	t.Helper()
	replacement := filepath.Join(filepath.Dir(path), ".test-replacement")
	if err := os.WriteFile(replacement, []byte("replacement"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
}

// CanonicalTempDir returns the same path representation used by runtime path
// resolution, including expanding Windows 8.3 components in the host TEMP path.
func CanonicalTempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}
