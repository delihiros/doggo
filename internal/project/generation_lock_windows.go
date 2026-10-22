//go:build windows

package project

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openGenerationLockFile(parent *os.File, name string, create bool) (*os.File, error) {
	return openGenerationLeaseFile(parent, name, create, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
}

func openGenerationPinFile(parent *os.File, name string) (*os.File, error) {
	return openGenerationLeaseFile(parent, name, false, windows.FILE_GENERIC_READ|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
}

func openGenerationLeaseFile(parent *os.File, name string, create bool, access, share uint32) (*os.File, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(parent.Fd()),
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	disposition := uint32(windows.FILE_OPEN)
	if create {
		disposition = windows.FILE_OPEN_IF
	}
	err = windows.NtCreateFile(
		&handle,
		access,
		&attributes,
		&status,
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		share,
		disposition,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT,
		0,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}

func lockGenerationFile(file *os.File, exclusive bool) error {
	var flags uint32
	if exclusive {
		flags = windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	return windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, ^uint32(0), ^uint32(0), &windows.Overlapped{})
}

func unlockGenerationFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, ^uint32(0), ^uint32(0), &windows.Overlapped{})
}
