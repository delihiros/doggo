//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris || zos

package project

import (
	"os"

	"golang.org/x/sys/unix"
)

func syncAtomicParent(parent *os.File) error { return syncDirectory(parent) }

func syncDirectory(directory *os.File) error { return directory.Sync() }

func syncFile(file *os.File) error { return file.Sync() }

func createFileAtomicAt(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func replaceFileAtomicRoot(parent, _ *os.File, oldName, newName string) (bool, error) {
	if err := atomicRootRename(parent, nil, oldName, newName); err != nil {
		return false, err
	}
	if err := atomicParentSync(parent); err != nil {
		return true, err
	}
	return true, nil
}

func renameAtomicRootStep(parent, _ *os.File, oldName, newName string) error {
	return unix.Renameat(int(parent.Fd()), oldName, int(parent.Fd()), newName)
}
