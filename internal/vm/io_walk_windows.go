package vm

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

func ioWalkSymlinkLoopError(err error) bool {
	// Stat reports self-referential Windows symlinks with this native error.
	return errors.Is(err, syscall.ELOOP) || errors.Is(err, windows.ERROR_CANT_RESOLVE_FILENAME)
}
