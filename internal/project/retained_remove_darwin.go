//go:build darwin || ios

package project

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func openRetainedNoFollow(parent *os.File, name string, expected fs.FileInfo) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC
	if expected.Mode()&os.ModeSymlink != 0 {
		// O_NOFOLLOW rejects symlinks even with O_SYMLINK on Darwin.
		flags |= unix.O_SYMLINK
	} else {
		flags |= unix.O_NOFOLLOW
		if expected.IsDir() {
			flags |= unix.O_DIRECTORY
		}
	}
	fd, err := unix.Openat(int(parent.Fd()), name, flags, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}
