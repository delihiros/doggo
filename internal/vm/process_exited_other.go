//go:build !linux && !darwin && !windows

package vm

import "os/exec"

func processExitObserved(cmd *exec.Cmd) bool {
	return false
}
