package schema

import (
	"fmt"
	"math/rand"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

const maxGenerateSize = 32
const maxUniqueGenerationAttemptsPerItem = 8

type GeneratorFunc func(schema *Schema, rng *rand.Rand, opts GenerateOptions) (types.Value, error)

type GenerateOptions struct {
	Seed               int64
	Size               int
	GeneratorOverrides map[Kind]any
}

type GenerateError struct {
	Path        Path
	Schema      *Schema
	OptionPath  Path
	OptionValue any
	Reason      string
	Message     string
	Cause       error
}

func (e *GenerateError) Error() string {
	if e == nil {
		return "schema generation failed"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Reason != "" {
		return e.Reason
	}
	return "schema generation failed"
}

func (e *GenerateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *GenerateError) ExceptionData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}

	data := map[string]any{
		"schema_path": pathData(e.Path),
	}
	if e.Schema != nil {
		data["schema_form"] = e.Schema.Raw()
		data["schema_kind"] = string(e.Schema.Kind())
	}
	if e.Reason != "" {
		data["reason"] = e.Reason
	}
	if len(e.OptionPath) > 0 {
		data["option_path"] = pathData(e.OptionPath)
	}
	if e.OptionValue != nil {
		data["option_value"] = e.OptionValue
	}

	message := e.Error()
	if message == "" {
		message = "schema generation failed"
	}

	return schemaExceptionData("SchemaGenerateError", "schema generation error", message, data, e.Cause)
}

func Generate(schema *Schema, opts GenerateOptions) (types.Value, error) {
	rng := rand.New(rand.NewSource(opts.Seed))
	return generateWithRNG(schema, rng, normalizeGenerateOptions(opts), nil)
}

func Sample(schema *Schema, n int, opts GenerateOptions) ([]types.Value, error) {
	if n < 0 {
		return nil, &GenerateError{Schema: schema, Reason: "invalid sample size", Message: fmt.Sprintf("sample count must be non-negative, got %d", n)}
	}

	rng := rand.New(rand.NewSource(opts.Seed))
	norm := normalizeGenerateOptions(opts)
	values := make([]types.Value, 0, n)
	for i := 0; i < n; i++ {
		value, err := generateWithRNG(schema, rng, norm, nil)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func normalizeGenerateOptions(opts GenerateOptions) GenerateOptions {
	opts.Size = boundedGenerateSize(opts.Size)
	return opts
}

func boundedGenerateSize(size int) int {
	if size < 0 {
		return 0
	}
	if size > maxGenerateSize {
		return maxGenerateSize
	}
	return size
}

func generateWithRNG(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	if schema == nil {
		return nil, &GenerateError{Path: path, Reason: "nil schema", Message: "schema cannot be nil"}
	}

	if value, handled, err := generateOverride(schema, rng, opts, path); handled {
		return value, err
	}

	switch schema.kind {
	case KindAny:
		return atom.NilValue, nil
	case KindNil:
		return nil, nil
	case KindString:
		return atom.NewString(fmt.Sprintf("s-%d", rng.Int63())), nil
	case KindKeyword:
		return atom.NewKeyword(fmt.Sprintf("k-%d", rng.Int63())), nil
	case KindSymbol:
		return types.NewSymbol(fmt.Sprintf("s-%d", rng.Int63())), nil
	case KindBool:
		return atom.NewBoolean(rng.Intn(2) == 0), nil
	case KindInt:
		return atom.NewNumberInt(int64(rng.Int63n(int64(maxGenerateSize + 1)))), nil
	case KindFloat:
		return atom.NewNumberFloat(rng.Float64() * float64(opts.Size+1)), nil
	case KindNumber:
		if rng.Intn(2) == 0 {
			return atom.NewNumberInt(int64(rng.Int63n(int64(maxGenerateSize + 1)))), nil
		}
		return atom.NewNumberFloat(rng.Float64() * float64(opts.Size+1)), nil
	case KindMaybe:
		return generateMaybe(schema, rng, opts, path)
	case KindVector:
		return generateVector(schema, rng, opts, path)
	case KindSet:
		return generateSet(schema, rng, opts, path)
	case KindList:
		return generateList(schema, rng, opts, path)
	case KindSeq:
		return generateSeq(schema, rng, opts, path)
	case KindTuple:
		return generateTuple(schema, rng, opts, path)
	case KindMap:
		return generateMap(schema, rng, opts, path)
	case KindMapOf:
		return generateMapOf(schema, rng, opts, path)
	case KindOr:
		return generateOr(schema, rng, opts, path)
	case KindEnum:
		return generateEnum(schema, rng, path)
	case KindPredicate:
		return nil, &GenerateError{Path: path, Schema: schema, Reason: "non-generatable schema kind", Message: "schema kind :predicate is not generatable without an override"}
	case KindAnd:
		return nil, &GenerateError{Path: path, Schema: schema, Reason: "non-generatable schema kind", Message: "schema kind :and is not generatable without an override"}
	default:
		return nil, &GenerateError{Path: path, Schema: schema, Reason: "unsupported schema kind", Message: fmt.Sprintf("unsupported schema kind %s", schema.kind)}
	}
}

func generateOverride(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, bool, error) {
	if len(opts.GeneratorOverrides) == 0 {
		return nil, false, nil
	}
	override, ok := opts.GeneratorOverrides[schema.kind]
	if !ok {
		return nil, false, nil
	}

	switch generator := override.(type) {
	case GeneratorFunc:
		value, err := generator(schema, rng, opts)
		if err != nil {
			return nil, true, &GenerateError{Path: path, Schema: schema, Reason: "override generator failed", Message: err.Error(), Cause: err}
		}
		return validateGeneratedValue(schema, value, path, "override generator produced invalid value")
	case func(*Schema, *rand.Rand, GenerateOptions) (types.Value, error):
		value, err := generator(schema, rng, opts)
		if err != nil {
			return nil, true, &GenerateError{Path: path, Schema: schema, Reason: "override generator failed", Message: err.Error(), Cause: err}
		}
		return validateGeneratedValue(schema, value, path, "override generator produced invalid value")
	case types.Value:
		return validateGeneratedValue(schema, generator, path, "override value does not satisfy schema")
	default:
		return nil, true, &GenerateError{
			Path:        path,
			Schema:      schema,
			OptionPath:  Path{atom.NewKeyword("generator-overrides"), atom.NewKeyword(string(schema.kind))},
			OptionValue: override,
			Reason:      "malformed generator override",
			Message:     fmt.Sprintf("malformed generator override for %s", schema.kind),
		}
	}
}

func validateGeneratedValue(schema *Schema, value types.Value, path Path, reason string) (types.Value, bool, error) {
	if Validate(schema, value) {
		return value, true, nil
	}
	return nil, true, &GenerateError{
		Path:    path,
		Schema:  schema,
		Reason:  "invalid generator override result",
		Message: reason,
	}
}

func generateMaybe(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	if schema.element == nil {
		return nil, &GenerateError{Path: path, Schema: schema, Reason: "underconstrained schema", Message: "maybe schema has no child schema"}
	}
	if opts.Size == 0 || rng.Intn(2) == 0 {
		return nil, nil
	}
	childOpts := opts
	childOpts.Size = boundedGenerateSize(opts.Size - 1)
	return generateWithRNG(schema.element, rng, childOpts, path.Push(atom.NewKeyword("value")))
}

func generateVector(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	return generateCollection(schema, rng, opts, path, func(items []types.Value) types.Value { return coll.NewVector(items...) })
}

func generateSet(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	if schema.element == nil {
		return nil, &GenerateError{Path: path, Schema: schema, Reason: "underconstrained schema", Message: fmt.Sprintf("schema kind %s requires an element schema", schema.kind)}
	}

	target := opts.Size
	items := make([]types.Value, 0, target)
	childOpts := opts
	childOpts.Size = boundedGenerateSize(opts.Size - 1)
	attemptsRemaining := maxUniqueGenerationAttempts(target)
	seen := coll.NewSet()
	for len(items) < target && attemptsRemaining > 0 {
		attemptsRemaining--
		item, err := generateWithRNG(schema.element, rng, childOpts, path.Push(indexValue(len(items))))
		if err != nil {
			return nil, err
		}
		if seen.Contains(item) {
			continue
		}
		seen = seen.Conj(item).(*coll.Set)
		items = append(items, item)
	}
	return coll.NewSet(items...), nil
}

func generateList(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	return generateCollection(schema, rng, opts, path, func(items []types.Value) types.Value { return coll.NewList(items...) })
}

func generateSeq(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	return generateCollection(schema, rng, opts, path, func(items []types.Value) types.Value { return coll.NewList(items...) })
}

func generateCollection(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path, build func([]types.Value) types.Value) (types.Value, error) {
	if schema.element == nil {
		return nil, &GenerateError{Path: path, Schema: schema, Reason: "underconstrained schema", Message: fmt.Sprintf("schema kind %s requires an element schema", schema.kind)}
	}

	count := opts.Size
	items := make([]types.Value, 0, count)
	childOpts := opts
	childOpts.Size = boundedGenerateSize(opts.Size - 1)
	for i := 0; i < count; i++ {
		item, err := generateWithRNG(schema.element, rng, childOpts, path.Push(indexValue(i)))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return build(items), nil
}

func generateTuple(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	items := make([]types.Value, 0, len(schema.items))
	childOpts := opts
	childOpts.Size = boundedGenerateSize(opts.Size - 1)
	for i, child := range schema.items {
		value, err := generateWithRNG(child, rng, childOpts, path.Push(indexValue(i)))
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return coll.NewVector(items...), nil
}

func generateMap(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	pairs := make([]types.Value, 0, len(schema.entries)*2)
	childOpts := opts
	childOpts.Size = boundedGenerateSize(opts.Size - 1)
	for _, entry := range schema.entries {
		value, err := generateWithRNG(entry.Schema, rng, childOpts, path.Push(entry.Key))
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, entry.Key, value)
	}
	return coll.NewMapFromPairs(pairs), nil
}

func generateMapOf(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	if schema.keySchema == nil || schema.valueSchema == nil {
		return nil, &GenerateError{Path: path, Schema: schema, Reason: "underconstrained schema", Message: "map-of schema requires key and value schemas"}
	}

	target := opts.Size
	pairs := make([]types.Value, 0, target*2)
	childOpts := opts
	childOpts.Size = boundedGenerateSize(opts.Size - 1)
	attemptsRemaining := maxUniqueGenerationAttempts(target)
	seenKeys := coll.NewSet()
	for len(pairs)/2 < target && attemptsRemaining > 0 {
		idx := len(pairs) / 2
		attemptsRemaining--
		key, err := generateWithRNG(schema.keySchema, rng, childOpts, path.Push(indexValue(idx)).Push(atom.NewKeyword("key")))
		if err != nil {
			return nil, err
		}
		if seenKeys.Contains(key) {
			continue
		}
		value, err := generateWithRNG(schema.valueSchema, rng, childOpts, path.Push(indexValue(idx)).Push(atom.NewKeyword("value")))
		if err != nil {
			return nil, err
		}
		seenKeys = seenKeys.Conj(key).(*coll.Set)
		pairs = append(pairs, key, value)
	}
	return coll.NewMapFromPairs(pairs), nil
}

func generateOr(schema *Schema, rng *rand.Rand, opts GenerateOptions, path Path) (types.Value, error) {
	if len(schema.items) == 0 {
		return nil, &GenerateError{Path: path, Schema: schema, Reason: "underconstrained schema", Message: "or schema requires at least one branch"}
	}
	branch := rng.Intn(len(schema.items))
	childOpts := opts
	childOpts.Size = boundedGenerateSize(opts.Size - 1)
	return generateWithRNG(schema.items[branch], rng, childOpts, path.Push(indexValue(branch)))
}

func generateEnum(schema *Schema, rng *rand.Rand, path Path) (types.Value, error) {
	if len(schema.enumValues) == 0 {
		return nil, &GenerateError{Path: path, Schema: schema, Reason: "underconstrained schema", Message: "enum schema requires at least one value"}
	}
	return schema.enumValues[rng.Intn(len(schema.enumValues))], nil
}

func schemaExceptionData(typeName, kind, message string, data map[string]any, cause error) *doggoerr.ExceptionData {
	if data == nil {
		data = map[string]any{}
	}
	return &doggoerr.ExceptionData{
		Type:    typeName,
		Kind:    kind,
		Phase:   "runtime",
		Message: message,
		Data:    data,
		Context: map[string]any{"context": "schema"},
		Cause:   doggoerr.NormalizeExceptionData(cause),
	}
}

func maxUniqueGenerationAttempts(target int) int {
	if target <= 0 {
		return 0
	}
	return target * maxUniqueGenerationAttemptsPerItem
}

func pathData(path Path) []any {
	if len(path) == 0 {
		return []any{}
	}
	data := make([]any, len(path))
	for i, item := range path {
		data[i] = item
	}
	return data
}
