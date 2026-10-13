package vm

import (
	"context"
	"fmt"
	"time"

	"doggo/internal/compiler"
	"doggo/internal/stm"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/types/fun"
)

// executionMachine is the low-level execution kernel for VM control flow.
type executionMachine struct {
	vm       *VM
	env      *vmExecutionEnv
	borrowed bool
	nested   bool
}

func newVMExecutionMachine(vm *VM) *executionMachine {
	return &executionMachine{vm: vm}
}

func newEnvExecutionMachine(env vmExecutionEnv) *executionMachine {
	envCopy := env
	return &executionMachine{
		env:      &envCopy,
		borrowed: true,
	}
}

func (m *executionMachine) requireVM(action string) (*VM, error) {
	if m == nil || m.vm == nil {
		return nil, fmt.Errorf("%s: VM-backed execution machine not initialized", action)
	}
	return m.vm, nil
}

func (m *executionMachine) requireEnv(action string) (*vmExecutionEnv, error) {
	if m == nil || m.env == nil {
		return nil, fmt.Errorf("%s: execution environment not initialized", action)
	}
	return m.env, nil
}

func (m *executionMachine) executeCall(numArgs int) error {
	vm, err := m.requireVM("executeCall")
	if err != nil {
		return err
	}

	callee := vm.stack[vm.sp-1-numArgs]

	// Cell透過: callee が Cell なら中身を取り出す（letfn で使用）
	if cell, ok := callee.(*Cell); ok {
		callee = cell.Value
		vm.stack[vm.sp-1-numArgs] = callee // スタック上も更新
	}

	switch fn := callee.(type) {
	case *compiler.Closure:
		return m.callClosure(fn, numArgs)
	case *Builtin:
		return m.callBuiltin(fn, numArgs)
	case *fun.Multimethod:
		// Multimethod は types.Fn より先にチェック（Multimethod は types.Fn を実装）
		return m.callMultimethod(fn, numArgs)
	case types.Fn:
		return m.callFn(fn, numArgs)
	default:
		return vm.newRuntimeError("calling non-function: %T", callee)
	}
}

func (vm *VM) executeCall(numArgs int) error {
	return newVMExecutionMachine(vm).executeCall(numArgs)
}

func checkArity(numParams int, variadic bool, numArgs int) bool {
	if variadic {
		return numArgs >= numParams
	}
	return numArgs == numParams
}

type resolvedClosureCall struct {
	numParams int
	variadic  bool
	entryIP   int
}

func resolveClosureCall(cl *compiler.Closure, numArgs int) (resolvedClosureCall, error) {
	fn := cl.Fn
	if checkArity(fn.NumParameters, fn.Variadic, numArgs) {
		return resolvedClosureCall{
			numParams: fn.NumParameters,
			variadic:  fn.Variadic,
			entryIP:   0,
		}, nil
	}

	for _, arity := range fn.Arities {
		if checkArity(arity.NumParameters, arity.Variadic, numArgs) {
			return resolvedClosureCall{
				numParams: arity.NumParameters,
				variadic:  arity.Variadic,
				entryIP:   arity.EntryIP,
			}, nil
		}
	}

	return resolvedClosureCall{}, fmt.Errorf(
		"wrong number of arguments: want %d (or compatible arity), got=%d",
		fn.NumParameters,
		numArgs,
	)
}

func (m *executionMachine) callClosure(cl *compiler.Closure, numArgs int) error {
	vm, err := m.requireVM("callClosure")
	if err != nil {
		return err
	}

	resolved, err := resolveClosureCall(cl, numArgs)
	if err != nil {
		return vm.newRuntimeError("%s", err.Error())
	}

	return m.callClosureWithArity(cl, numArgs, resolved.numParams, resolved.variadic, resolved.entryIP)
}

func (m *executionMachine) callClosureWithArity(cl *compiler.Closure, numArgs, numParams int, variadic bool, entryIP int) error {
	vm, err := m.requireVM("callClosureWithArity")
	if err != nil {
		return err
	}

	if variadic {
		numRequired := numParams
		numExtra := numArgs - numRequired

		extraArgs := make([]types.Value, numExtra)
		for i := 0; i < numExtra; i++ {
			extraArgs[i] = vm.stack[vm.sp-numExtra+i]
		}
		restList := coll.NewList(extraArgs...)

		vm.sp = vm.sp - numExtra
		vm.stack[vm.sp] = restList
		vm.sp++

		frame := acquireFrame(cl, vm.sp-(numRequired+1))
		frame.ip = entryIP - 1
		if vm.profiler != nil && vm.profiler.IsEnabled() {
			frame.profileStartTime = time.Now().UnixNano()
		}
		vm.pushFrame(frame)
		vm.sp = frame.basePointer + cl.Fn.NumLocals
		return nil
	}

	frame := acquireFrame(cl, vm.sp-numArgs)
	frame.ip = entryIP - 1
	if vm.profiler != nil && vm.profiler.IsEnabled() {
		frame.profileStartTime = time.Now().UnixNano()
	}
	vm.pushFrame(frame)
	vm.sp = frame.basePointer + cl.Fn.NumLocals
	return nil
}

func (m *executionMachine) callBuiltin(builtin *Builtin, numArgs int) error {
	vm, err := m.requireVM("callBuiltin")
	if err != nil {
		return err
	}

	if numArgs == 2 && builtin.Fn2 != nil {
		arg1 := vm.stack[vm.sp-2]
		arg2 := vm.stack[vm.sp-1]
		result := builtin.Fn2(arg1, arg2)
		vm.sp = vm.sp - 3

		if errVal, ok := result.(*Error); ok {
			return fmt.Errorf("%s", errVal.Message)
		}
		if err := coll.CollectionError(result); err != nil {
			return err
		}

		if result != nil {
			vm.push(result)
		} else {
			vm.push(atom.NilValue)
		}
		return nil
	}

	args := make([]types.Value, numArgs)
	copy(args, vm.stack[vm.sp-numArgs:vm.sp])

	if vm.cachedVMCtx == nil {
		vm.cachedVMCtx = bindVMContext(vm.ctx, vm)
	}
	result, err := builtin.Invoke(vm.cachedVMCtx, args...)
	vm.sp = vm.sp - numArgs - 1

	if err != nil {
		return err
	}

	if result != nil {
		vm.push(result)
	} else {
		vm.push(atom.NilValue)
	}

	return nil
}

func (m *executionMachine) callFn(fn types.Fn, numArgs int) error {
	vm, err := m.requireVM("callFn")
	if err != nil {
		return err
	}

	args := make([]types.Value, numArgs)
	for i := 0; i < numArgs; i++ {
		args[i] = vm.stack[vm.sp-numArgs+i]
	}
	vm.sp = vm.sp - numArgs - 1

	if vm.cachedVMCtx == nil {
		vm.cachedVMCtx = bindVMContext(vm.ctx, vm)
	}
	result, err := fn.Invoke(vm.cachedVMCtx, args...)
	if err != nil {
		return err
	}

	if result != nil {
		vm.push(result)
	} else {
		vm.push(atom.NilValue)
	}

	return nil
}

func (m *executionMachine) callMultimethod(mm *fun.Multimethod, numArgs int) error {
	vm, err := m.requireVM("callMultimethod")
	if err != nil {
		return err
	}

	args := make([]types.Value, numArgs)
	copy(args, vm.stack[vm.sp-numArgs:vm.sp])
	vm.sp = vm.sp - numArgs - 1

	dispatchVal, err := m.invokeCallable(mm.Dispatch, args)
	if err != nil {
		return vm.newRuntimeError("multimethod %s: dispatch error: %v", mm.Name, err)
	}

	method := mm.GetMethod(dispatchVal)
	if method == nil {
		method = mm.GetMethod(atom.NewKeyword("default"))
	}
	if method == nil {
		return vm.newRuntimeError("multimethod %s: no method for dispatch value %s", mm.Name, dispatchVal.String())
	}

	result, err := m.invokeCallable(method, args)
	if err != nil {
		return err
	}

	if result != nil {
		vm.push(result)
	} else {
		vm.push(atom.NilValue)
	}

	return nil
}

func (m *executionMachine) invokeCallable(callee types.Value, args []types.Value) (types.Value, error) {
	vm, err := m.requireVM("invokeCallable")
	if err != nil {
		return nil, err
	}

	runtime := vm.callableRuntime
	if runtime == nil {
		return nil, vm.newRuntimeError("callable runtime not initialized")
	}
	return runtime.Apply(vm.ctx, callee, args)
}

func (m *executionMachine) Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	vm, err := m.requireVM("Apply")
	if err != nil {
		return nil, err
	}

	runtime := vm.callableRuntime
	if runtime == nil {
		return nil, vm.newRuntimeError("callable runtime not initialized")
	}
	result, err := runtime.Apply(ctx, fn, args)
	if err != nil {
		switch fn.(type) {
		case *compiler.Closure, *fun.Multimethod, *Builtin, types.Fn:
			return nil, err
		default:
			return nil, vm.newRuntimeError("Apply: expected function, got %T", fn)
		}
	}
	return result, nil
}

func (m *executionMachine) stmStore() *stm.Store {
	vm, err := m.requireVM("stmStore")
	if err != nil {
		return nil
	}
	if vm.session != nil {
		if vm.session.stmStore == nil {
			vm.session.stmStore = stm.NewStore()
		}
		vm.stmStore = vm.session.stmStore
		return vm.stmStore
	}
	if vm.stmStore == nil {
		vm.stmStore = stm.NewStore()
	}
	return vm.stmStore
}

func (m *executionMachine) setSTMStore(store *stm.Store) {
	vm, err := m.requireVM("setSTMStore")
	if err != nil {
		return
	}
	vm.stmStore = store
	vm.callableRuntime = nil
	if vm.session != nil {
		vm.session.stmStore = store
		vm.session.callableRuntime = nil
	}
}

func (m *executionMachine) applyCallableWithBinding(ctx context.Context, bindingVM *VM, fn types.Value, args []types.Value) (types.Value, error) {
	switch callable := fn.(type) {
	case *compiler.Closure:
		return m.invokeClosure(ctx, callable, args)
	case *fun.Multimethod:
		return m.applyMultimethodWithBinding(ctx, bindingVM, callable, args)
	case *Builtin:
		return m.invokeBuiltinOrFnWithBinding(ctx, bindingVM, callable, args)
	case types.Fn:
		return m.invokeBuiltinOrFnWithBinding(ctx, bindingVM, callable, args)
	default:
		return nil, fmt.Errorf("Apply: expected function, got %T", fn)
	}
}

func (m *executionMachine) applyCallableFastWithBinding(ctx context.Context, bindingVM *VM, fn types.Value, args []types.Value) (types.Value, error) {
	if result, ok := DirectApply(fn, args); ok {
		return result, nil
	}
	return m.applyCallableWithBinding(ctx, bindingVM, fn, args)
}

func (m *executionMachine) invokeBuiltinOrFnWithBinding(ctx context.Context, bindingVM *VM, fn types.Value, args []types.Value) (types.Value, error) {
	if bindingVM != nil {
		callCtx := bindVMContext(ctx, bindingVM)
		switch callable := fn.(type) {
		case *Builtin:
			return callable.Invoke(callCtx, args...)
		case types.Fn:
			return callable.Invoke(callCtx, args...)
		default:
			return nil, fmt.Errorf("cannot invoke non-callable: %T", fn)
		}
	}

	return m.withBorrowedMachineValue(ctx, func(tempVM *VM) (types.Value, error) {
		callCtx := bindVMContext(ctx, tempVM)
		switch callable := fn.(type) {
		case *Builtin:
			return callable.Invoke(callCtx, args...)
		case types.Fn:
			return callable.Invoke(callCtx, args...)
		default:
			return nil, fmt.Errorf("cannot invoke non-callable: %T", fn)
		}
	})
}

func (m *executionMachine) applyMultimethodWithBinding(ctx context.Context, bindingVM *VM, mm *fun.Multimethod, args []types.Value) (types.Value, error) {
	dispatchVal, err := m.applyCallableWithBinding(ctx, bindingVM, mm.Dispatch, args)
	if err != nil {
		return nil, fmt.Errorf("multimethod %s: dispatch error: %v", mm.Name, err)
	}

	method := mm.GetMethod(dispatchVal)
	if method == nil {
		method = mm.GetMethod(atom.NewKeyword("default"))
	}
	if method == nil {
		return nil, fmt.Errorf("multimethod %s: no method for dispatch value %s", mm.Name, dispatchVal.String())
	}

	return m.applyCallableWithBinding(ctx, bindingVM, method, args)
}

func (m *executionMachine) withBorrowedMachine(ctx context.Context, fn func(*VM) error) error {
	env, err := m.requireEnv("borrowed machine")
	if err != nil {
		return err
	}

	borrowedMachine := *m
	borrowedMachine.borrowed = true
	if borrowedMachine.borrowed {
		// Borrowed machine execution is isolated from the caller's VM state.
	}

	vm := acquireVMWithBuiltins(env.builtinCatalog, env.builtins)
	env.applyToVM(vm, ctx)
	defer releaseVM(vm)
	return fn(vm)
}

func (m *executionMachine) withBorrowedMachineValue(ctx context.Context, fn func(*VM) (types.Value, error)) (types.Value, error) {
	var result types.Value
	err := m.withBorrowedMachine(ctx, func(vm *VM) error {
		var innerErr error
		result, innerErr = fn(vm)
		return innerErr
	})
	return result, err
}

func (runtime exactCallableRuntime) newExecutionMachine() *executionMachine {
	if runtime.env == nil {
		return nil
	}
	return newEnvExecutionMachine(*runtime.env)
}

func (runtime exactCallableRuntime) withBorrowedMachine(ctx context.Context, fn func(*VM) error) error {
	machine := runtime.newExecutionMachine()
	if machine == nil {
		return fmt.Errorf("callable execution machine is not initialized")
	}
	return machine.withBorrowedMachine(ctx, fn)
}

func (runtime exactCallableRuntime) invokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	machine := runtime.newExecutionMachine()
	if machine == nil {
		return nil, fmt.Errorf("callable execution machine is not initialized")
	}
	return machine.invokeClosure(ctx, cl, args)
}

func (m *executionMachine) invokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	if _, err := m.requireEnv("invokeClosure"); err != nil {
		return nil, err
	}

	return m.withBorrowedMachineValue(ctx, func(childVM *VM) (types.Value, error) {
		nestedMachine := newVMExecutionMachine(childVM)
		nestedMachine.nested = true
		if nestedMachine.nested {
			// Nested VM handoff keeps the borrowed child machine isolated from the parent.
		}

		if err := childVM.push(cl); err != nil {
			return nil, err
		}
		for _, arg := range args {
			if err := childVM.push(arg); err != nil {
				return nil, err
			}
		}

		resolved, err := resolveClosureCall(cl, len(args))
		if err != nil {
			return nil, err
		}
		if err := newVMExecutionMachine(childVM).callClosureWithArity(cl, len(args), resolved.numParams, resolved.variadic, resolved.entryIP); err != nil {
			return nil, err
		}

		if childVM.frameIndex == 0 {
			result := childVM.StackTop()
			if result == nil {
				return atom.NilValue, nil
			}
			return result, nil
		}

		frame := childVM.currentFrame()
		defer func() {
			for i := range childVM.frames {
				childVM.frames[i] = nil
			}
			if frame != nil {
				releaseFrame(frame)
			}
		}()

		if err := childVM.Run(); err != nil {
			return nil, err
		}

		result := childVM.StackTop()
		if result == nil {
			return atom.NilValue, nil
		}
		return result, nil
	})
}

func (m *executionMachine) executeReturn(withValue bool) (bool, error) {
	vm, err := m.requireVM("executeReturn")
	if err != nil {
		return false, err
	}

	var returnValue types.Value
	if withValue {
		returnValue = vm.pop()
	} else {
		returnValue = atom.NilValue
	}

	returnFrame := vm.popFrame()
	vm.recordCompletedFrameProfile(returnFrame)

	if vm.frameIndex == 0 {
		vm.sp = 0
		_ = vm.push(returnValue)
		return true, nil
	}

	vm.sp = returnFrame.basePointer - 1
	err = vm.push(returnValue)
	releaseFrame(returnFrame)
	return false, err
}

func (m *executionMachine) pushClosure(constIndex, numFree int) error {
	vm, err := m.requireVM("pushClosure")
	if err != nil {
		return err
	}

	constant := vm.constants[constIndex]
	function, ok := constant.(*compiler.CompiledFunction)
	if !ok {
		return vm.newRuntimeError("not a function: %+v", constant)
	}

	free := make([]types.Value, numFree)
	for i := 0; i < numFree; i++ {
		free[i] = vm.stack[vm.sp-numFree+i]
	}
	vm.sp = vm.sp - numFree

	closure := &compiler.Closure{Fn: function, Free: free}
	return vm.push(closure)
}
