//go:build !windows && !aix && !android && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris && !zos

package project

import (
	"fmt"
	"os"
)

func syncAtomicParent(parent *os.File) error { return syncDirectory(parent) }

func syncDirectory(directory *os.File) error { return directory.Sync() }

func syncFile(file *os.File) error { return file.Sync() }

func createFileAtomicAt(_ *os.File, _ string) (*os.File, error) {
	return nil, fmt.Errorf("descriptor-relative atomic writes are unsupported on this platform")
}

func replaceFileAtomicRoot(_, _ *os.File, _, _ string) (bool, error) {
	return false, fmt.Errorf("descriptor-relative atomic writes are unsupported on this platform")
}

func renameAtomicRootStep(_, _ *os.File, _, _ string) error {
	return fmt.Errorf("descriptor-relative atomic writes are unsupported on this platform")
}
