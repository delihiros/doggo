//go:build unix

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

func execForwardSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func forwardExecSignal(process *os.Process, signal os.Signal) error {
	return process.Signal(signal)
}

func execChildExitCode(err *exec.ExitError) int {
	if code := err.ExitCode(); code >= 0 {
		return code
	}
	if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return 1
}
