package vm

import (
	"context"
	"errors"
	"os/exec"
)

type processTreeController interface {
	configure(*exec.Cmd) error
	afterStart(context.Context, *exec.Cmd) error
	close() error
}

type processHandleControl struct {
	controller             processTreeController
	killTreeUnsupportedErr error
}

var (
	errProcessTreeUnsupported    = errors.New("process-tree management is unsupported")
	processTreeControllerFactory = newProcessTreeController
)

type processTreeUnsupportedError struct {
	message string
}

func (e processTreeUnsupportedError) Error() string {
	return e.message
}

func (e processTreeUnsupportedError) Is(target error) bool {
	return target == errProcessTreeUnsupported
}

func newProcessTreeUnsupportedError(message string) error {
	return processTreeUnsupportedError{message: message}
}

type noopProcessTreeController struct{}

func (noopProcessTreeController) configure(*exec.Cmd) error {
	return nil
}

func (noopProcessTreeController) afterStart(context.Context, *exec.Cmd) error {
	return nil
}

func (noopProcessTreeController) close() error {
	return nil
}

func processHandleController(ctx context.Context, timeoutMs int64) (processHandleControl, error) {
	requireTreeControl := processNeedsTreeControl(ctx, timeoutMs)
	controller, err := processTreeControllerFactory()
	if err == nil {
		return processHandleControl{controller: controller}, nil
	}
	if errors.Is(err, errProcessTreeUnsupported) && !requireTreeControl {
		return processHandleControl{
			controller:             noopProcessTreeController{},
			killTreeUnsupportedErr: err,
		}, nil
	}
	return processHandleControl{}, err
}

func processTreeSupported() (bool, error) {
	controller, err := processTreeControllerFactory()
	if err == nil {
		_ = controller.close()
		return true, nil
	}
	if errors.Is(err, errProcessTreeUnsupported) {
		return false, nil
	}
	return false, err
}

func processNeedsTreeControl(ctx context.Context, timeoutMs int64) bool {
	return timeoutMs > 0 || ctx.Done() != nil
}

type windowsProcessTreeEnvironment struct {
	currentProcessInJob      bool
	nestedJobsSupported      bool
	parentJobAllowsBreakaway bool
}

type windowsProcessTreeStrategy struct {
	breakawayFromParentJob bool
}

type windowsProcessTreeCancelAction int

const (
	windowsProcessTreeCancelProcess windowsProcessTreeCancelAction = iota
	windowsProcessTreeCancelJob
)

type windowsProcessTreeState struct {
	assigned bool
	canceled bool
}

func (s *windowsProcessTreeState) cancelAction() windowsProcessTreeCancelAction {
	s.canceled = true
	if s.assigned {
		return windowsProcessTreeCancelJob
	}
	return windowsProcessTreeCancelProcess
}

func (s *windowsProcessTreeState) markAssigned() bool {
	s.assigned = true
	return s.canceled
}

func (s windowsProcessTreeState) isCanceled() bool {
	return s.canceled
}

func resolveWindowsProcessTreeStrategy(env windowsProcessTreeEnvironment) (windowsProcessTreeStrategy, error) {
	if !env.currentProcessInJob || env.nestedJobsSupported {
		return windowsProcessTreeStrategy{}, nil
	}
	if env.parentJobAllowsBreakaway {
		return windowsProcessTreeStrategy{breakawayFromParentJob: true}, nil
	}
	return windowsProcessTreeStrategy{}, newProcessTreeUnsupportedError("Windows process-tree management requires nested jobs or a parent job with breakaway enabled")
}

func processStartAndWait(ctx context.Context, cmd *exec.Cmd, controller processTreeController) error {
	if err := controller.configure(cmd); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if ctx != nil && ctx.Err() != nil && cmd.Cancel != nil {
		_ = cmd.Cancel()
	}
	if err := controller.afterStart(ctx, cmd); err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}
