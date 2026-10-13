package schema

import (
	"context"
	"fmt"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

type InstrumentContract struct {
	Args *Schema
	Ret  *Schema
}

type InstrumentOptions struct {
	Target types.Value
}

type InstrumentError struct {
	Contract InstrumentContract
	Target   types.Value
	Actual   types.Value
	Problems []Problem
	Reason   string
	Message  string
	Cause    error
}

func (e *InstrumentError) Error() string {
	if e == nil {
		return "schema instrumentation failed"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Reason != "" {
		return e.Reason
	}
	return "schema instrumentation failed"
}

func (e *InstrumentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *InstrumentError) ExceptionData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}

	data := map[string]any{
		"contract": instrumentContractData(e.Contract),
	}
	if e.Target != nil {
		data["target"] = e.Target
	}
	if e.Actual != nil {
		data["actual"] = e.Actual
	}
	if len(e.Problems) > 0 {
		data["problems"] = instrumentProblemsData(e.Problems)
	}
	if e.Reason != "" {
		data["reason"] = e.Reason
	}

	message := e.Error()
	if message == "" {
		message = "schema instrumentation failed"
	}

	return schemaExceptionData("SchemaInstrumentError", "schema instrumentation error", message, data, e.Cause)
}

func InstrumentCallable(fn types.Fn, contract InstrumentContract, opts InstrumentOptions) (types.Fn, error) {
	if fn == nil {
		return nil, &InstrumentError{
			Contract: contract,
			Target:   opts.Target,
			Reason:   "malformed instrumentation contract",
			Message:  "instrument target must be callable",
		}
	}
	if err := validateInstrumentContract(contract, opts); err != nil {
		return nil, err
	}
	return &instrumentedFn{original: fn, contract: contract, opts: opts}, nil
}

func UnstrumentCallable(value types.Value) types.Value {
	if wrapped, ok := value.(*instrumentedFn); ok {
		return wrapped.original
	}
	return value
}

type instrumentedFn struct {
	original types.Fn
	contract InstrumentContract
	opts     InstrumentOptions
}

func (f *instrumentedFn) String() string {
	if f == nil || f.original == nil {
		return "#<instrumented-function>"
	}
	return f.original.String()
}

func (f *instrumentedFn) Type() string {
	return "instrumented-function"
}

func (f *instrumentedFn) Hash() uint32 {
	if f == nil {
		return 0
	}
	return util.HashString(fmt.Sprintf("%p", f))
}

func (f *instrumentedFn) Equals(other types.Value) bool {
	otherFn, ok := other.(*instrumentedFn)
	return ok && f == otherFn
}

func (f *instrumentedFn) IsSelfEvaluating() bool { return true }

func (f *instrumentedFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if f == nil || f.original == nil {
		return nil, &InstrumentError{
			Contract: f.contract,
			Target:   f.opts.Target,
			Reason:   "instrumentation target missing",
			Message:  "instrumented callable has no target",
		}
	}

	if f.contract.Args != nil {
		argVector := vectorFromValues(args)
		if explanation := ExplainWithContext(ctx, f.contract.Args, argVector); !explanation.Valid {
			return nil, &InstrumentError{
				Contract: f.contract,
				Target:   f.opts.Target,
				Actual:   argVector,
				Problems: explanation.Problems,
				Reason:   "instrumented args do not satisfy contract",
				Message:  "instrumented args do not satisfy contract",
			}
		}
	}

	result, err := f.original.Invoke(ctx, args...)
	if err != nil {
		return nil, err
	}

	if f.contract.Ret != nil {
		if explanation := ExplainWithContext(ctx, f.contract.Ret, result); !explanation.Valid {
			return nil, &InstrumentError{
				Contract: f.contract,
				Target:   f.opts.Target,
				Actual:   result,
				Problems: explanation.Problems,
				Reason:   "instrumented return value does not satisfy contract",
				Message:  "instrumented return value does not satisfy contract",
			}
		}
	}

	return result, nil
}

func validateInstrumentContract(contract InstrumentContract, opts InstrumentOptions) error {
	if contract.Args == nil && contract.Ret == nil {
		return &InstrumentError{
			Contract: contract,
			Target:   opts.Target,
			Reason:   "malformed instrumentation contract",
			Message:  "instrument contract must include :args or :ret",
		}
	}
	if contract.Args != nil && contract.Args.Kind() != KindTuple {
		return &InstrumentError{
			Contract: contract,
			Target:   opts.Target,
			Reason:   "malformed instrumentation contract",
			Message:  "instrument contract :args must be a :tuple schema",
		}
	}
	return nil
}

func instrumentContractData(contract InstrumentContract) map[string]any {
	data := map[string]any{}
	if contract.Args != nil {
		data["args"] = contract.Args.Raw()
	}
	if contract.Ret != nil {
		data["ret"] = contract.Ret.Raw()
	}
	return data
}

func instrumentProblemsData(problems []Problem) []any {
	if len(problems) == 0 {
		return nil
	}
	out := make([]any, 0, len(problems))
	for _, problem := range problems {
		entry := map[string]any{
			"path":     pathData(problem.Path),
			"expected": problem.Expected,
			"actual":   problem.Actual,
			"message":  problem.Message,
		}
		if problem.Predicate != nil {
			entry["predicate"] = problem.Predicate
		}
		if problem.Branch != nil {
			entry["branch"] = *problem.Branch
		}
		out = append(out, entry)
	}
	return out
}

func vectorFromValues(values []types.Value) types.Value {
	if len(values) == 0 {
		return coll.NewVector()
	}
	return coll.NewVector(values...)
}
