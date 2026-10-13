//go:build darwin || ios

package project

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func openGenerationBinary(generation *os.Root, name string) (*os.File, error) {
	return generation.Open(name)
}

func generationBinaryCommand(binary *generationBinary, args ...string) (*exec.Cmd, error) {
	if binary.executable == nil {
		snapshot, err := snapshotGenerationBinary(binary)
		if err != nil {
			return nil, err
		}
		executable, err := NewOwnedTempRoot(".doggo-exec-")
		if err != nil {
			return nil, err
		}
		name := GenerationBinaryName()
		if err := executable.Tree().WriteOwnedFile(name, snapshot.Content, binary.info.Mode().Perm()); err != nil {
			return nil, errors.Join(fmt.Errorf("materialize generation binary: %w", err), executable.Close())
		}
		binary.executable = executable
	}
	command := exec.Command(filepath.Join(binary.executable.Path(), GenerationBinaryName()), args...)
	command.Args[0] = binary.launchPath
	return command, nil
}
