//go:build windows

package vm

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	processTreeWindowsIsProcessInJobProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
	processTreeWindowsCurrentProcessEnv  = detectWindowsProcessTreeEnvironment
)

type windowsProcessTreeController struct {
	mu       sync.Mutex
	job      windows.Handle
	state    windowsProcessTreeState
	strategy windowsProcessTreeStrategy
}

func newProcessTreeController() (processTreeController, error) {
	env, err := processTreeWindowsCurrentProcessEnv()
	if err != nil {
		return nil, err
	}
	strategy, err := resolveWindowsProcessTreeStrategy(env)
	if err != nil {
		return nil, err
	}
	job, err := windowsCreateProcessTreeJob()
	if err != nil {
		return nil, err
	}
	return &windowsProcessTreeController{job: job, strategy: strategy}, nil
}

func (c *windowsProcessTreeController) configure(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Keep the child suspended until it is safely attached to our job object.
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	if c.strategy.breakawayFromParentJob {
		cmd.SysProcAttr.CreationFlags |= windows.CREATE_BREAKAWAY_FROM_JOB
	}
	// Cancellation explicitly terminates the job. Closing the handle on a
	// successful run must not kill surviving descendants.
	cmd.Cancel = func() error {
		return c.cancel(cmd)
	}
	return nil
}

func (c *windowsProcessTreeController) afterStart(ctx context.Context, cmd *exec.Cmd) error {
	processHandle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_SET_QUOTA, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(processHandle)

	if err := c.cancelIfRequested(ctx, cmd); err != nil {
		return err
	}

	if err := windows.AssignProcessToJobObject(c.job, processHandle); err != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if c.isCanceled() {
			return context.Canceled
		}
		return err
	}

	if err := c.markAssignedAndCancelIfNeeded(ctx, cmd); err != nil {
		return err
	}

	threadHandle, err := windowsOpenPrimaryThread(uint32(cmd.Process.Pid))
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if c.isCanceled() {
			return context.Canceled
		}
		return err
	}
	defer windows.CloseHandle(threadHandle)

	if err := c.cancelIfRequested(ctx, cmd); err != nil {
		return err
	}

	_, err = windows.ResumeThread(threadHandle)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if c.isCanceled() {
			return context.Canceled
		}
		return err
	}
	if err := c.cancelIfRequested(ctx, cmd); err != nil {
		return err
	}
	return nil
}

func (c *windowsProcessTreeController) close() error {
	c.mu.Lock()
	if c.job == 0 {
		c.mu.Unlock()
		return nil
	}
	job := c.job
	c.job = 0
	c.mu.Unlock()
	err := windows.CloseHandle(job)
	if err == nil || errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return nil
	}
	return err
}

func (c *windowsProcessTreeController) cancel(cmd *exec.Cmd) error {
	c.mu.Lock()
	action := c.state.cancelAction()
	job := c.job
	c.mu.Unlock()

	switch action {
	case windowsProcessTreeCancelJob:
		return windowsTerminateProcessTreeJob(job)
	default:
		if cmd.Process == nil {
			return nil
		}
		err := cmd.Process.Kill()
		if err == nil || errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
}

func (c *windowsProcessTreeController) cancelIfRequested(ctx context.Context, cmd *exec.Cmd) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if err := c.cancel(cmd); err != nil {
		return err
	}
	return ctx.Err()
}

func (c *windowsProcessTreeController) markAssignedAndCancelIfNeeded(ctx context.Context, cmd *exec.Cmd) error {
	c.mu.Lock()
	canceled := c.state.markAssigned()
	job := c.job
	c.mu.Unlock()
	if !canceled && (ctx == nil || ctx.Err() == nil) {
		return nil
	}
	if err := windowsTerminateProcessTreeJob(job); err != nil {
		return err
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return context.Canceled
}

func (c *windowsProcessTreeController) isCanceled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.isCanceled()
}

func detectWindowsProcessTreeEnvironment() (windowsProcessTreeEnvironment, error) {
	inJob, err := windowsCurrentProcessInJob()
	if err != nil {
		return windowsProcessTreeEnvironment{}, err
	}

	env := windowsProcessTreeEnvironment{
		currentProcessInJob: inJob,
		nestedJobsSupported: windowsNestedJobsSupported(),
	}
	if !inJob {
		return env, nil
	}

	limitFlags, err := windowsCurrentJobLimitFlags()
	if err != nil {
		return windowsProcessTreeEnvironment{}, err
	}
	env.parentJobAllowsBreakaway = limitFlags&(windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK|windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK) != 0
	return env, nil
}

func windowsNestedJobsSupported() bool {
	major, minor, _ := windows.RtlGetNtVersionNumbers()
	return major > 6 || (major == 6 && minor >= 2)
}

func windowsCurrentProcessInJob() (bool, error) {
	var inJob int32
	r1, _, e1 := processTreeWindowsIsProcessInJobProc.Call(
		uintptr(windows.CurrentProcess()),
		0,
		uintptr(unsafe.Pointer(&inJob)),
	)
	if r1 == 0 {
		if e1 != syscall.Errno(0) {
			return false, e1
		}
		return false, syscall.EINVAL
	}
	return inJob != 0, nil
}

func windowsCurrentJobLimitFlags() (uint32, error) {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	err := windows.QueryInformationJobObject(
		0,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	)
	if err != nil {
		return 0, err
	}
	return info.BasicLimitInformation.LimitFlags, nil
}

func windowsCreateProcessTreeJob() (windows.Handle, error) {
	return windows.CreateJobObject(nil, nil)
}

func windowsTerminateProcessTreeJob(job windows.Handle) error {
	if job == 0 {
		return os.ErrProcessDone
	}
	err := windows.TerminateJobObject(job, 137)
	if err == nil || errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return nil
	}
	return err
}

func windowsOpenPrimaryThread(pid uint32) (windows.Handle, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ThreadEntry32{
		Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{})),
	}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return 0, err
	}

	for {
		if entry.OwnerProcessID == pid {
			return windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return 0, windows.ERROR_NOT_FOUND
			}
			return 0, err
		}
	}
}
