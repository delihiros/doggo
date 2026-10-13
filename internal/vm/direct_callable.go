// Package vm - DirectCallable interface for fast function application
// Phase 10b: direct apply fast path
package vm

import (
	"context"

	"doggo/internal/types"
)

// DirectCallable is an interface for functions that can be called directly
// without going through the full VM dispatch. This provides significant
// performance improvements for HOF operations like map, filter, and reduce.
type DirectCallable interface {
	// DirectApply applies the function to the given arguments directly.
	// Returns the result and any error that occurred.
	// If the function cannot be called directly (e.g., requires context),
	// it should return (nil, false) to indicate fallback is needed.
	DirectApply(args []types.Value) (types.Value, bool)
}

// DirectApply attempts to call a function using the DirectCallable interface.
// If the function implements DirectCallable and can handle the call, it uses the fast path.
// Otherwise, it returns nil, false to indicate fallback is needed.
func DirectApply(fn types.Value, args []types.Value) (types.Value, bool) {
	if dc, ok := fn.(DirectCallable); ok {
		return dc.DirectApply(args)
	}
	return nil, false
}

// ApplyCallableFast attempts to use DirectApply first, then falls back to callable runtime dispatch.
// This is the recommended way to apply callables in HOF implementations.
func ApplyCallableFast(ctx context.Context, vm *VM, callable types.Value, args []types.Value) (types.Value, error) {
	runtime := vm.callableRuntime
	if runtime == nil {
		return nil, vm.newRuntimeError("callable runtime not initialized")
	}
	return runtime.ApplyFast(ctx, callable, args)
}
