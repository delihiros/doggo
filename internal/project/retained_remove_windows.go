//go:build windows

package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type retainedDispositionInformationEx struct {
	Flags uint32
}

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
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return retainedRemoveOwnershipError(name, err)
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(parentFile.Fd()),
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	options := uint32(windows.FILE_OPEN_REPARSE_POINT | windows.FILE_SYNCHRONOUS_IO_NONALERT)
	if directory {
		options |= windows.FILE_DIRECTORY_FILE
	} else if expected.Mode()&os.ModeSymlink == 0 {
		options |= windows.FILE_NON_DIRECTORY_FILE
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(
		&handle,
		windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		&attributes,
		&status,
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		options,
		0,
		0,
	)
	if err != nil {
		return retainedRemoveOwnershipError(name, err)
	}
	retained := os.NewFile(uintptr(handle), name)
	if retained == nil {
		_ = windows.CloseHandle(handle)
		return retainedRemoveOwnershipError(name, fmt.Errorf("invalid retained handle"))
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
	if beforeRetainedRemoveForTest != nil {
		beforeRetainedRemoveForTest(parent, name)
	}
	current, currentErr := parent.Lstat(name)
	changed := currentErr != nil || !sameRetainedEntry(opened, current)
	// Subprocess output such as Git objects may be read-only. Delete only this
	// verified handle; clearing attributes through the path could alter a racer.
	disposition := retainedDispositionInformationEx{Flags: windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE}
	err = windows.NtSetInformationFile(
		handle,
		&status,
		(*byte)(unsafe.Pointer(&disposition)),
		uint32(unsafe.Sizeof(disposition)),
		windows.FileDispositionInformationEx,
	)
	committed = err == nil
	if directory && (errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) || errors.Is(err, windows.STATUS_DIRECTORY_NOT_EMPTY)) {
		err = retainedRemoveOwnershipError(name, err)
	}
	return retainedDispositionResult(name, changed, currentErr, err)
}
