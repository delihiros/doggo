//go:build !windows

package project

import (
	"io/fs"
	"net"
	"os"
	"testing"
)

func createSocketFileForTest(t *testing.T, path string) func() {
	t.Helper()
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix sockets unavailable: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	})
	return func() {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&fs.ModeSocket == 0 {
			t.Fatalf("special-file fixture mode = %s, want socket", info.Mode())
		}
	}
}
