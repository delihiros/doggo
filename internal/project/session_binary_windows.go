//go:build windows

package project

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openGenerationBinary(generation *os.Root, name string) (file *os.File, retErr error) {
	parent, err := generation.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := parent.Close(); err != nil {
			retErr = errors.Join(retErr, err)
			if file != nil {
				retErr = errors.Join(retErr, file.Close())
				file = nil
			}
		}
	}()
	if err := VerifyRootHandle(generation, parent); err != nil {
		return nil, err
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(parent.Fd()),
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(
		&handle,
		windows.FILE_GENERIC_READ|windows.SYNCHRONIZE,
		&attributes,
		&status,
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ,
		windows.FILE_OPEN,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT,
		0,
		0,
	)
	if err != nil {
		return nil, err
	}
	file = os.NewFile(uintptr(handle), name)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("retain generation binary: invalid handle")
	}
	return file, nil
}

func generationBinaryCommand(binary *generationBinary, args ...string) (*exec.Cmd, error) {
	return exec.Command(binary.launchPath, args...), nil
}
