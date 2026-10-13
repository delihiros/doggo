package vm

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/util"
)

type futureValue struct {
	done  chan struct{}
	value types.Value
	err   error
}

func newFuture(ctx context.Context, runtime CallableRuntime, fn types.Value) *futureValue {
	future := &futureValue{done: make(chan struct{})}
	ctx = forkBindingContext(ctx)
	go func() {
		future.value, future.err = runtime.Apply(ctx, fn, nil)
		if future.value == nil && future.err == nil {
			future.value = atom.NilValue
		}
		close(future.done)
	}()
	return future
}

func (f *futureValue) Deref(ctx context.Context) (types.Value, error) {
	select {
	case <-f.done:
		return f.value, f.err
	default:
	}
	select {
	case <-f.done:
		return f.value, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *futureValue) Realized() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

func (f *futureValue) String() string                { return "#<future>" }
func (f *futureValue) Type() string                  { return "future" }
func (f *futureValue) Hash() uint32                  { return util.HashString(fmt.Sprintf("%p", f)) }
func (f *futureValue) Equals(other types.Value) bool { return f == other }
func (f *futureValue) IsSelfEvaluating() bool        { return true }
