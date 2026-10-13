//go:build linux || android

package project

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func openRetainedNoFollow(parent *os.File, name string, _ fs.FileInfo) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}
