//go:build linux || android

package project

import (
	"os"
	"os/exec"
)

func openGenerationBinary(generation *os.Root, name string) (*os.File, error) {
	return generation.Open(name)
}

func generationBinaryCommand(binary *generationBinary, args ...string) (*exec.Cmd, error) {
	return &exec.Cmd{
		Path:       "/proc/self/fd/3",
		Args:       append([]string{binary.launchPath}, args...),
		ExtraFiles: []*os.File{binary.file},
	}, nil
}
