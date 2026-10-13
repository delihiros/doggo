//go:build linux || darwin

package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOwnedTempRootSubprocessSpecialEntryFailsClosed(t *testing.T) {
	root, err := NewOwnedTempRoot(".doggo-owned-special-")
	if err != nil {
		t.Fatal(err)
	}
	path := root.Path()
	t.Cleanup(func() { _ = os.RemoveAll(path) })
	if err := unix.Mkfifo(filepath.Join(path, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := root.CaptureSubprocessOutput(); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("CaptureSubprocessOutput error = %v, want ownership failure", err)
	}
	if err := root.Close(); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("Close error = %v, want ownership failure", err)
	}
	if _, err := os.Lstat(filepath.Join(path, "fifo")); err != nil {
		t.Fatalf("special entry was removed: %v", err)
	}
}
