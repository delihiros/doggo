// builtins_core_ext.go defines extended core builtin functions.
// Provides: zero?, neg?, identity, reverse, concat, some, every?, partial, juxt, not
package vm

import (
	"context"
	"fmt"
	"sync/atomic"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

var builtinsCoreExt = []BuiltinDefinition{
	// not - Logical negation as a function (for use in higher-order functions)
	// Note: (not x) as a special form still works, but (map not coll) needs this
	{
		Name: "not",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("not", args, 1); err != nil {
				return err
			}
			if isTruthy(args[0]) {
				return atom.False
			}
			return atom.True
		}},
	},

	// not= - Returns true if arguments are not equal
	// (not= 1 2) => true
	// (not= 1 1) => false
	// (not= 1 1 2) => true (not all equal)
	{
		Name: "not=",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("not=", args, 1); err != nil {
				return err
			}
			if len(args) == 1 {
				return atom.False // Single argument is equal to itself
			}
			// Check if all arguments are equal
			first := args[0]
			for i := 1; i < len(args); i++ {
				equal, err := coll.ValueEqualsResult(first, args[i])
				if err != nil {
					return newArgumentError("%v", err)
				}
				if !equal {
					return atom.True // Found inequality
				}
			}
			return atom.False // All equal
		}},
	},

	// zero? - Check if number is zero
	{
		Name: "zero?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("zero?", args, 1); err != nil {
				return err
			}
			num, ok := args[0].(types.Number)
			if !ok {
				return newArgumentError("zero?: argument must be a number, got %s", args[0].Type())
			}
			return atom.Boolean(num.ToFloat64() == 0)
		}},
	},

	// neg? - Check if number is negative
	{
		Name: "neg?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("neg?", args, 1); err != nil {
				return err
			}
			num, ok := args[0].(types.Number)
			if !ok {
				return newArgumentError("neg?: argument must be a number, got %s", args[0].Type())
			}
			return atom.Boolean(num.ToFloat64() < 0)
		}},
	},

	// identity - Return argument unchanged
	{
		Name: "identity",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("identity", args, 1); err != nil {
				return err
			}
			return args[0]
		}},
	},

	// reverse - Reverse a collection, returns a list
	// Note: reverse is inherently eager (must see all elements to reverse)
	{
		Name: "reverse",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("reverse", args, 1); err != nil {
				return err
			}
			if args[0] == nil {
				return coll.NewList()
			}
			if _, ok := args[0].(types.Nil); ok {
				return coll.NewList()
			}

			// Use asSeq to get proper type checking
			seq, err := asSeq(args[0], "reverse")
			if err != nil {
				return newArgumentError("%s", err.Error())
			}
			if seq == nil {
				return coll.NewList()
			}

			items, err := materializeSeq(seq)
			if err != nil {
				return newArgumentError("%v", err)
			}

			if len(items) == 0 {
				return coll.NewList()
			}

			// Reverse the collected items
			reversed := make([]types.Value, len(items))
			for i, item := range items {
				reversed[len(items)-1-i] = item
			}
			return coll.NewList(reversed...)
		}},
	},

	// rseq - Returns a reversed list of a vector in linear time
	// (rseq [1 2 3]) => (3 2 1)
	// (rseq []) => nil
	{
		Name: "rseq",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("rseq", args, 1); err != nil {
				return err
			}
			vec, ok := args[0].(*coll.Vector)
			if !ok {
				return newArgumentError("rseq: argument must be a vector, got %s", args[0].Type())
			}
			if len(vec.Items) == 0 {
				return types.Nil{}
			}
			// Build a list in reverse order
			reversed := make([]types.Value, len(vec.Items))
			for i, item := range vec.Items {
				reversed[len(vec.Items)-1-i] = item
			}
			return coll.NewList(reversed...)
		}},
	},

	// concat - Concatenate collections, returns a lazy sequence
	// Lazily concatenates the supplied collections
	{
		Name: "concat",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if len(args) == 0 {
				return coll.NewList(), nil
			}

			// Filter out nil arguments and validate types upfront
			var colls []types.Value
			for i, arg := range args {
				if arg == nil {
					continue
				}
				if _, ok := arg.(types.Nil); ok {
					continue
				}
				// Validate that argument is sequable (type check)
				// Accept: Seq, Sequable, or String
				if _, ok := arg.(types.Seq); !ok {
					if _, ok := arg.(types.Sequable); !ok {
						if _, ok := arg.(atom.String); !ok {
							return nil, newArgumentError("concat: argument %d must be a collection, got %s", i+1, arg.Type())
						}
					}
				}
				colls = append(colls, arg)
			}

			if len(colls) == 0 {
				return coll.NewList(), nil
			}

			// Create a lazy sequence that concatenates all collections
			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("concat: VM not available in context")
			}

			return newConcatLazySeq(ctx, vm, colls, 0, nil), nil
		}},
	},

	// some - Return first truthy predicate result, or nil
	// Uses lazy iteration for short-circuit evaluation
	{
		Name: "some",
		Builtin: &Builtin{CtxFn: predicateSeqCtxFn("some", atom.NilValue, func(result types.Value) (types.Value, bool) {
			if isTruthy(result) {
				return result, true
			}
			return nil, false
		})},
	},

	// every? - Check if all elements satisfy predicate
	// Uses lazy iteration for short-circuit evaluation
	{
		Name: "every?",
		Builtin: &Builtin{CtxFn: predicateSeqCtxFn("every?", atom.True, func(result types.Value) (types.Value, bool) {
			if !isTruthy(result) {
				return atom.False, true
			}
			return nil, false
		})},
	},

	// not-any? - Returns true if no element satisfies pred
	// (not-any? pred coll) => (not (some pred coll))
	{
		Name: "not-any?",
		Builtin: &Builtin{CtxFn: predicateSeqCtxFn("not-any?", atom.True, func(result types.Value) (types.Value, bool) {
			if isTruthy(result) {
				return atom.False, true
			}
			return nil, false
		})},
	},

	// not-every? - Returns true if any element fails pred
	// (not-every? pred coll) => (not (every? pred coll))
	{
		Name: "not-every?",
		Builtin: &Builtin{CtxFn: predicateSeqCtxFn("not-every?", atom.False, func(result types.Value) (types.Value, bool) {
			if !isTruthy(result) {
				return atom.True, true
			}
			return nil, false
		})},
	},

	// partial - Partial function application with no argument limit
	{
		Name: "partial",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("partial", args, 1); err != nil {
				return nil, err
			}

			fn := args[0]
			boundArgs := make([]types.Value, len(args)-1)
			copy(boundArgs, args[1:])

			// Return a closure that prepends boundArgs to any new args
			partialFn := &Builtin{
				Name: "partial-fn",
				CtxFn: func(callCtx context.Context, callArgs ...types.Value) (types.Value, error) {
					vm := GetVM(callCtx)
					if vm == nil {
						return nil, fmt.Errorf("partial function: VM not available in context")
					}

					// Combine bound args with call args
					allArgs := make([]types.Value, 0, len(boundArgs)+len(callArgs))
					allArgs = append(allArgs, boundArgs...)
					allArgs = append(allArgs, callArgs...)

					return applyWithVM(callCtx, vm, fn, allArgs)
				},
			}

			return partialFn, nil
		}},
	},

	// juxt - Apply multiple functions to the same arguments
	{
		Name: "juxt",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("juxt", args, 1); err != nil {
				return nil, err
			}

			fns := make([]types.Value, len(args))
			copy(fns, args)

			// Return a function that applies all fns to its args and returns a vector
			juxtFn := &Builtin{
				Name: "juxt-fn",
				CtxFn: func(callCtx context.Context, callArgs ...types.Value) (types.Value, error) {
					vm := GetVM(callCtx)
					if vm == nil {
						return nil, fmt.Errorf("juxt function: VM not available in context")
					}

					results := make([]types.Value, len(fns))
					for i, fn := range fns {
						result, err := applyWithVM(callCtx, vm, fn, callArgs)
						if err != nil {
							return nil, err
						}
						results[i] = result
					}

					return coll.NewVector(results...), nil
				},
			}

			return juxtFn, nil
		}},
	},

	// some-fn - Returns a function that applies predicates with OR logic
	// ((some-fn even? neg?) 2) => true
	// ((some-fn even? neg?) 3) => false
	// ((some-fn even? neg?) -3) => true
	{
		Name: "some-fn",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("some-fn", args, 1); err != nil {
				return nil, err
			}

			preds := make([]types.Value, len(args))
			copy(preds, args)

			// Return a function that applies predicates with OR logic
			someFn := &Builtin{
				Name: "some-fn-result",
				CtxFn: func(callCtx context.Context, callArgs ...types.Value) (types.Value, error) {
					vm := GetVM(callCtx)
					if vm == nil {
						return nil, fmt.Errorf("some-fn function: VM not available in context")
					}

					for _, pred := range preds {
						result, err := applyWithVM(callCtx, vm, pred, callArgs)
						if err != nil {
							return nil, err
						}
						if isTruthy(result) {
							return result, nil // Return first truthy result
						}
					}

					return types.Nil{}, nil // No predicate succeeded
				},
			}

			return someFn, nil
		}},
	},

	// every-pred - Returns a function that applies predicates with AND logic
	// ((every-pred even? pos?) 2) => true
	// ((every-pred even? pos?) -2) => false
	// ((every-pred even? pos?) 3) => false
	{
		Name: "every-pred",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("every-pred", args, 1); err != nil {
				return nil, err
			}

			preds := make([]types.Value, len(args))
			copy(preds, args)

			// Return a function that applies predicates with AND logic
			everyPredFn := &Builtin{
				Name: "every-pred-result",
				CtxFn: func(callCtx context.Context, callArgs ...types.Value) (types.Value, error) {
					vm := GetVM(callCtx)
					if vm == nil {
						return nil, fmt.Errorf("every-pred function: VM not available in context")
					}

					for _, pred := range preds {
						result, err := applyWithVM(callCtx, vm, pred, callArgs)
						if err != nil {
							return nil, err
						}
						if !isTruthy(result) {
							return atom.False, nil // Return false on first failure
						}
					}

					return atom.True, nil // All predicates succeeded
				},
			}

			return everyPredFn, nil
		}},
	},
}

func predicateSeqCtxFn(fnName string, emptyResult types.Value, shortCircuit func(types.Value) (types.Value, bool)) func(context.Context, ...types.Value) (types.Value, error) {
	return func(ctx context.Context, args ...types.Value) (types.Value, error) {
		if err := CheckArgCountCtx(fnName, args, 2); err != nil {
			return nil, err
		}

		vm := GetVM(ctx)
		if vm == nil {
			return nil, fmt.Errorf("%s: VM not available in context", fnName)
		}

		pred := args[0]
		collArg := args[1]
		if collArg == nil {
			return emptyResult, nil
		}
		if _, ok := collArg.(types.Nil); ok {
			return emptyResult, nil
		}

		seq, err := asSeq(collArg, fnName)
		if err != nil {
			return nil, err
		}
		if seq == nil {
			return emptyResult, nil
		}

		for seq != nil {
			if err := sequenceEvalError(seq); err != nil {
				return nil, err
			}
			item := seq.First()
			if item == nil {
				break
			}

			result, err := ApplyCallableFast(ctx, vm, pred, []types.Value{item})
			if err != nil {
				return nil, err
			}
			if value, done := shortCircuit(result); done {
				return value, nil
			}

			seq = seq.Next()
		}

		return emptyResult, nil
	}
}

// concatLazySeq implements a lazy sequence that concatenates multiple collections
type concatLazySeq struct {
	ctx        context.Context
	vm         *VM
	colls      []types.Value // remaining collections to concat
	collIdx    int           // current collection index
	currentSeq types.Seq     // current sequence being iterated
	sourceIdx  int           // immutable collection index used by Sources
	sourceSeq  types.Seq     // immutable current sequence used by Sources
	first      types.Value   // cached first value
	rest       types.Seq     // cached rest sequence
	err        error         // lazy evaluation error from the current collection
	once       util.RecursionDetectingOnce
	realized   atomic.Bool
}

func newConcatLazySeq(ctx context.Context, vm *VM, colls []types.Value, idx int, currentSeq types.Seq) *concatLazySeq {
	return &concatLazySeq{
		ctx:        ctx,
		vm:         vm,
		colls:      colls,
		collIdx:    idx,
		currentSeq: currentSeq,
		sourceIdx:  idx,
		sourceSeq:  currentSeq,
	}
}

func (cs *concatLazySeq) realize() error {
	return cs.once.Do(func() {
		defer cs.realized.Store(true)
		cs.realizeOnce()
	})
}

func (cs *concatLazySeq) realizeOnce() {
	// If we have a current sequence, use it
	if cs.currentSeq != nil {
		if err := sequenceEvalError(cs.currentSeq); err != nil {
			cs.err = err
			return
		}
		cs.first = cs.currentSeq.First()
		if cs.first != nil {
			nextSeq := cs.currentSeq.Next()
			if nextSeq != nil {
				cs.rest = newConcatLazySeq(cs.ctx, cs.vm, cs.colls, cs.collIdx, nextSeq)
			} else {
				// Current sequence exhausted, move to next collection
				cs.rest = newConcatLazySeq(cs.ctx, cs.vm, cs.colls, cs.collIdx+1, nil)
			}
			return
		}
		// Current sequence is empty, move to next collection
		cs.collIdx++
		cs.currentSeq = nil
	}

	// Find the next non-empty collection
	for cs.collIdx < len(cs.colls) {
		coll := cs.colls[cs.collIdx]
		seq, err := asSeq(coll, "concat")
		if err != nil {
			cs.err = err
			return
		}
		if seq == nil {
			cs.collIdx++
			continue
		}
		if err := sequenceEvalError(seq); err != nil {
			cs.err = err
			return
		}

		cs.first = seq.First()
		if cs.first != nil {
			nextSeq := seq.Next()
			if nextSeq != nil {
				cs.rest = newConcatLazySeq(cs.ctx, cs.vm, cs.colls, cs.collIdx, nextSeq)
			} else {
				cs.rest = newConcatLazySeq(cs.ctx, cs.vm, cs.colls, cs.collIdx+1, nil)
			}
			return
		}
		cs.collIdx++
	}

	// All collections exhausted
	cs.first = nil
	cs.rest = nil
}

func (cs *concatLazySeq) First() types.Value {
	if cs.realize() != nil {
		return nil
	}
	return cs.first
}

func (cs *concatLazySeq) Next() types.Seq {
	if cs.realize() != nil {
		return nil
	}
	if cs.first == nil {
		return nil
	}
	if cs.rest == nil {
		return nil
	}
	// Check if rest is empty
	if restConcat, ok := cs.rest.(*concatLazySeq); ok {
		if err := restConcat.realize(); err != nil {
			return restConcat
		}
		if restConcat.err != nil {
			return restConcat
		}
		if restConcat.first == nil {
			return nil
		}
	}
	return cs.rest
}

func (cs *concatLazySeq) More() types.Seq {
	n := cs.Next()
	if n == nil {
		return coll.NewList()
	}
	return n
}

func (cs *concatLazySeq) Seq() types.Value {
	if err := cs.realize(); err != nil {
		return &coll.LazySeqError{Err: err}
	}
	if cs.err != nil {
		return &coll.LazySeqError{Err: cs.err}
	}
	if cs.first == nil {
		return nil
	}
	return cs
}

func (cs *concatLazySeq) Type() string           { return "lazy-seq" }
func (cs *concatLazySeq) IsSelfEvaluating() bool { return false }
func (cs *concatLazySeq) String() string {
	return coll.SeqString(cs)
}

func (cs *concatLazySeq) Equals(other types.Value) bool {
	return coll.SequentialEquals(cs, other)
}
func (cs *concatLazySeq) Hash() uint32 { return coll.SeqHash(cs) }

func (cs *concatLazySeq) Sources() []types.Seq {
	if cs == nil {
		return nil
	}
	sources := make([]types.Seq, 0, len(cs.colls)-cs.sourceIdx+1)
	if cs.sourceSeq != nil {
		sources = append(sources, cs.sourceSeq)
	}
	for i := cs.sourceIdx; i < len(cs.colls); i++ {
		if source, ok := cs.colls[i].(types.Seq); ok {
			sources = append(sources, source)
		}
	}
	return sources
}

func (cs *concatLazySeq) PreservesInfiniteLength() bool { return true }

// Realized returns whether the first element has been computed
func (cs *concatLazySeq) Realized() bool {
	return cs != nil && cs.realized.Load()
}

// Ensure concatLazySeq implements types.Seq, types.Value, and types.LazySeq
var _ types.Seq = (*concatLazySeq)(nil)
var _ types.Value = (*concatLazySeq)(nil)
var _ types.LazySeq = (*concatLazySeq)(nil)
