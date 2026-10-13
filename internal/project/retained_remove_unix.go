//go:build linux || android || darwin || ios

package project

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func removeRetainedEntry(parent *os.Root, name string, expected fs.FileInfo, root *os.Root, directory bool) (retErr error) {
	committed := false
	parentFile, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer func() { retErr = joinRetainedRemovalClose(retErr, committed, parentFile.Close()) }()
	if err := VerifyRootHandle(parent, parentFile); err != nil {
		return err
	}
	retained, err := openRetainedNoFollow(parentFile, name, expected)
	if err != nil {
		return retainedRemoveOwnershipError(name, err)
	}
	defer func() { retErr = joinRetainedRemovalClose(retErr, committed, retained.Close()) }()
	opened, err := retained.Stat()
	if err != nil || !sameRetainedEntry(expected, opened) {
		return retainedRemoveOwnershipError(name, err)
	}
	if directory {
		rootInfo, rootErr := root.Stat(".")
		if rootErr != nil || !sameRetainedEntry(rootInfo, opened) {
			return retainedRemoveOwnershipError(name, rootErr)
		}
	}
	flags := 0
	if directory {
		flags = unix.AT_REMOVEDIR
	}
	if beforeRetainedRemoveForTest != nil {
		beforeRetainedRemoveForTest(parent, name)
	}
	current, err := parent.Lstat(name)
	if err != nil || !sameRetainedEntry(opened, current) {
		return retainedRemoveOwnershipError(name, err)
	}
	err = unix.Unlinkat(int(parentFile.Fd()), name, flags)
	committed = err == nil
	if directory && (errors.Is(err, unix.ENOTEMPTY) || errors.Is(err, unix.EEXIST)) {
		return retainedRemoveOwnershipError(name, err)
	}
	return err
}
