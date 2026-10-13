package vm

import (
	"context"
	"sync"

	"doggo/internal/compiler"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

// vmMappedSeqState owns the source cursor and every mapped value shared by
// the lightweight VMMappedSeq views.
type vmMappedSeqState struct {
	ctx     context.Context
	runtime CallableRuntime
	mapFn   types.Value
	pattern *ClosurePattern

	mu         sync.Mutex
	cond       *sync.Cond
	source     types.Seq
	values     []types.Value
	sourceDone bool
	err        error
	errIndex   int
	realizing  bool
	trackOwner bool
	ownerGoID  uint64
}

func newVMMappedSeqState(
	ctx context.Context,
	runtime CallableRuntime,
	constants []types.Value,
	source types.Seq,
	mapFn types.Value,
) *vmMappedSeqState {
	state := &vmMappedSeqState{
		ctx:      ctx,
		runtime:  runtime,
		mapFn:    mapFn,
		source:   source,
		errIndex: -1,
	}
	if closure, ok := mapFn.(*compiler.Closure); ok {
		state.pattern = AnalyzeClosurePattern(closure, constants)
	}
	state.trackOwner = mappedRealizationCanReenter(source, mapFn, state.pattern)
	state.cond = sync.NewCond(&state.mu)
	return state
}

func mappedRealizationCanReenter(source types.Seq, mapFn types.Value, pattern *ClosurePattern) bool {
	switch source := source.(type) {
	case *coll.RangeLazySeq, *coll.InfiniteRangeLazySeq:
		// These generators do not invoke Doggo code while advancing.
	case *VMMappedSeq:
		if source == nil || source.state == nil || source.state.trackOwner {
			return true
		}
	default:
		if _, lazy := source.(types.LazySeq); lazy {
			return true
		}
	}

	if pattern != nil {
		return false
	}
	if builtin, ok := mapFn.(*Builtin); ok && builtin.Fn != nil {
		return false
	}
	if closure, ok := mapFn.(*compiler.Closure); ok {
		return closureMayReenterLazySequence(closure)
	}
	return true
}

func closureMayReenterLazySequence(closure *compiler.Closure) bool {
	if closure == nil || closure.Fn == nil || len(closure.Free) > 0 {
		return true
	}
	ins := closure.Fn.Instructions
	for offset := 0; offset < len(ins); {
		op := opcode.Opcode(ins[offset])
		switch op {
		case opcode.OpConst, opcode.OpPop, opcode.OpDup,
			opcode.OpTrue, opcode.OpFalse, opcode.OpNil,
			opcode.OpAdd, opcode.OpSub, opcode.OpMul, opcode.OpDiv, opcode.OpMod, opcode.OpNeg,
			opcode.OpGT, opcode.OpGTE, opcode.OpLT, opcode.OpLTE, opcode.OpNot,
			opcode.OpJump, opcode.OpJumpNotTruthy, opcode.OpJumpTruthy,
			opcode.OpGetLocal, opcode.OpSetLocal,
			opcode.OpReturnValue, opcode.OpReturn:
		default:
			return true
		}
		definition, err := opcode.Lookup(op)
		if err != nil {
			return true
		}
		width := 1
		for _, operandWidth := range definition.OperandWidths {
			width += operandWidth
		}
		if offset+width > len(ins) {
			return true
		}
		offset += width
	}
	return false
}

func (state *vmMappedSeqState) valueAt(index int) (types.Value, bool, error) {
	if state == nil || index < 0 {
		return nil, false, nil
	}

	var callerGoID uint64
	if state.trackOwner {
		var err error
		callerGoID, err = util.CurrentGoroutineID()
		if err != nil {
			return nil, false, &RuntimeError{Message: err.Error()}
		}
	}

	state.mu.Lock()
	for {
		if index < len(state.values) {
			value := state.values[index]
			state.mu.Unlock()
			return value, true, nil
		}
		if state.err != nil && index >= state.errIndex {
			err := state.err
			state.mu.Unlock()
			return nil, false, err
		}
		if state.sourceDone {
			state.mu.Unlock()
			return nil, false, nil
		}

		if state.realizing {
			if state.trackOwner && state.ownerGoID == callerGoID {
				state.mu.Unlock()
				return nil, false, &RuntimeError{Message: "recursive mapped sequence realization"}
			}
			state.cond.Wait()
			continue
		}

		state.realizing = true
		state.ownerGoID = callerGoID
		state.mu.Unlock()

		func() {
			defer state.finishRealization()
			state.realizeNext()
		}()

		state.mu.Lock()
	}
}

func (state *vmMappedSeqState) finishRealization() {
	state.mu.Lock()
	state.realizing = false
	state.ownerGoID = 0
	state.cond.Broadcast()
	state.mu.Unlock()
}

func (state *vmMappedSeqState) realizeNext() {
	state.mu.Lock()
	source := state.source
	index := len(state.values)
	state.mu.Unlock()

	if source == nil {
		state.publishExhausted()
		return
	}
	if err := sequenceEvalError(source); err != nil {
		state.publishError(index, err)
		return
	}

	first := source.First()
	if err := sequenceEvalError(source); err != nil {
		state.publishError(index, err)
		return
	}
	if first == nil {
		state.publishExhausted()
		return
	}

	mapped, err := state.applyMapFn(first)
	if err != nil {
		state.publishError(index, err)
		return
	}
	next := source.Next()

	state.mu.Lock()
	state.values = append(state.values, mapped)
	state.source = next
	if next == nil {
		state.sourceDone = true
	}
	state.mu.Unlock()
}

func (state *vmMappedSeqState) applyMapFn(value types.Value) (types.Value, error) {
	if state.pattern != nil {
		if mapped := state.pattern.Apply(value); mapped != nil {
			return mapped, nil
		}
	}
	if result, ok := DirectApply(state.mapFn, []types.Value{value}); ok {
		return result, nil
	}
	if state.runtime == nil {
		return nil, &RuntimeError{Message: "callable runtime not initialized"}
	}
	return state.runtime.ApplyFast(state.ctx, state.mapFn, []types.Value{value})
}

func (state *vmMappedSeqState) publishError(index int, err error) {
	state.mu.Lock()
	if state.err == nil {
		state.err = err
		state.errIndex = index
		state.source = nil
		state.sourceDone = true
	}
	state.mu.Unlock()
}

func (state *vmMappedSeqState) publishExhausted() {
	state.mu.Lock()
	state.source = nil
	state.sourceDone = true
	state.mu.Unlock()
}

func (state *vmMappedSeqState) hasNextAfter(index int) bool {
	state.mu.Lock()
	defer state.mu.Unlock()

	nextIndex := index + 1
	if nextIndex < len(state.values) {
		return true
	}
	if state.err != nil && nextIndex >= state.errIndex {
		return true
	}
	return !state.sourceDone
}

func (state *vmMappedSeqState) realizedAt(index int) bool {
	state.mu.Lock()
	defer state.mu.Unlock()

	if index < len(state.values) {
		return true
	}
	if state.err != nil && index >= state.errIndex {
		return true
	}
	return state.sourceDone
}
