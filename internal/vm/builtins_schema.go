package vm

import (
	"context"
	"errors"
	"fmt"
	"math/rand"

	"doggo/internal/compiler"
	doggoerr "doggo/internal/errors"
	"doggo/internal/schema"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/types/fun"
)

var builtinsSchema = []BuiltinDefinition{
	{
		Name: "valid?",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("valid?", args, 2); err != nil {
				return nil, err
			}

			compiled, err := normalizeSchemaArg(args[0])
			if err != nil {
				return nil, err
			}
			return atom.NewBoolean(schema.ValidateWithContext(ctx, compiled, args[1])), nil
		}},
	},
	{
		Name: "explain",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("explain", args, 2); err != nil {
				return nil, err
			}

			compiled, err := normalizeSchemaArg(args[0])
			if err != nil {
				return nil, err
			}
			return explainValue(schema.ExplainWithContext(ctx, compiled, args[1])), nil
		}},
	},
	{
		Name: "generate",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("generate", args, 1, 2); err != nil {
				return nil, err
			}

			compiled, err := normalizeSchemaArg(args[0])
			if err != nil {
				return nil, err
			}

			opts, err := parseSchemaGenerateOptions(ctx, "generate", compiled, optValue(args, 1))
			if err != nil {
				return nil, err
			}

			return schema.Generate(compiled, opts)
		}},
	},
	{
		Name: "sample",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("sample", args, 2, 3); err != nil {
				return nil, err
			}

			compiled, err := normalizeSchemaArg(args[0])
			if err != nil {
				return nil, err
			}

			count, err := schemaGenerateCount("sample", compiled, args[1])
			if err != nil {
				return nil, err
			}

			opts, err := parseSchemaGenerateOptions(ctx, "sample", compiled, optValue(args, 2))
			if err != nil {
				return nil, err
			}

			values, err := schema.Sample(compiled, count, opts)
			if err != nil {
				return nil, err
			}
			return coll.NewVector(values...), nil
		}},
	},
	{
		Name: "shrink",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("shrink", args, 2); err != nil {
				return nil, err
			}

			compiled, err := normalizeSchemaArg(args[0])
			if err != nil {
				return nil, err
			}

			stream, err := schema.Shrink(compiled, args[1])
			if err != nil {
				return nil, err
			}

			return newLazySeqFromValueStream(stream), nil
		}},
	},
	{
		Name: "shrink-seq",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("shrink-seq", args, 3); err != nil {
				return nil, err
			}

			compiled, err := normalizeSchemaArg(args[0])
			if err != nil {
				return nil, err
			}

			count, err := schemaShrinkCount("shrink-seq", compiled, args[2])
			if err != nil {
				return nil, err
			}

			values, err := schema.ShrinkSeq(compiled, args[1], count)
			if err != nil {
				return nil, err
			}
			return coll.NewVector(values...), nil
		}},
	},
	{
		Name: "instrument",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("instrument", args, 2, 3); err != nil {
				return nil, err
			}

			contract, opts, err := parseSchemaInstrumentSpec(args[0], args[1], optValue(args, 2))
			if err != nil {
				return nil, err
			}

			if symbol, ok := args[0].(types.Symbol); ok {
				return instrumentNamedSchemaTarget(ctx, string(symbol), contract, opts)
			}

			callable, err := schemaInstrumentTargetCallable(ctx, args[0])
			if err != nil {
				return nil, err
			}
			return schema.InstrumentCallable(callable, contract, opts)
		}},
	},
	{
		Name: "unstrument",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("unstrument", args, 1); err != nil {
				return nil, err
			}

			if symbol, ok := args[0].(types.Symbol); ok {
				return unstrumentNamedSchemaTarget(ctx, string(symbol))
			}

			return schemaInstrumentOriginalValue(schema.UnstrumentCallable(args[0])), nil
		}},
	},
	{
		Name: "check",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("check", args, 2, 3); err != nil {
				return nil, err
			}

			property, err := schemaCheckTargetCallable(ctx, args[0])
			if err != nil {
				return nil, err
			}

			compiled, err := normalizeSchemaArg(args[1])
			if err != nil {
				return nil, err
			}

			opts, err := parseSchemaCheckOptions(ctx, "check", compiled, optValue(args, 2))
			if err != nil {
				return nil, err
			}

			report, err := schema.Check(ctx, property, compiled, opts)
			if err != nil {
				return nil, err
			}
			return schemaCheckReportValue(report), nil
		}},
	},
}

func normalizeSchemaArg(value types.Value) (*schema.Schema, error) {
	compiled, err := schema.Normalize(value)
	if err == nil {
		return compiled, nil
	}
	var normalizeErr *schema.NormalizeError
	if errors.As(err, &normalizeErr) {
		return nil, &schemaNormalizationError{cause: normalizeErr}
	}
	return nil, doggoerr.EvalError("schema", err.Error()).SetCause(err)
}

type schemaNormalizationError struct {
	cause *schema.NormalizeError
}

func (e *schemaNormalizationError) Error() string {
	if e == nil || e.cause == nil {
		return "schema normalization failed"
	}
	return e.cause.Error()
}

func (e *schemaNormalizationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *schemaNormalizationError) ExceptionData() *doggoerr.ExceptionData {
	if e == nil || e.cause == nil {
		return nil
	}

	data := map[string]any{}
	if len(e.cause.Path) > 0 {
		path := make([]any, len(e.cause.Path))
		for i, item := range e.cause.Path {
			path[i] = item
		}
		data["path"] = path
	}
	if e.cause.Expected != "" {
		data["expected"] = e.cause.Expected
	}
	if e.cause.Actual != nil {
		data["actual"] = e.cause.Actual
		data["actual_type"] = e.cause.Actual.Type()
	}

	return &doggoerr.ExceptionData{
		Type:    "SchemaNormalizeError",
		Kind:    "schema normalization error",
		Phase:   "runtime",
		Message: e.cause.Error(),
		Data:    data,
		Context: map[string]any{"context": "schema"},
		Cause:   doggoerr.NormalizeExceptionData(e.cause),
	}
}

func explainValue(explanation schema.Explanation) types.Value {
	problems := make([]types.Value, len(explanation.Problems))
	for i, problem := range explanation.Problems {
		problems[i] = explainProblemValue(problem)
	}

	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("valid?"), atom.NewBoolean(explanation.Valid),
		atom.NewKeyword("schema"), nilToValue(explanation.Schema.Raw()),
		atom.NewKeyword("value"), nilToValue(explanation.Value),
		atom.NewKeyword("problems"), coll.NewVector(problems...),
	})
}

func explainProblemValue(problem schema.Problem) types.Value {
	pairs := []types.Value{
		atom.NewKeyword("path"), pathValue(problem.Path),
		atom.NewKeyword("expected"), nilToValue(problem.Expected),
		atom.NewKeyword("actual"), nilToValue(problem.Actual),
		atom.NewKeyword("message"), atom.NewString(problem.Message),
	}
	if problem.Predicate != nil {
		pairs = append(pairs, atom.NewKeyword("predicate"), problem.Predicate)
	}
	if problem.Branch != nil {
		pairs = append(pairs, atom.NewKeyword("branch"), atom.NewNumberInt(int64(*problem.Branch)))
	}
	return coll.NewMapFromPairs(pairs)
}

func pathValue(path schema.Path) types.Value {
	if len(path) == 0 {
		return coll.NewVector()
	}
	items := make([]types.Value, len(path))
	copy(items, path)
	return coll.NewVector(items...)
}

func nilToValue(value types.Value) types.Value {
	if value == nil {
		return atom.NilValue
	}
	return value
}

func optValue(args []types.Value, index int) types.Value {
	if len(args) <= index {
		return nil
	}
	return args[index]
}

func parseSchemaGenerateOptions(ctx context.Context, fnName string, compiled *schema.Schema, raw types.Value) (schema.GenerateOptions, error) {
	if raw == nil || raw == atom.NilValue {
		return schema.GenerateOptions{}, nil
	}

	optionMap, ok := raw.(types.Map)
	if !ok {
		return schema.GenerateOptions{}, schemaGenerateOptionError(compiled, nil, raw, fmt.Sprintf("%s: options must be a map, got %s", fnName, schemaValueTypeName(raw)))
	}

	opts := schema.GenerateOptions{}

	if err := parseSchemaSeedOption(fnName, compiled, optionMap, &opts); err != nil {
		return schema.GenerateOptions{}, err
	}
	if err := parseSchemaSizeOption(fnName, compiled, optionMap, &opts); err != nil {
		return schema.GenerateOptions{}, err
	}
	if err := parseSchemaGeneratorOverridesOption(ctx, fnName, compiled, optionMap, &opts); err != nil {
		return schema.GenerateOptions{}, err
	}

	return opts, nil
}

func parseSchemaCheckOptions(ctx context.Context, fnName string, compiled *schema.Schema, raw types.Value) (schema.CheckOptions, error) {
	if raw == nil || raw == atom.NilValue {
		return schema.CheckOptions{}, nil
	}

	optionMap, ok := raw.(types.Map)
	if !ok {
		return schema.CheckOptions{}, schemaCheckOptionError(compiled, nil, raw, "malformed check options", fmt.Sprintf("%s: options must be a map, got %s", fnName, schemaValueTypeName(raw)))
	}

	opts := schema.CheckOptions{}
	if err := parseSchemaCheckSeedOption(fnName, compiled, optionMap, &opts); err != nil {
		return schema.CheckOptions{}, err
	}
	if err := parseSchemaCheckIterationsOption(fnName, compiled, optionMap, &opts); err != nil {
		return schema.CheckOptions{}, err
	}
	if err := parseSchemaCheckSizeOption(fnName, compiled, optionMap, &opts); err != nil {
		return schema.CheckOptions{}, err
	}
	if err := parseSchemaCheckGeneratorOverridesOption(ctx, fnName, compiled, optionMap, &opts); err != nil {
		return schema.CheckOptions{}, err
	}
	if err := parseSchemaCheckShrinkLimitOption(fnName, compiled, optionMap, &opts); err != nil {
		return schema.CheckOptions{}, err
	}
	return opts, nil
}

func parseSchemaSeedOption(fnName string, compiled *schema.Schema, options types.Map, opts *schema.GenerateOptions) error {
	value := options.Get(atom.NewKeyword("seed"))
	if value == nil || value == atom.NilValue {
		return nil
	}

	seed, ok := value.(types.Int64Valuer)
	if !ok {
		return schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("seed")}, value, fmt.Sprintf("%s: option :seed must be an integer, got %s", fnName, schemaValueTypeName(value)))
	}
	seedValue, ok := seed.Int64Value()
	if !ok {
		return schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("seed")}, value, fmt.Sprintf("%s: option :seed must be an integer, got %s", fnName, schemaValueTypeName(value)))
	}
	opts.Seed = seedValue
	return nil
}

func parseSchemaCheckSeedOption(fnName string, compiled *schema.Schema, options types.Map, opts *schema.CheckOptions) error {
	value := options.Get(atom.NewKeyword("seed"))
	if value == nil || value == atom.NilValue {
		return nil
	}

	seed, ok := value.(types.Int64Valuer)
	if !ok {
		return schemaCheckOptionError(compiled, schema.Path{atom.NewKeyword("seed")}, value, "malformed check options", fmt.Sprintf("%s: option :seed must be an integer, got %s", fnName, schemaValueTypeName(value)))
	}
	seedValue, ok := seed.Int64Value()
	if !ok {
		return schemaCheckOptionError(compiled, schema.Path{atom.NewKeyword("seed")}, value, "malformed check options", fmt.Sprintf("%s: option :seed must be an integer, got %s", fnName, schemaValueTypeName(value)))
	}
	opts.Seed = seedValue
	opts.SeedSet = true
	return nil
}

func parseSchemaSizeOption(fnName string, compiled *schema.Schema, options types.Map, opts *schema.GenerateOptions) error {
	value := options.Get(atom.NewKeyword("size"))
	if value == nil || value == atom.NilValue {
		return nil
	}

	size, ok := value.(types.Int64Valuer)
	if !ok {
		return schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("size")}, value, fmt.Sprintf("%s: option :size must be an integer, got %s", fnName, schemaValueTypeName(value)))
	}
	sizeValue, ok := size.Int64Value()
	if !ok {
		return schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("size")}, value, fmt.Sprintf("%s: option :size must be an integer, got %s", fnName, schemaValueTypeName(value)))
	}
	if sizeValue < 0 {
		return schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("size")}, value, fmt.Sprintf("%s: option :size must be non-negative, got %d", fnName, sizeValue))
	}
	if sizeValue > int64(^uint(0)>>1) {
		return schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("size")}, value, fmt.Sprintf("%s: option :size is too large, got %d", fnName, sizeValue))
	}
	opts.Size = int(sizeValue)
	return nil
}

func parseSchemaCheckIterationsOption(fnName string, compiled *schema.Schema, options types.Map, opts *schema.CheckOptions) error {
	iterations, set, err := parseSchemaCheckNonNegativeIntOption(fnName, compiled, options, "iterations", "malformed iteration count")
	if err != nil || !set {
		return err
	}
	opts.Iterations = iterations
	opts.IterationsSet = true
	return nil
}

func parseSchemaCheckSizeOption(fnName string, compiled *schema.Schema, options types.Map, opts *schema.CheckOptions) error {
	size, set, err := parseSchemaCheckNonNegativeIntOption(fnName, compiled, options, "size", "malformed check options")
	if err != nil || !set {
		return err
	}
	opts.Size = size
	return nil
}

func parseSchemaGeneratorOverridesOption(ctx context.Context, fnName string, compiled *schema.Schema, options types.Map, opts *schema.GenerateOptions) error {
	value := options.Get(atom.NewKeyword("generator-overrides"))
	if value == nil || value == atom.NilValue {
		return nil
	}

	overrideMap, ok := value.(types.Map)
	if !ok {
		return schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("generator-overrides")}, value, fmt.Sprintf("%s: option :generator-overrides must be a map, got %s", fnName, schemaValueTypeName(value)))
	}

	overrides := make(map[schema.Kind]any)
	for _, key := range sequableToSlice(overrideMap.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok {
			return schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("generator-overrides"), key}, overrideMap.Get(key), fmt.Sprintf("%s: generator override keys must be keywords, got %s", fnName, schemaValueTypeName(key)))
		}

		overrideValue, err := schemaGenerationOverrideValue(ctx, overrideMap.Get(key))
		if err != nil {
			return err
		}
		overrides[schema.Kind(string(keyword))] = overrideValue
	}

	opts.GeneratorOverrides = overrides
	return nil
}

func parseSchemaCheckGeneratorOverridesOption(ctx context.Context, fnName string, compiled *schema.Schema, options types.Map, opts *schema.CheckOptions) error {
	value := options.Get(atom.NewKeyword("generator-overrides"))
	if value == nil || value == atom.NilValue {
		return nil
	}

	overrideMap, ok := value.(types.Map)
	if !ok {
		return schemaCheckOptionError(compiled, schema.Path{atom.NewKeyword("generator-overrides")}, value, "malformed check options", fmt.Sprintf("%s: option :generator-overrides must be a map, got %s", fnName, schemaValueTypeName(value)))
	}

	overrides := make(map[schema.Kind]any)
	for _, key := range sequableToSlice(overrideMap.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok {
			return schemaCheckOptionError(compiled, schema.Path{atom.NewKeyword("generator-overrides"), key}, overrideMap.Get(key), "malformed check options", fmt.Sprintf("%s: generator override keys must be keywords, got %s", fnName, schemaValueTypeName(key)))
		}
		overrideValue, err := schemaCheckGenerationOverrideValue(ctx, compiled, key, overrideMap.Get(key))
		if err != nil {
			return err
		}
		overrides[schema.Kind(string(keyword))] = overrideValue
	}

	opts.GeneratorOverrides = overrides
	return nil
}

func parseSchemaCheckShrinkLimitOption(fnName string, compiled *schema.Schema, options types.Map, opts *schema.CheckOptions) error {
	limit, set, err := parseSchemaCheckNonNegativeIntOption(fnName, compiled, options, "shrink-limit", "malformed shrink limit")
	if err != nil || !set {
		return err
	}
	opts.ShrinkLimit = limit
	opts.ShrinkLimitSet = true
	return nil
}

func parseSchemaCheckNonNegativeIntOption(fnName string, compiled *schema.Schema, options types.Map, optionName, problem string) (int, bool, error) {
	keyword := atom.NewKeyword(optionName)
	value := options.Get(keyword)
	if value == nil || value == atom.NilValue {
		return 0, false, nil
	}

	number, ok := value.(types.Int64Valuer)
	if !ok {
		return 0, false, schemaCheckOptionError(compiled, schema.Path{keyword}, value, problem, fmt.Sprintf("%s: option :%s must be an integer, got %s", fnName, optionName, schemaValueTypeName(value)))
	}
	intValue, ok := number.Int64Value()
	if !ok {
		return 0, false, schemaCheckOptionError(compiled, schema.Path{keyword}, value, problem, fmt.Sprintf("%s: option :%s must be an integer, got %s", fnName, optionName, schemaValueTypeName(value)))
	}
	if intValue < 0 {
		return 0, false, schemaCheckOptionError(compiled, schema.Path{keyword}, value, problem, fmt.Sprintf("%s: option :%s must be non-negative, got %d", fnName, optionName, intValue))
	}
	if intValue > int64(^uint(0)>>1) {
		return 0, false, schemaCheckOptionError(compiled, schema.Path{keyword}, value, problem, fmt.Sprintf("%s: option :%s is too large, got %d", fnName, optionName, intValue))
	}
	return int(intValue), true, nil
}

func schemaGenerationOverrideValue(ctx context.Context, value types.Value) (any, error) {
	switch fn := value.(type) {
	case *Builtin:
		return schema.GeneratorFunc(func(_ *schema.Schema, _ *rand.Rand, _ schema.GenerateOptions) (types.Value, error) {
			return fn.Invoke(ctx)
		}), nil
	case *fun.Multimethod:
		return schema.GeneratorFunc(func(_ *schema.Schema, _ *rand.Rand, _ schema.GenerateOptions) (types.Value, error) {
			return fn.Invoke(ctx)
		}), nil
	case *compiler.Closure:
		vm := GetVM(ctx)
		if vm == nil {
			return nil, schemaGenerateOptionError(nil, schema.Path{atom.NewKeyword("generator-overrides")}, value, "callable generator override requires VM context")
		}
		callCtx := ctx
		if callCtx == nil {
			callCtx = context.Background()
		}
		return schema.GeneratorFunc(func(_ *schema.Schema, _ *rand.Rand, _ schema.GenerateOptions) (types.Value, error) {
			return vm.Apply(callCtx, fn, nil)
		}), nil
	default:
		return value, nil
	}
}

func schemaCheckGenerationOverrideValue(ctx context.Context, compiled *schema.Schema, key, value types.Value) (any, error) {
	switch fn := value.(type) {
	case *Builtin:
		return schema.GeneratorFunc(func(_ *schema.Schema, _ *rand.Rand, _ schema.GenerateOptions) (types.Value, error) {
			return fn.Invoke(ctx)
		}), nil
	case *fun.Multimethod:
		return schema.GeneratorFunc(func(_ *schema.Schema, _ *rand.Rand, _ schema.GenerateOptions) (types.Value, error) {
			return fn.Invoke(ctx)
		}), nil
	case *compiler.Closure:
		vm := GetVM(ctx)
		if vm == nil {
			return nil, schemaCheckOptionError(compiled, schema.Path{atom.NewKeyword("generator-overrides"), key}, value, "malformed check options", "callable generator override requires VM context")
		}
		callCtx := ctx
		if callCtx == nil {
			callCtx = context.Background()
		}
		return schema.GeneratorFunc(func(_ *schema.Schema, _ *rand.Rand, _ schema.GenerateOptions) (types.Value, error) {
			return vm.Apply(callCtx, fn, nil)
		}), nil
	default:
		return value, nil
	}
}

func parseSchemaInstrumentSpec(target, rawContract, rawOpts types.Value) (schema.InstrumentContract, schema.InstrumentOptions, error) {
	contract, err := parseSchemaInstrumentContract(target, rawContract)
	if err != nil {
		return schema.InstrumentContract{}, schema.InstrumentOptions{}, err
	}

	if err := parseSchemaInstrumentOptions(target, rawOpts); err != nil {
		return schema.InstrumentContract{}, schema.InstrumentOptions{}, err
	}
	opts := schema.InstrumentOptions{}
	opts.Target = target

	return contract, opts, nil
}

func parseSchemaInstrumentContract(target, raw types.Value) (schema.InstrumentContract, error) {
	optionMap, ok := raw.(types.Map)
	if !ok {
		return schema.InstrumentContract{}, &schema.InstrumentError{
			Target:  target,
			Reason:  "malformed instrumentation contract",
			Message: "instrument contract must be a map",
		}
	}

	contract := schema.InstrumentContract{}
	if rawArgs := optionMap.Get(atom.NewKeyword("args")); rawArgs != nil && rawArgs != atom.NilValue {
		argsSchema, err := normalizeSchemaArg(rawArgs)
		if err != nil {
			return schema.InstrumentContract{}, err
		}
		contract.Args = argsSchema
	}
	if rawRet := optionMap.Get(atom.NewKeyword("ret")); rawRet != nil && rawRet != atom.NilValue {
		retSchema, err := normalizeSchemaArg(rawRet)
		if err != nil {
			return schema.InstrumentContract{}, err
		}
		contract.Ret = retSchema
	}

	return contract, nil
}

func parseSchemaInstrumentOptions(target, raw types.Value) error {
	if raw == nil || raw == atom.NilValue {
		return nil
	}

	optionMap, ok := raw.(types.Map)
	if !ok {
		return &schema.InstrumentError{
			Target:  target,
			Reason:  "malformed instrumentation options",
			Message: "instrument options must be a map",
		}
	}

	if keys := sequableToSlice(optionMap.Keys()); len(keys) > 0 {
		return &schema.InstrumentError{
			Target:  target,
			Reason:  "unsupported instrumentation options",
			Message: "instrument options do not support keys in the first slice",
		}
	}

	return nil
}

func instrumentNamedSchemaTarget(ctx context.Context, symbolName string, contract schema.InstrumentContract, opts schema.InstrumentOptions) (types.Value, error) {
	runner, target, err := resolveSchemaInstrumentNamedTarget(ctx, symbolName)
	if err != nil {
		return nil, err
	}

	rootValue := schemaInstrumentTargetRootValue(target)
	callable, err := schemaInstrumentTargetCallable(ctx, rootValue)
	if err != nil {
		return nil, err
	}

	wrapped, err := schema.InstrumentCallable(callable, contract, opts)
	if err != nil {
		return nil, err
	}

	if err := rewriteSchemaInstrumentTarget(runner, target, wrapped); err != nil {
		return nil, err
	}
	return wrapped, nil
}

func unstrumentNamedSchemaTarget(ctx context.Context, symbolName string) (types.Value, error) {
	runner, target, err := resolveSchemaInstrumentNamedTarget(ctx, symbolName)
	if err != nil {
		return nil, err
	}

	restored := schemaInstrumentOriginalValue(schema.UnstrumentCallable(schemaInstrumentTargetRootValue(target)))
	if err := rewriteSchemaInstrumentTarget(runner, target, restored); err != nil {
		return nil, err
	}
	return restored, nil
}

func resolveSchemaInstrumentNamedTarget(ctx context.Context, symbolName string) (*Runner, *introspectionTarget, error) {
	vm := GetVM(ctx)
	if vm == nil {
		return nil, nil, &schema.InstrumentError{
			Target:  types.Symbol(symbolName),
			Reason:  "named target instrumentation requires VM context",
			Message: "named target instrumentation requires VM context",
		}
	}

	runner := vm.getOrCreateExecutionSession().runner
	if runner == nil {
		return nil, nil, &schema.InstrumentError{
			Target:  types.Symbol(symbolName),
			Reason:  "named target instrumentation requires runner context",
			Message: "named target instrumentation requires runner context",
		}
	}

	if symbolName == "" || compiler.IsQualifiedSymbol(symbolName) {
		return nil, nil, &schema.InstrumentError{
			Target:  types.Symbol(symbolName),
			Reason:  "named target must be an unqualified current-namespace symbol",
			Message: "instrument target must be an unqualified current-namespace symbol",
		}
	}

	baseNs := schemaInstrumentationNamespace(vm, runner)
	if baseNs == "" {
		return nil, nil, &schema.InstrumentError{
			Target:  types.Symbol(symbolName),
			Reason:  "named target instrumentation requires current namespace context",
			Message: "instrument target requires current namespace context",
		}
	}

	target := runner.resolveIntrospectionTargetInNamespace(baseNs, symbolName)
	expectedQualified := compiler.QualifySymbol(baseNs, symbolName)
	if target == nil || target.symbol.Scope != compiler.GlobalScope || target.qualifiedName != expectedQualified {
		return nil, nil, &schema.InstrumentError{
			Target:  types.Symbol(symbolName),
			Reason:  "named target must resolve to a current-namespace global binding",
			Message: "instrument target must resolve to a current-namespace global binding",
		}
	}

	rootValue := schemaInstrumentTargetRootValue(target)
	if rootValue == nil {
		return nil, nil, &schema.InstrumentError{
			Target:  types.Symbol(symbolName),
			Reason:  "named target must resolve to a callable",
			Message: "instrument target must resolve to a callable",
		}
	}

	return runner, target, nil
}

func schemaInstrumentationNamespace(vm *VM, runner *Runner) string {
	if vm != nil {
		for i := vm.frameIndex - 1; i >= 0; i-- {
			frame := vm.frames[i]
			if frame == nil {
				continue
			}
			if ns := frame.CurrentNamespace(); ns != "" {
				return ns
			}
		}
	}
	if runner != nil {
		return runner.CurrentNamespace()
	}
	return ""
}

func schemaInstrumentTargetRootValue(target *introspectionTarget) types.Value {
	if target == nil {
		return nil
	}
	if v, ok := target.value.(*atom.Var); ok {
		return v.Value
	}
	return target.value
}

func rewriteSchemaInstrumentTarget(runner *Runner, target *introspectionTarget, value types.Value) error {
	if runner == nil || target == nil {
		return &schema.InstrumentError{
			Target:  nil,
			Reason:  "named target instrumentation requires runner context",
			Message: "instrument target rewrite requires runner context",
		}
	}
	if v, ok := target.value.(*atom.Var); ok {
		v.Value = value
		return nil
	}
	session := runner.getOrCreateExecutionSession()
	if target.symbol.Index < 0 || target.symbol.Index >= len(session.globals) {
		return &schema.InstrumentError{
			Target:  types.Symbol(target.qualifiedName),
			Reason:  "named target global slot is unavailable",
			Message: "instrument target global slot is unavailable",
		}
	}
	session.globals[target.symbol.Index] = value
	target.value = value
	return nil
}

func schemaInstrumentTargetCallable(ctx context.Context, value types.Value) (types.Fn, error) {
	if fn, ok := value.(types.Fn); ok {
		return fn, nil
	}

	switch value.(type) {
	case *compiler.Closure, *compiler.CompiledFunction:
		vm := GetVM(ctx)
		if vm == nil {
			return nil, &schema.InstrumentError{
				Target:  value,
				Reason:  "instrument target requires VM context",
				Message: "instrument target requires VM context",
			}
		}
		return &schemaInstrumentVMCallable{vm: vm, target: value}, nil
	default:
		return nil, &schema.InstrumentError{
			Target:  value,
			Reason:  "instrument target must be callable",
			Message: "instrument target must be callable",
		}
	}
}

func schemaCheckTargetCallable(ctx context.Context, value types.Value) (types.Fn, error) {
	if fn, ok := value.(types.Fn); ok {
		return fn, nil
	}

	switch value.(type) {
	case *compiler.Closure, *compiler.CompiledFunction:
		vm := GetVM(ctx)
		if vm == nil {
			return nil, &schema.CheckError{
				Reason:  "non-callable property target",
				Message: "property target requires VM context",
			}
		}
		return &schemaInstrumentVMCallable{vm: vm, target: value}, nil
	default:
		return nil, &schema.CheckError{
			Reason:  "non-callable property target",
			Message: "property target must be callable",
		}
	}
}

type schemaInstrumentVMCallable struct {
	vm     *VM
	target types.Value
}

func (f *schemaInstrumentVMCallable) String() string {
	if f == nil || f.target == nil {
		return "#<instrument-target:nil>"
	}
	return f.target.String()
}

func (f *schemaInstrumentVMCallable) Type() string {
	if f == nil || f.target == nil {
		return "instrument-target"
	}
	return f.target.Type()
}

func (f *schemaInstrumentVMCallable) Hash() uint32 {
	if f == nil || f.target == nil {
		return 0
	}
	return f.target.Hash()
}

func (f *schemaInstrumentVMCallable) Equals(other types.Value) bool {
	return f == other
}

func (f *schemaInstrumentVMCallable) IsSelfEvaluating() bool { return true }

func (f *schemaInstrumentVMCallable) schemaInstrumentOriginalValue() types.Value {
	if f == nil {
		return nil
	}
	return f.target
}

func (f *schemaInstrumentVMCallable) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if f == nil || f.vm == nil || f.target == nil {
		return nil, &schema.InstrumentError{
			Target:  nil,
			Reason:  "instrument target requires VM context",
			Message: "instrument target requires VM context",
		}
	}

	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	return f.vm.Apply(callCtx, f.target, args)
}

type schemaInstrumentOriginaler interface {
	schemaInstrumentOriginalValue() types.Value
}

func schemaInstrumentOriginalValue(value types.Value) types.Value {
	if originaler, ok := value.(schemaInstrumentOriginaler); ok {
		if original := originaler.schemaInstrumentOriginalValue(); original != nil {
			return original
		}
	}
	return value
}

func schemaGenerateCount(fnName string, compiled *schema.Schema, raw types.Value) (int, error) {
	intVal, ok := raw.(types.Int64Valuer)
	if !ok {
		return 0, schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("n")}, raw, fmt.Sprintf("%s: sample count must be an integer, got %s", fnName, schemaValueTypeName(raw)))
	}
	n, ok := intVal.Int64Value()
	if !ok {
		return 0, schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("n")}, raw, fmt.Sprintf("%s: sample count must be an integer, got %s", fnName, schemaValueTypeName(raw)))
	}
	if n < 0 {
		return 0, schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("n")}, raw, fmt.Sprintf("%s: sample count must be non-negative, got %d", fnName, n))
	}
	if n > int64(^uint(0)>>1) {
		return 0, schemaGenerateOptionError(compiled, schema.Path{atom.NewKeyword("n")}, raw, fmt.Sprintf("%s: sample count is too large, got %d", fnName, n))
	}
	return int(n), nil
}

func schemaShrinkCount(fnName string, compiled *schema.Schema, raw types.Value) (int, error) {
	intVal, ok := raw.(types.Int64Valuer)
	if !ok {
		return 0, &schema.ShrinkError{
			Schema:  compiled,
			Value:   raw,
			Count:   -1,
			Reason:  "malformed shrink count",
			Message: fmt.Sprintf("%s: count must be an integer, got %s", fnName, schemaValueTypeName(raw)),
		}
	}
	n, ok := intVal.Int64Value()
	if !ok {
		return 0, &schema.ShrinkError{
			Schema:  compiled,
			Value:   raw,
			Count:   -1,
			Reason:  "malformed shrink count",
			Message: fmt.Sprintf("%s: count must be an integer, got %s", fnName, schemaValueTypeName(raw)),
		}
	}
	if n < 0 {
		return 0, &schema.ShrinkError{
			Schema:  compiled,
			Value:   raw,
			Count:   int(n),
			Reason:  "malformed shrink count",
			Message: fmt.Sprintf("%s: count must be non-negative, got %d", fnName, n),
		}
	}
	if n > int64(^uint(0)>>1) {
		return 0, &schema.ShrinkError{
			Schema:  compiled,
			Value:   raw,
			Count:   int(^uint(0) >> 1),
			Reason:  "malformed shrink count",
			Message: fmt.Sprintf("%s: count is too large, got %d", fnName, n),
		}
	}
	return int(n), nil
}

func schemaGenerateOptionError(compiled *schema.Schema, optionPath schema.Path, optionValue any, message string) error {
	return &schema.GenerateError{
		Schema:      compiled,
		OptionPath:  optionPath,
		OptionValue: optionValue,
		Reason:      "malformed generate options",
		Message:     message,
	}
}

func schemaCheckOptionError(compiled *schema.Schema, optionPath schema.Path, optionValue any, reason, message string) error {
	return &schema.CheckError{
		Schema:      compiled,
		OptionPath:  optionPath,
		OptionValue: optionValue,
		Reason:      reason,
		Message:     message,
	}
}

func schemaValueTypeName(value types.Value) string {
	if value == nil {
		return "nil"
	}
	return value.Type()
}
