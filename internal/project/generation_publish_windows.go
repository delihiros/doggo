//go:build windows

package project

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var closeGenerationPublishSource = (*os.File).Close

func renameGenerationNoReplaceOS(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
	oldObjectName, err := windows.NewNTUnicodeString(oldName)
	if err != nil {
		return false, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(parent.Fd()),
		ObjectName:    oldObjectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
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
		windows.FILE_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT,
		0,
		0,
	)
	if err != nil {
		return false, fmt.Errorf("open generation publish source: %w", err)
	}
	source := os.NewFile(uintptr(handle), oldName)
	closeSource := func() error {
		if err := closeGenerationPublishSource(source); err != nil {
			return fmt.Errorf("close generation publish source: %w", err)
		}
		return nil
	}
	if err := VerifyRootHandle(retained, source); err != nil {
		return false, errors.Join(err, closeSource())
	}
	buffer, err := generationRenameInformation(windows.Handle(parent.Fd()), newName)
	if err != nil {
		return false, errors.Join(err, closeSource())
	}
	if err := windows.NtSetInformationFile(windows.Handle(source.Fd()), &status, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation); err != nil {
		if ntStatus, ok := err.(windows.NTStatus); ok {
			err = generationRenameNTStatus(ntStatus)
		}
		return false, errors.Join(fmt.Errorf("rename generation publish source: %w", err), closeSource())
	}
	return true, closeSource()
}

func generationRenameNTStatus(status windows.NTStatus) error { return status.Errno() }

func generationRenameInformation(parent windows.Handle, newName string) ([]byte, error) {
	return legacyRenameInformation(parent, newName, false)
}
