//go:build linux

package vm

import (
	"os/exec"

	"golang.org/x/sys/unix"
)

func processExitObserved(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}

	var info unix.Siginfo
	if err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil); err != nil {
		return false
	}
	return info.Signo != 0
}
