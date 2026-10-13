//go:build !windows && !android && !darwin && !dragonfly && !freebsd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris && !zos

package project

import (
	"fmt"
	"os"
)

func openGenerationLockFile(_ *os.File, _ string, _ bool) (*os.File, error) {
	return nil, fmt.Errorf("generation leases are unsupported on this platform")
}

func lockGenerationFile(_ *os.File, _ bool) error {
	return fmt.Errorf("generation leases are unsupported on this platform")
}

func unlockGenerationFile(_ *os.File) error {
	return fmt.Errorf("generation leases are unsupported on this platform")
}
