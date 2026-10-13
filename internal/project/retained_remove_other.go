//go:build !linux && !android && !darwin && !ios && !windows

package project

import (
	"fmt"
	"io/fs"
	"os"
)

func removeRetainedEntry(_ *os.Root, name string, _ fs.FileInfo, _ *os.Root, _ bool) error {
	return fmt.Errorf("%w: retained removal is unsupported on this platform: %s", ErrOwnershipChanged, name)
}
