//go:build darwin

package vm

import (
	"errors"
	"os/exec"

	"golang.org/x/sys/unix"
)

func processExitObserved(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}

	kqueue, err := unix.Kqueue()
	if err != nil {
		return false
	}
	defer unix.Close(kqueue)

	changes := []unix.Kevent_t{{
		Ident:  uint64(cmd.Process.Pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}}
	events := make([]unix.Kevent_t, 1)
	timeout := unix.Timespec{}
	n, err := unix.Kevent(kqueue, changes, events, &timeout)
	if errors.Is(err, unix.ESRCH) {
		return true
	}
	if err != nil || n == 0 {
		return false
	}
	if events[0].Flags&unix.EV_ERROR != 0 {
		return unix.Errno(events[0].Data) == unix.ESRCH
	}
	return events[0].Fflags&unix.NOTE_EXIT != 0
}
