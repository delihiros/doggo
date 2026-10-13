//go:build windows

package project

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const fileRenameInformationExClass = 65

var atomicNtSetInformationFile = windows.NtSetInformationFile

var reOpenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

func syncAtomicParent(parent *os.File) error {
	return syncDirectory(parent)
}

func syncDirectory(directory *os.File) error {
	syncHandle, err := openDirectoryForSync(directory)
	if err != nil {
		return err
	}
	return errors.Join(syncHandle.Sync(), syncHandle.Close())
}

func syncFile(file *os.File) error {
	handle, _, callErr := reOpenFile.Call(
		file.Fd(),
		uintptr(windows.GENERIC_WRITE),
		uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE),
		0,
	)
	if windows.Handle(handle) == windows.InvalidHandle {
		return callErr
	}
	reopened := os.NewFile(handle, file.Name())
	return errors.Join(reopened.Sync(), reopened.Close())
}

func openDirectoryForSync(directory *os.File) (*os.File, error) {
	objectName, err := windows.NewNTUnicodeString("")
	if err != nil {
		return nil, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(directory.Fd()),
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(
		&handle,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.SYNCHRONIZE,
		&attributes,
		&status,
		nil,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		windows.FILE_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_OPEN_REPARSE_POINT,
		0,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), directory.Name()), nil
}

func createFileAtomicAt(parent *os.File, name string) (*os.File, error) {
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
	var allocationSize int64
	err = windows.NtCreateFile(
		&handle,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE|windows.SYNCHRONIZE,
		&attributes,
		&status,
		&allocationSize,
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_CREATE,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}

type fileRenameInformationEx struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

type legacyFileRenameInformation struct {
	ReplaceIfExists bool
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

func replaceFileAtomicRoot(parent, temp *os.File, _, newName string) (bool, error) {
	if err := atomicRootRename(parent, temp, "", newName); err != nil {
		return false, err
	}
	if err := atomicParentSync(parent); err != nil {
		return true, err
	}
	return true, nil
}

func renameAtomicRootStep(parent, temp *os.File, _, newName string) error {
	buffer, err := renameInformationEx(windows.Handle(parent.Fd()), newName)
	if err != nil {
		return err
	}
	var status windows.IO_STATUS_BLOCK
	if err := atomicNtSetInformationFile(windows.Handle(temp.Fd()), &status, &buffer[0], uint32(len(buffer)), fileRenameInformationExClass); err == nil {
		return nil
	}
	buffer, err = legacyRenameInformation(windows.Handle(parent.Fd()), newName, true)
	if err != nil {
		return err
	}
	return setAtomicRenameInformation(temp, buffer, windows.FileRenameInformation)
}

func renameAtomicRootNoReplaceStep(parent, temp *os.File, _, newName string) error {
	buffer, err := legacyRenameInformation(windows.Handle(parent.Fd()), newName, false)
	if err != nil {
		return err
	}
	return setAtomicRenameInformation(temp, buffer, windows.FileRenameInformation)
}

func renameInformationEx(parent windows.Handle, newName string) ([]byte, error) {
	name, err := windows.UTF16FromString(newName)
	if err != nil {
		return nil, err
	}
	nameBytes := (len(name) - 1) * 2
	var header fileRenameInformationEx
	buffer := make([]byte, int(unsafe.Offsetof(header.FileName))+nameBytes)
	info := (*fileRenameInformationEx)(unsafe.Pointer(&buffer[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.RootDirectory = parent
	info.FileNameLength = uint32(nameBytes)
	copy(unsafe.Slice(&info.FileName[0], len(name)-1), name[:len(name)-1])
	return buffer, nil
}

func legacyRenameInformation(parent windows.Handle, newName string, replaceIfExists bool) ([]byte, error) {
	name, err := windows.UTF16FromString(newName)
	if err != nil {
		return nil, err
	}
	nameBytes := (len(name) - 1) * 2
	var header legacyFileRenameInformation
	buffer := make([]byte, int(unsafe.Offsetof(header.FileName))+nameBytes)
	info := (*legacyFileRenameInformation)(unsafe.Pointer(&buffer[0]))
	info.ReplaceIfExists = replaceIfExists
	info.RootDirectory = parent
	info.FileNameLength = uint32(nameBytes)
	copy(unsafe.Slice(&info.FileName[0], len(name)-1), name[:len(name)-1])
	return buffer, nil
}

func setAtomicRenameInformation(temp *os.File, buffer []byte, class uint32) error {
	var status windows.IO_STATUS_BLOCK
	err := atomicNtSetInformationFile(windows.Handle(temp.Fd()), &status, &buffer[0], uint32(len(buffer)), class)
	if ntStatus, ok := err.(windows.NTStatus); ok {
		return ntStatus.Errno()
	}
	return err
}
