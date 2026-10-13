//go:build !linux && !android && !darwin && !ios && !windows

package project

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func openGenerationBinary(generation *os.Root, name string) (*os.File, error) {
	return generation.Open(name)
}

func generationBinaryCommand(_ *generationBinary, _ ...string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("executing a retained generation binary is unsupported on %s", runtime.GOOS)
}
