//go:build unix

package vm

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

type unixProcessTreeController struct{}

func newProcessTreeController() (processTreeController, error) {
	return unixProcessTreeController{}, nil
}

func (unixProcessTreeController) configure(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == nil || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}

func (unixProcessTreeController) afterStart(context.Context, *exec.Cmd) error {
	return nil
}

func (unixProcessTreeController) close() error {
	return nil
}
