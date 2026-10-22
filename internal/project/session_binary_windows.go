//go:build windows

package project

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	command := exec.Command(filepath.Join(binary.executable.Path(), GenerationBinaryName()), args...)
	command.Args[0] = binary.launchPath
	return command, nil
}

// Windows retains an immutable private executable. Keeping a deny-write handle
// on the published file would prevent SyncPublished from flushing a reused
// generation, even though no content is changed. Never weaken the executable's
// share mode or execute an unverified pathname to work around that conflict.
func prepareGenerationBinary(binary *generationBinary) error {
	snapshot, err := snapshotGenerationBinary(binary)
	if err != nil {
		return err
	}
	executable, err := NewOwnedTempRoot(".doggo-exec-")
	if err != nil {
		return err
	}
	name := GenerationBinaryName()
	if err := executable.Tree().WriteOwnedFile(name, snapshot.Content, binary.info.Mode().Perm()); err != nil {
		return errors.Join(fmt.Errorf("materialize generation binary: %w", err), executable.Close())
	}
	file, err := openGenerationBinary(executable.root, name)
	if err != nil {
		return errors.Join(err, executable.Close())
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(err, file.Close(), executable.Close())
	}
	if err := binary.file.Close(); err != nil {
		return errors.Join(err, file.Close(), executable.Close())
	}
	binary.file, binary.info, binary.executable = file, info, executable
	// Verify the retained copy through its final deny-write descriptor as well.
	_, err = snapshotGenerationBinary(binary)
	return err
}
