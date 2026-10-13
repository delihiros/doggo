//go:build darwin || ios

package project

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameAtomicRootNoReplaceStep(parent, _ *os.File, oldName, newName string) error {
	return unix.RenameatxNp(int(parent.Fd()), oldName, int(parent.Fd()), newName, unix.RENAME_EXCL)
}
