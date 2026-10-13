//go:build !unix && !windows

package cli

import (
	"os"
	"os/exec"
)

func execForwardSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func forwardExecSignal(process *os.Process, _ os.Signal) error {
	return process.Kill()
}

func execChildExitCode(err *exec.ExitError) int {
	if code := err.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
