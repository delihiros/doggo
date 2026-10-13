//go:build !linux && !android && !darwin && !ios && !windows

package project

import (
	"fmt"
	"os"
)

func renameGenerationNoReplaceOS(_ *os.File, _ *os.Root, _, _ string) (bool, error) {
	return false, fmt.Errorf("descriptor-relative no-replace generation publication is unsupported on this platform")
}
