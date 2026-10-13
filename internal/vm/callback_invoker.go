package vm

import (
	"context"
	"fmt"
	"strings"

	"doggo/internal/types"
	"doggo/internal/vm/engine"
)

// VMCallbackInvoker adapts reified Go method calls to the Doggo runtime.
type VMCallbackInvoker struct {
	runtime        CallableRuntime
	origin         engine.SourceOrigin
	bindingContext context.Context
}

func NewVMCallbackInvoker(vm *VM) *VMCallbackInvoker {
	if vm == nil {
		return &VMCallbackInvoker{}
	}
	invoker := &VMCallbackInvoker{
		runtime:        vm.snapshotCallableRuntime(),
		bindingContext: withBindingState(vm.ctx),
	}
	if vm.frameIndex > 0 {
		if frame := vm.currentFrame(); frame != nil {
			if file := strings.TrimSpace(frame.CurrentFile()); file != "" {
				invoker.origin.File = file
				if vm.session != nil && vm.session.runner != nil {
					if origin, found := vm.session.runner.resolveSourceOrigin(file); found {
						invoker.origin = origin
					}
				}
			}
		}
	}
	if invoker.origin.File == "" {
		invoker.origin, _ = sourceOriginFromContext(vm.ctx)
	}
	return invoker
}

// Invoke calls a Doggo callback. The VM owns its environment, so env is unused.
func (i *VMCallbackInvoker) Invoke(ctx context.Context, fn types.Value, args []types.Value, _ types.Environment) (types.Value, error) {
	if i == nil || i.runtime == nil {
		return nil, fmt.Errorf("reify callback runtime is not initialized")
	}
	return i.runtime.Apply(i.callContext(ctx), fn, args)
}

func (i *VMCallbackInvoker) callContext(ctx context.Context) context.Context {
	parent := ctx
	if bindingStateFromContext(parent) == nil {
		parent = i.bindingContext
	}
	ctx = forkBindingContextFrom(ctx, parent)
	if i.origin.File == "" {
		return ctx
	}
	return withSourceOrigin(ctx, i.origin)
}
