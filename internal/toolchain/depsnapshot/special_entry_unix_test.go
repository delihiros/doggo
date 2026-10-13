//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris || zos

package depsnapshot

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSnapshotDependenciesRejectsFIFOEntry(t *testing.T) {
	root := t.TempDir()
	dep := filepath.Join(root, "dep")
	writeFile(t, filepath.Join(dep, "doggo.mod"), `{:module "example/dep" :deps []}`)
	if err := unix.Mkfifo(filepath.Join(dep, "bad-entry"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), []string{"./dep"})
	if err == nil || !strings.Contains(err.Error(), "unsupported filesystem entry") {
		t.Fatalf("error = %v, want FIFO rejection", err)
	}
}
