//go:build !windows

package vm

import (
	"errors"
	"syscall"
)

func ioWalkSymlinkLoopError(err error) bool {
	return errors.Is(err, syscall.ELOOP)
}
