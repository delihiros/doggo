//go:build android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris || zos

package project

import (
	"os"

	"golang.org/x/sys/unix"
)

func openGenerationLockFile(parent *os.File, name string, create bool) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if create {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Openat(int(parent.Fd()), name, flags, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func lockGenerationFile(file *os.File, exclusive bool) error {
	operation := unix.LOCK_SH
	if exclusive {
		operation = unix.LOCK_EX
	}
	return unix.Flock(int(file.Fd()), operation)
}

func openGenerationPinFile(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func unlockGenerationFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
