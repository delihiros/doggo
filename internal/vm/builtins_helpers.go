// builtins_helpers.go provides helper functions for builtin implementations.
// Consolidates argument validation and type conversion utilities.
package vm

import (
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// ============================================================================
// Argument Validation Helpers
// ============================================================================

// CheckArgCount validates that exactly 'want' arguments are provided.
// Returns nil if valid, or an error Value if invalid.
func CheckArgCount(name string, args []types.Value, want int) types.Value {
	if len(args) != want {
		return newArgumentError("%s: wrong number of arguments. got=%d, want=%d", name, len(args), want)
	}
	return nil
}

// CheckArgRange validates that between min and max arguments are provided.
// Returns nil if valid, or an error Value if invalid.
func CheckArgRange(name string, args []types.Value, min, max int) types.Value {
	if len(args) < min || len(args) > max {
		return newArgumentError("%s: wrong number of arguments. got=%d, want=%d-%d", name, len(args), min, max)
	}
	return nil
}

// CheckArgMin validates that at least min arguments are provided.
// Returns nil if valid, or an error Value if invalid.
func CheckArgMin(name string, args []types.Value, min int) types.Value {
	if len(args) < min {
		return newArgumentError("%s: wrong number of arguments. got=%d, want at least %d", name, len(args), min)
	}
	return nil
}

// CheckArgCountCtx validates argument count for CtxFn functions, returning error.
func CheckArgCountCtx(name string, args []types.Value, want int) error {
	if len(args) != want {
		return newArgumentError("%s: wrong number of arguments. got=%d, want=%d", name, len(args), want)
	}
	return nil
}

// CheckArgRangeCtx validates argument range for CtxFn functions, returning error.
func CheckArgRangeCtx(name string, args []types.Value, min, max int) error {
	if len(args) < min || len(args) > max {
		return newArgumentError("%s: wrong number of arguments. got=%d, want=%d-%d", name, len(args), min, max)
	}
	return nil
}

// CheckArgMinCtx validates minimum argument count for CtxFn functions, returning error.
func CheckArgMinCtx(name string, args []types.Value, min int) error {
	if len(args) < min {
		return newArgumentError("%s: wrong number of arguments. got=%d, want at least %d", name, len(args), min)
	}
	return nil
}

// ============================================================================
// Structured Error Constructors (for Fn builtins returning types.Value)
// ============================================================================

// TypeError creates a type error with structured information.
// Used when an argument has an unexpected type.
func TypeError(fnName, expected, got string) *Error {
	return &Error{
		Message:  fmt.Sprintf("%s: expected %s, got %s", fnName, expected, got),
		Kind:     "type",
		Context:  fnName,
		Expected: expected,
		Got:      got,
	}
}

// TypeErrorAt creates a type error for a specific argument position.
// argIdx is 0-based, but displayed as 1-based in the message.
func TypeErrorAt(fnName string, argIdx int, expected, got string) *Error {
	return &Error{
		Message:  fmt.Sprintf("%s: argument %d must be %s, got %s", fnName, argIdx+1, expected, got),
		Kind:     "type",
		Context:  fnName,
		Expected: expected,
		Got:      got,
	}
}

// RuntimeErr creates a runtime error with structured information.
func RuntimeErr(fnName, msg string) *Error {
	return &Error{
		Message: fmt.Sprintf("%s: %s", fnName, msg),
		Kind:    "runtime",
		Context: fnName,
	}
}

// RuntimeErrf creates a formatted runtime error with structured information.
func RuntimeErrf(fnName, format string, args ...any) *Error {
	return &Error{
		Message: fmt.Sprintf("%s: %s", fnName, fmt.Sprintf(format, args...)),
		Kind:    "runtime",
		Context: fnName,
	}
}

// ============================================================================
// Structured Error Constructors (for CtxFn builtins returning error)
// ============================================================================

// NewTypeErrorAt creates a type error for a specific argument (CtxFn).
func NewTypeErrorAt(fnName string, argIdx int, expected, got string) error {
	return &Error{
		Message:  fmt.Sprintf("%s: argument %d: expected %s, got %s", fnName, argIdx+1, expected, got),
		Kind:     "type",
		Context:  fnName,
		Expected: expected,
		Got:      got,
	}
}

// ============================================================================
// Type Conversion Helpers
// ============================================================================

// isNilArg checks if a value is nil (either Go nil or types.Nil).
func isNilArg(v types.Value) bool {
	if v == nil {
		return true
	}
	_, ok := v.(types.Nil)
	return ok
}

// toNumber converts a Value to float64.
// Returns the number and true if successful, or 0 and false if not a number.
func toNumber(v types.Value) (float64, bool) {
	n, ok := v.(types.Number)
	if !ok {
		return 0, false
	}
	return n.ToFloat64(), true
}

// integerForOperation converts any language number to an integer using the
// historical collection/index convention of truncating toward zero.
func integerForOperation(value types.Value) (int64, bool) {
	number, ok := value.(types.Number)
	if !ok {
		return 0, false
	}
	return types.TruncateInt64Value(number)
}

// toSlice materializes a collection without discarding lazy evaluation errors.
func toSlice(v types.Value) ([]types.Value, error) {
	return materializeValue(v)
}

func sequenceEvalError(seq types.Seq) error {
	return coll.RealizeSequenceNode(seq)
}

func materializeSeq(seq types.Seq) ([]types.Value, error) {
	if seq == nil {
		return nil, nil
	}

	result := make([]types.Value, 0)
	for seq != nil {
		if err := sequenceEvalError(seq); err != nil {
			return nil, err
		}
		first := seq.First()
		if first == nil {
			break
		}
		result = append(result, first)
		seq = seq.Next()
	}
	return result, nil
}

func materializeValue(v types.Value) ([]types.Value, error) {
	if v == nil || v == atom.NilValue {
		return nil, nil
	}

	switch c := v.(type) {
	case *coll.Vector:
		return c.Items, nil
	case *coll.List:
		return c.Items, nil
	case *coll.Set:
		return c.AllElements(), nil
	case atom.String:
		pieces := []rune(string(c))
		result := make([]types.Value, len(pieces))
		for i, piece := range pieces {
			result[i] = atom.NewCharacter(piece)
		}
		return result, nil
	case types.Seq:
		return materializeSeq(c)
	case types.Sequable:
		seqVal := c.Seq()
		if seqVal == nil {
			return nil, nil
		}
		if lazyErr, ok := seqVal.(*coll.LazySeqError); ok {
			return nil, lazyErr.Cause()
		}
		seq, ok := seqVal.(types.Seq)
		if !ok {
			return nil, fmt.Errorf("expected a collection, got %s", v.Type())
		}
		return materializeSeq(seq)
	default:
		return nil, fmt.Errorf("expected a collection, got %s", v.Type())
	}
}

func drainSeq(seq types.Seq) error {
	if seq == nil {
		return nil
	}
	for seq != nil {
		if err := sequenceEvalError(seq); err != nil {
			return err
		}
		first := seq.First()
		if first == nil {
			return nil
		}
		seq = seq.Next()
	}
	return nil
}

func drainValue(v types.Value) error {
	if v == nil || v == atom.NilValue {
		return nil
	}

	switch c := v.(type) {
	case *coll.Vector, *coll.List, *coll.Set, atom.String:
		return nil
	case types.Seq:
		return drainSeq(c)
	case types.Sequable:
		seqVal := c.Seq()
		if lazyErr, ok := seqVal.(*coll.LazySeqError); ok {
			return lazyErr.Cause()
		}
		seq, ok := seqVal.(types.Seq)
		if !ok {
			return nil
		}
		return drainSeq(seq)
	default:
		return nil
	}
}

func conjValues(collection types.Value, items ...types.Value) (types.Value, error) {
	result := collection
	for _, item := range items {
		next, err := conjValue(result, item)
		if err != nil {
			return nil, err
		}
		result = next
	}
	return result, nil
}

func conjValue(collection, item types.Value) (types.Value, error) {
	if collection == nil {
		return coll.NewList(item), nil
	}
	if _, ok := collection.(types.Nil); ok {
		return coll.NewList(item), nil
	}

	if m, ok := collection.(types.Map); ok {
		return conjMapValue(m, item)
	}
	if c, ok := collection.(types.Conjable); ok {
		return c.Conj(item), nil
	}

	return nil, fmt.Errorf("conj: unsupported collection type %s", collection.Type())
}

func conjMapValue(m types.Map, item types.Value) (types.Value, error) {
	if entry, ok := item.(*types.MapEntry); ok {
		return m.Assoc(entry.Key, entry.Value), nil
	}

	if vec, ok := item.(*coll.Vector); ok {
		if len(vec.Items) != 2 {
			return nil, fmt.Errorf("conj: vector arg to map conj must be a pair")
		}
		return m.Assoc(vec.Items[0], vec.Items[1]), nil
	}

	seq, err := asSeq(item, "conj")
	if err != nil {
		return nil, err
	}
	if seq == nil {
		return m, nil
	}

	result := m
	for seq != nil {
		if err := sequenceEvalError(seq); err != nil {
			return nil, err
		}
		entry, ok := seq.First().(*types.MapEntry)
		if !ok {
			got := "nil"
			if first := seq.First(); first != nil {
				got = first.Type()
			}
			return nil, fmt.Errorf("conj: seq arg to map conj must contain map entries, got %s", got)
		}
		result = result.Assoc(entry.Key, entry.Value)
		seq = seq.Next()
	}
	return result, nil
}

func asSeqValue(v types.Value, fnName string) (types.Seq, types.Value) {
	seq, err := asSeq(v, fnName)
	if err != nil {
		return nil, newArgumentError("%v", err)
	}
	return seq, nil
}

// asSeq converts a value to a types.Seq for lazy iteration.
// Returns nil for nil/empty values, error for non-sequable types.
// Supports strings by converting them to sequences of single-character strings.
func asSeq(v types.Value, fnName string) (types.Seq, error) {
	if v == nil {
		return nil, nil
	}
	if _, ok := v.(types.Nil); ok {
		return nil, nil
	}

	// Already a Seq - use directly
	if seq, ok := v.(types.Seq); ok {
		if metaSeq, ok := seq.(*coll.MetaSeq); ok {
			return metaSeq.SeqValue, nil
		}
		return seq, nil
	}

	// String - convert to list of characters (consistent with seq/doall)
	if str, ok := v.(atom.String); ok {
		pieces := []rune(string(str))
		if len(pieces) == 0 {
			return nil, nil
		}
		chars := make([]types.Value, 0, len(pieces))
		for _, piece := range pieces {
			chars = append(chars, atom.NewCharacter(piece))
		}
		list := coll.NewList(chars...)
		return list, nil
	}

	// Sequable - convert to Seq
	if sequable, ok := v.(types.Sequable); ok {
		seqVal := sequable.Seq()
		if seqVal == nil {
			return nil, nil
		}
		if seq, ok := seqVal.(types.Seq); ok {
			if metaSeq, ok := seq.(*coll.MetaSeq); ok {
				return metaSeq.SeqValue, nil
			}
			return seq, nil
		}
	}

	return nil, fmt.Errorf("%s: argument must be a collection, got %s", fnName, v.Type())
}
