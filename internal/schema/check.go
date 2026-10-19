package schema

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"time"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

const (
	defaultCheckIterations  = 100
	defaultCheckShrinkLimit = 100
)

type CheckOptions struct {
	Seed               int64
	SeedSet            bool
	Iterations         int
	IterationsSet      bool
	Size               int
	GeneratorOverrides map[Kind]any
	ShrinkLimit        int
	ShrinkLimitSet     bool
}

type CheckFailure struct {
	Kind      atom.Keyword
	Message   string
	Value     types.Value
	Exception *doggoerr.ExceptionData
}

type CheckReport struct {
	Pass         bool
	Iterations   int
	Seed         int64
	FailingInput types.Value
	ShrunkInput  types.Value
	ShrinkSteps  []types.Value
	Failure      *CheckFailure
	Explain      *Explanation
}

type CheckError struct {
	Schema      *Schema
	OptionPath  Path
	OptionValue any
	Reason      string
	Message     string
	Cause       error
}

func (e *CheckError) Error() string {
	if e == nil {
		return "schema property check failed"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Reason != "" {
		return e.Reason
	}
	return "schema property check failed"
}

func (e *CheckError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *CheckError) ExceptionData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}

	data := map[string]any{}
	if e.Schema != nil {
		data["schema_form"] = e.Schema.Raw()
		data["schema_kind"] = string(e.Schema.Kind())
	}
	if len(e.OptionPath) > 0 {
		data["option_path"] = pathData(e.OptionPath)
	}
	if e.OptionValue != nil {
		data["option_value"] = e.OptionValue
	}
	if e.Reason != "" {
		data["reason"] = e.Reason
	}

	return schemaExceptionData("SchemaCheckError", "schema property check error", e.Error(), data, e.Cause)
}

type normalizedCheckOptions struct {
	seed         int64
	iterations   int
	size         int
	shrinkLimit  int
	genOverrides map[Kind]any
}

func Check(ctx context.Context, property types.Fn, schema *Schema, opts CheckOptions) (*CheckReport, error) {
	if isNilFn(property) {
		return nil, &CheckError{
			Schema:  schema,
			Reason:  "non-callable property target",
			Message: "property target must be callable",
		}
	}

	norm, err := normalizeCheckOptions(schema, opts)
	if err != nil {
		return nil, err
	}

	rng := rand.New(rand.NewSource(norm.seed))
	genOpts := GenerateOptions{
		Seed:               norm.seed,
		Size:               norm.size,
		GeneratorOverrides: norm.genOverrides,
	}

	for iteration := 1; iteration <= norm.iterations; iteration++ {
		input, args, err := generateCheckInput(ctx, schema, rng, genOpts)
		if err != nil {
			return nil, err
		}

		failure, failed := evaluatePropertyFailure(ctx, property, args)
		if !failed {
			continue
		}

		shrunkInput := input
		shrinkSteps := []types.Value{}
		finalFailure := failure

		if shouldAttemptShrink(schema, input, norm.shrinkLimit) {
			shrunkInput, shrinkSteps, finalFailure, err = shrinkCounterexample(ctx, property, schema, input, failure, norm.shrinkLimit)
			if err != nil {
				return nil, err
			}
		}

		return &CheckReport{
			Pass:         false,
			Iterations:   iteration,
			Seed:         norm.seed,
			FailingInput: input,
			ShrunkInput:  shrunkInput,
			ShrinkSteps:  shrinkSteps,
			Failure:      finalFailure,
			Explain:      nil,
		}, nil
	}

	return &CheckReport{
		Pass:         true,
		Iterations:   norm.iterations,
		Seed:         norm.seed,
		FailingInput: nil,
		ShrunkInput:  nil,
		ShrinkSteps:  []types.Value{},
		Failure:      nil,
		Explain:      nil,
	}, nil
}

func normalizeCheckOptions(schema *Schema, opts CheckOptions) (normalizedCheckOptions, error) {
	iterations := defaultCheckIterations
	if opts.IterationsSet {
		if opts.Iterations < 0 {
			return normalizedCheckOptions{}, &CheckError{
				Schema:      schema,
				OptionPath:  Path{atom.NewKeyword("iterations")},
				OptionValue: opts.Iterations,
				Reason:      "malformed iteration count",
				Message:     fmt.Sprintf("iteration count must be non-negative, got %d", opts.Iterations),
			}
		}
		iterations = opts.Iterations
	}

	shrinkLimit := defaultCheckShrinkLimit
	if opts.ShrinkLimitSet {
		if opts.ShrinkLimit < 0 {
			return normalizedCheckOptions{}, &CheckError{
				Schema:      schema,
				OptionPath:  Path{atom.NewKeyword("shrink-limit")},
				OptionValue: opts.ShrinkLimit,
				Reason:      "malformed shrink limit",
				Message:     fmt.Sprintf("shrink limit must be non-negative, got %d", opts.ShrinkLimit),
			}
		}
		shrinkLimit = opts.ShrinkLimit
	}

	seed := time.Now().UnixNano()
	if opts.SeedSet {
		seed = opts.Seed
	}

	return normalizedCheckOptions{
		seed:         seed,
		iterations:   iterations,
		size:         boundedGenerateSize(opts.Size),
		shrinkLimit:  shrinkLimit,
		genOverrides: opts.GeneratorOverrides,
	}, validateCheckGeneratorOverrides(schema, opts.GeneratorOverrides)
}

func generateCheckInput(ctx context.Context, schema *Schema, rng *rand.Rand, opts GenerateOptions) (types.Value, []types.Value, error) {
	value, err := generateWithRNG(ctx, schema, rng, normalizeGenerateOptions(opts), nil)
	if err != nil {
		return nil, nil, err
	}
	return value, inputArgsForSchema(schema, value), nil
}

func inputArgsForSchema(schema *Schema, value types.Value) []types.Value {
	if schema != nil && schema.Kind() == KindTuple {
		items, ok := seqLikeItems(value)
		if !ok {
			return []types.Value{}
		}
		return items
	}
	return []types.Value{value}
}

func evaluatePropertyFailure(ctx context.Context, property types.Fn, args []types.Value) (*CheckFailure, bool) {
	result, err := property.Invoke(ctx, args...)
	if err != nil {
		return &CheckFailure{
			Kind:      atom.NewKeyword("threw"),
			Message:   err.Error(),
			Value:     nil,
			Exception: doggoerr.NormalizeExceptionData(err),
		}, true
	}
	if result == nil || result.Type() == "nil" {
		return &CheckFailure{
			Kind:      atom.NewKeyword("return-nil"),
			Message:   "property returned nil",
			Value:     result,
			Exception: nil,
		}, true
	}
	if !types.IsTruthy(result) {
		return &CheckFailure{
			Kind:      atom.NewKeyword("return-false"),
			Message:   "property returned false",
			Value:     result,
			Exception: nil,
		}, true
	}
	return nil, false
}

func shouldAttemptShrink(schema *Schema, input types.Value, shrinkLimit int) bool {
	if shrinkLimit == 0 {
		return false
	}
	if schema == nil {
		return false
	}
	if schema.Kind() != KindTuple {
		return true
	}
	items, ok := seqLikeItems(input)
	if !ok {
		return false
	}
	return len(items) > 0
}

func shrinkCounterexample(ctx context.Context, property types.Fn, schema *Schema, originalInput types.Value, originalFailure *CheckFailure, shrinkLimit int) (types.Value, []types.Value, *CheckFailure, error) {
	currentInput := originalInput
	currentFailure := originalFailure
	accepted := make([]types.Value, 0)
	explored := 0

	for explored < shrinkLimit {
		stream, err := ShrinkWithContext(ctx, schema, currentInput)
		if err != nil {
			var shrinkErr *ShrinkError
			if ok := asShrinkUnsupported(err, &shrinkErr); ok {
				return currentInput, accepted, currentFailure, nil
			}
			return nil, nil, nil, err
		}

		foundBetter := false
		for candidate := range stream {
			explored++
			failure, failed := evaluatePropertyFailure(ctx, property, inputArgsForSchema(schema, candidate))
			if !failed {
				if explored >= shrinkLimit {
					break
				}
				continue
			}
			accepted = append(accepted, candidate)
			currentInput = candidate
			currentFailure = failure
			foundBetter = true
			break
		}
		if !foundBetter || explored >= shrinkLimit {
			break
		}
	}

	return currentInput, accepted, currentFailure, nil
}

func asShrinkUnsupported(err error, target **ShrinkError) bool {
	shrinkErr, ok := err.(*ShrinkError)
	if !ok {
		return false
	}
	if target != nil {
		*target = shrinkErr
	}
	return shrinkErr.Reason == "unsupported shrink schema kind"
}

func isNilFn(property types.Fn) bool {
	if property == nil {
		return true
	}
	value := reflect.ValueOf(property)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func validateCheckGeneratorOverrides(schema *Schema, overrides map[Kind]any) error {
	for kind, override := range overrides {
		switch override.(type) {
		case GeneratorFunc:
			continue
		case func(*Schema, *rand.Rand, GenerateOptions) (types.Value, error):
			continue
		case types.Value:
			continue
		default:
			return &CheckError{
				Schema:      schema,
				OptionPath:  Path{atom.NewKeyword("generator-overrides"), atom.NewKeyword(string(kind))},
				OptionValue: override,
				Reason:      "malformed generator override",
				Message:     fmt.Sprintf("malformed generator override for %s", kind),
			}
		}
	}
	return nil
}
