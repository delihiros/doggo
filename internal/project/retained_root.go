package project

import (
	"errors"
	"os"
)

// RetainedRoot transfers one already-open directory identity with its
// canonical path relative to an owned project root.
type RetainedRoot struct {
	Path string
	Root *os.Root
}

func CloseRetainedRoots(roots []RetainedRoot) error {
	var err error
	for i := len(roots) - 1; i >= 0; i-- {
		if roots[i].Root != nil {
			err = errors.Join(err, roots[i].Root.Close())
		}
	}
	return err
}
