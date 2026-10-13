package schema

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func Validate(schema *Schema, value types.Value) bool {
	return ValidateWithContext(context.Background(), schema, value)
}

func ValidateWithContext(ctx context.Context, schema *Schema, value types.Value) bool {
	return len(validateProblems(ctx, schema, value, nil)) == 0
}

func validateProblems(ctx context.Context, schema *Schema, value types.Value, path Path) []Problem {
	if schema == nil {
		return []Problem{{
			Path:     path,
			Expected: atom.NewKeyword("schema"),
			Actual:   value,
			Message:  "schema is nil",
		}}
	}

	switch schema.kind {
	case KindAny:
		return nil
	case KindNil:
		if isNilValue(value) {
			return nil
		}
		return []Problem{scalarProblem(path, schema, value)}
	case KindString:
		if value != nil && value.Type() == "string" {
			return nil
		}
		return []Problem{scalarProblem(path, schema, value)}
	case KindKeyword:
		if value != nil && value.Type() == "keyword" {
			return nil
		}
		return []Problem{scalarProblem(path, schema, value)}
	case KindSymbol:
		if value != nil && value.Type() == "symbol" {
			return nil
		}
		return []Problem{scalarProblem(path, schema, value)}
	case KindBool:
		if value != nil && value.Type() == "boolean" {
			return nil
		}
		return []Problem{scalarProblem(path, schema, value)}
	case KindInt:
		if intVal, ok := value.(types.Int64Valuer); ok {
			if _, exact := intVal.Int64Value(); exact {
				return nil
			}
		}
		return []Problem{scalarProblem(path, schema, value)}
	case KindFloat:
		if isFloatValue(value) {
			return nil
		}
		return []Problem{scalarProblem(path, schema, value)}
	case KindNumber:
		if _, ok := value.(types.Number); ok {
			return nil
		}
		return []Problem{scalarProblem(path, schema, value)}
	case KindMap:
		return validateMap(ctx, schema, value, path)
	case KindVector:
		return validateUnaryCollection(ctx, schema, value, path, vectorItems)
	case KindSet:
		return validateUnaryCollection(ctx, schema, value, path, setItems)
	case KindList:
		return validateUnaryCollection(ctx, schema, value, path, listItems)
	case KindSeq:
		return validateUnaryCollection(ctx, schema, value, path, seqItems)
	case KindMaybe:
		if isNilValue(value) {
			return nil
		}
		return validateProblems(ctx, schema.element, value, path)
	case KindTuple:
		return validateTuple(ctx, schema, value, path)
	case KindMapOf:
		return validateMapOf(ctx, schema, value, path)
	case KindOr:
		return validateOr(ctx, schema, value, path)
	case KindAnd:
		return validateAnd(ctx, schema, value, path)
	case KindEnum:
		if enumContains(schema.enumValues, value) {
			return nil
		}
		return []Problem{{
			Path:     path,
			Expected: schema.raw,
			Actual:   value,
			Message:  fmt.Sprintf("expected one of enum values, got %s", valueTypeName(value)),
		}}
	case KindPredicate:
		return validatePredicate(ctx, schema, value, path)
	default:
		return []Problem{{
			Path:     path,
			Expected: schema.raw,
			Actual:   value,
			Message:  fmt.Sprintf("unsupported schema kind %s", schema.kind),
		}}
	}
}

func validateUnaryCollection(ctx context.Context, schema *Schema, value types.Value, path Path, collect func(types.Value) ([]types.Value, bool)) []Problem {
	if !isCollectionOfKind(schema.kind, value) {
		return []Problem{collectionProblem(path, schema, value)}
	}
	if schema.element == nil {
		return nil
	}
	items, ok := collect(value)
	if !ok {
		return []Problem{collectionProblem(path, schema, value)}
	}
	var problems []Problem
	for i, item := range items {
		childProblems := validateProblems(ctx, schema.element, item, path.Push(indexValue(i)))
		if len(childProblems) > 0 {
			return childProblems
		}
		problems = append(problems, childProblems...)
	}
	return problems
}

func validateTuple(ctx context.Context, schema *Schema, value types.Value, path Path) []Problem {
	items, ok := seqLikeItems(value)
	if !ok {
		return []Problem{collectionProblem(path, schema, value)}
	}
	if len(items) != len(schema.items) {
		return []Problem{{
			Path:     path,
			Expected: schema.raw,
			Actual:   value,
			Message:  fmt.Sprintf("expected tuple of length %d, got %d", len(schema.items), len(items)),
		}}
	}
	var problems []Problem
	for i := range schema.items {
		childProblems := validateProblems(ctx, schema.items[i], items[i], path.Push(indexValue(i)))
		if len(childProblems) > 0 {
			return childProblems
		}
		problems = append(problems, childProblems...)
	}
	return problems
}

func validateMap(ctx context.Context, schema *Schema, value types.Value, path Path) []Problem {
	m, ok := value.(types.Map)
	if !ok {
		return []Problem{collectionProblem(path, schema, value)}
	}
	if len(schema.entries) == 0 {
		return nil
	}
	var problems []Problem
	for _, entry := range schema.entries {
		found := m.Find(entry.Key)
		if found == nil {
			return []Problem{{
				Path:     path.Push(entry.Key),
				Expected: entry.Schema.raw,
				Actual:   nil,
				Message:  fmt.Sprintf("missing key %s", entry.Key.String()),
			}}
		}
		childProblems := validateProblems(ctx, entry.Schema, found.Value, path.Push(entry.Key))
		if len(childProblems) > 0 {
			return childProblems
		}
		problems = append(problems, childProblems...)
	}
	return problems
}

func validateMapOf(ctx context.Context, schema *Schema, value types.Value, path Path) []Problem {
	m, ok := value.(types.Map)
	if !ok {
		return []Problem{collectionProblem(path, schema, value)}
	}
	keysSource := m.Keys()
	if keysSource == nil {
		return nil
	}
	keysSeq := keysSource.Seq()
	if keysSeq == nil {
		return nil
	}
	keys, ok := types.ToSlice(keysSeq)
	if !ok {
		return []Problem{collectionProblem(path, schema, value)}
	}
	var problems []Problem
	for _, key := range keys {
		if schema.keySchema != nil {
			childProblems := validateProblems(ctx, schema.keySchema, key, path.Push(key))
			if len(childProblems) > 0 {
				return childProblems
			}
		}
		actual := m.Get(key)
		childProblems := validateProblems(ctx, schema.valueSchema, actual, path.Push(key))
		if len(childProblems) > 0 {
			return childProblems
		}
		problems = append(problems, childProblems...)
	}
	return problems
}

func validateOr(ctx context.Context, schema *Schema, value types.Value, path Path) []Problem {
	if len(schema.items) == 0 {
		return nil
	}
	var problems []Problem
	for i, child := range schema.items {
		childProblems := validateProblems(ctx, child, value, path)
		if len(childProblems) == 0 {
			return nil
		}
		for j := range childProblems {
			if childProblems[j].Branch == nil {
				childProblems[j].Branch = intPtr(i)
			}
		}
		problems = append(problems, childProblems...)
	}
	if len(problems) == 0 {
		problems = []Problem{{
			Path:     path,
			Expected: schema.raw,
			Actual:   value,
			Message:  "no :or branches matched",
		}}
	}
	return problems
}

func validateAnd(ctx context.Context, schema *Schema, value types.Value, path Path) []Problem {
	var problems []Problem
	for i, child := range schema.items {
		childProblems := validateProblems(ctx, child, value, path)
		for j := range childProblems {
			if childProblems[j].Branch == nil {
				childProblems[j].Branch = intPtr(i)
			}
		}
		if len(childProblems) > 0 {
			return childProblems
		}
		problems = append(problems, childProblems...)
	}
	return problems
}

func validatePredicate(ctx context.Context, schema *Schema, value types.Value, path Path) []Problem {
	if schema.predicate == nil {
		return []Problem{{
			Path:     path,
			Expected: schema.raw,
			Actual:   value,
			Message:  "predicate schema has no callable",
		}}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := schema.predicate.Invoke(ctx, value)
	if err != nil {
		return []Problem{{
			Path:      path,
			Expected:  schema.raw,
			Actual:    value,
			Predicate: schema.predicate,
			Message:   err.Error(),
		}}
	}
	if types.IsTruthy(result) {
		return nil
	}
	return []Problem{{
		Path:      path,
		Expected:  schema.raw,
		Actual:    value,
		Predicate: schema.predicate,
		Message:   "predicate returned false",
	}}
}

func collectionProblem(path Path, schema *Schema, value types.Value) Problem {
	return Problem{
		Path:     path,
		Expected: expectedValue(schema),
		Actual:   value,
		Message:  fmt.Sprintf("expected %s, got %s", schema.kind, valueTypeName(value)),
	}
}

func scalarProblem(path Path, schema *Schema, value types.Value) Problem {
	return Problem{
		Path:     path,
		Expected: expectedValue(schema),
		Actual:   value,
		Message:  fmt.Sprintf("expected %s, got %s", schema.kind, valueTypeName(value)),
	}
}

func expectedValue(schema *Schema) types.Value {
	if schema == nil {
		return nil
	}
	if schema.raw != nil {
		return schema.raw
	}
	return atom.NewKeyword(string(schema.kind))
}

func isNilValue(value types.Value) bool {
	if value == nil {
		return true
	}
	return value.Type() == "nil"
}

func isFloatValue(value types.Value) bool {
	num, ok := value.(types.Number)
	if !ok {
		return false
	}
	if intValuer, ok := value.(types.Int64Valuer); ok {
		if _, exact := intValuer.Int64Value(); exact {
			return false
		}
	}
	if n, ok := value.(*atom.Number); ok {
		return n.FloatVal != nil
	}
	return num != nil
}

func isCollectionOfKind(kind Kind, value types.Value) bool {
	switch kind {
	case KindVector:
		_, ok := value.(*coll.Vector)
		return ok
	case KindSet:
		_, ok := value.(*coll.Set)
		return ok
	case KindList:
		_, ok := value.(types.List)
		return ok
	case KindSeq:
		_, ok := value.(types.Seq)
		return ok
	default:
		return false
	}
}

func vectorItems(value types.Value) ([]types.Value, bool) {
	v, ok := value.(*coll.Vector)
	if !ok {
		return nil, false
	}
	items := make([]types.Value, len(v.Items))
	copy(items, v.Items)
	return items, true
}

func setItems(value types.Value) ([]types.Value, bool) {
	s, ok := value.(*coll.Set)
	if !ok {
		return nil, false
	}
	return s.AllElements(), true
}

func listItems(value types.Value) ([]types.Value, bool) {
	l, ok := value.(types.List)
	if !ok {
		return nil, false
	}
	return types.ToSlice(l)
}

func seqItems(value types.Value) ([]types.Value, bool) {
	if seq, ok := value.(types.Seq); ok {
		return types.ToSlice(seq)
	}
	return nil, false
}

func seqLikeItems(value types.Value) ([]types.Value, bool) {
	if items, ok := listItems(value); ok {
		return items, true
	}
	if items, ok := vectorItems(value); ok {
		return items, true
	}
	if items, ok := seqItems(value); ok {
		return items, true
	}
	return nil, false
}

func enumContains(values []types.Value, candidate types.Value) bool {
	for _, val := range values {
		if types.Equal(val, candidate) {
			return true
		}
	}
	return false
}

func intPtr(v int) *int {
	return &v
}
