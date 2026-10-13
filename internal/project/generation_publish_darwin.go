//go:build darwin || ios

package project

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameGenerationNoReplaceOS(parent *os.File, _ *os.Root, oldName, newName string) (bool, error) {
	err := unix.RenameatxNp(int(parent.Fd()), oldName, int(parent.Fd()), newName, unix.RENAME_EXCL)
	return err == nil, err
}
