//go:build !linux && !android && !darwin && !ios && !windows

package project

import (
	"fmt"
	"os"
)

func renameAtomicRootNoReplaceStep(_, _ *os.File, _, _ string) error {
	return fmt.Errorf("descriptor-relative no-replace file publication is unsupported on this platform")
}
