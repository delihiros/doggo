//go:build !windows

package project

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"testing"
)

func createSocketFileForTest(t *testing.T, path string) func(fs.FileInfo, fs.FileInfo) error {
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
	return func(before, after fs.FileInfo) error {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSocket == 0 {
			return fmt.Errorf("special-file fixture mode = %s, want socket", info.Mode())
		}
		if !os.SameFile(before, after) || !os.SameFile(after, info) || after.Mode() != before.Mode() {
			return fmt.Errorf("rejected special file changed")
		}
		return nil
	}
}
