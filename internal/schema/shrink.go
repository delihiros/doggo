package schema

import (
	"context"
	"fmt"
	"iter"
	"math"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

const maxNestedShrinkCandidatesPerChild = 8

type ShrinkError struct {
	Path    Path
	Schema  *Schema
	Value   types.Value
	Count   int
	Reason  string
	Message string
	Cause   error
}

func (e *ShrinkError) Error() string {
	if e == nil {
		return "schema shrinking failed"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Reason != "" {
		return e.Reason
	}
	return "schema shrinking failed"
}

func (e *ShrinkError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *ShrinkError) ExceptionData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}

	data := map[string]any{}
	if e.Schema != nil {
		data["schema"] = e.Schema.Raw()
		data["schema_kind"] = string(e.Schema.Kind())
	}
	if e.Value != nil {
		data["value"] = e.Value
	}
	if e.Reason != "" {
		data["reason"] = e.Reason
	}
	if len(e.Path) > 0 {
		data["path"] = pathData(e.Path)
	}
	if e.Count != 0 {
		data["count"] = e.Count
	}

	return schemaExceptionData("SchemaShrinkError", "schema shrinking error", e.Error(), data, e.Cause)
}

func Shrink(schema *Schema, value types.Value) (iter.Seq[types.Value], error) {
	return ShrinkWithContext(context.Background(), schema, value)
}

// ShrinkWithContext retains the caller context throughout recursive shrinking
// and validation of the input, branches, and candidates.
func ShrinkWithContext(ctx context.Context, schema *Schema, value types.Value) (iter.Seq[types.Value], error) {
	candidates, err := shrinkValue(ctx, schema, value, nil)
	if err != nil {
		return nil, err
	}
	return func(yield func(types.Value) bool) {
		for _, candidate := range candidates {
			if !yield(candidate) {
				return
			}
		}
	}, nil
}

func ShrinkSeq(schema *Schema, value types.Value, n int) ([]types.Value, error) {
	return ShrinkSeqWithContext(context.Background(), schema, value, n)
}

// ShrinkSeqWithContext retains the caller context while collecting candidates.
func ShrinkSeqWithContext(ctx context.Context, schema *Schema, value types.Value, n int) ([]types.Value, error) {
	if n < 0 {
		return nil, &ShrinkError{
			Schema:  schema,
			Value:   value,
			Count:   n,
			Reason:  "malformed shrink count",
			Message: fmt.Sprintf("shrink count must be non-negative, got %d", n),
		}
	}
	if n == 0 {
		return []types.Value{}, nil
	}

	stream, err := ShrinkWithContext(ctx, schema, value)
	if err != nil {
		return nil, err
	}

	candidates := make([]types.Value, 0, n)
	for candidate := range stream {
		candidates = append(candidates, candidate)
		if len(candidates) == n {
			break
		}
	}
	return candidates, nil
}

func shrinkValue(ctx context.Context, schema *Schema, value types.Value, path Path) ([]types.Value, error) {
	if schema == nil {
		return nil, &ShrinkError{
			Path:    path,
			Value:   value,
			Reason:  "nil schema",
			Message: "schema cannot be nil",
		}
	}
	if !ValidateWithContext(ctx, schema, value) {
		return nil, &ShrinkError{
			Path:    path,
			Schema:  schema,
			Value:   value,
			Reason:  "value does not satisfy schema for shrinking",
			Message: "value does not satisfy schema for shrinking",
		}
	}

	switch schema.kind {
	case KindAny, KindNil, KindKeyword, KindSymbol:
		return []types.Value{}, nil
	case KindBool:
		if types.IsTruthy(value) {
			return []types.Value{atom.False}, nil
		}
		return []types.Value{}, nil
	case KindInt:
		return shrinkInt(value), nil
	case KindFloat:
		return shrinkFloat(value), nil
	case KindNumber:
		if intValuer, ok := value.(types.Int64Valuer); ok {
			if _, exact := intValuer.Int64Value(); exact {
				return shrinkInt(value), nil
			}
		}
		return shrinkFloat(value), nil
	case KindString:
		return shrinkString(value), nil
	case KindMaybe:
		return shrinkMaybe(ctx, schema, value, path)
	case KindVector:
		return shrinkVector(ctx, schema, value, path)
	case KindList:
		return shrinkList(ctx, schema, value, path)
	case KindSet:
		return shrinkSet(ctx, schema, value, path)
	case KindTuple:
		return shrinkTuple(ctx, schema, value, path)
	case KindMap:
		return shrinkMap(ctx, schema, value, path)
	case KindMapOf:
		return shrinkMapOf(ctx, schema, value, path)
	case KindOr:
		return shrinkOr(ctx, schema, value, path)
	case KindEnum:
		return shrinkEnum(schema, value), nil
	case KindPredicate, KindAnd:
		return nil, &ShrinkError{
			Path:    path,
			Schema:  schema,
			Value:   value,
			Reason:  "unsupported shrink schema kind",
			Message: fmt.Sprintf("schema kind :%s is not shrinkable in the first slice", schema.kind),
		}
	default:
		return nil, &ShrinkError{
			Path:    path,
			Schema:  schema,
			Value:   value,
			Reason:  "unsupported shrink schema kind",
			Message: fmt.Sprintf("unsupported shrink schema kind %s", schema.kind),
		}
	}
}

func shrinkInt(value types.Value) []types.Value {
	intValuer, ok := value.(types.Int64Valuer)
	if !ok {
		return []types.Value{}
	}
	current, exact := intValuer.Int64Value()
	if !exact || current == 0 {
		return []types.Value{}
	}

	candidates := make([]types.Value, 0)
	appendUniqueValue(&candidates, atom.NewNumberInt(0))

	half := current / 2
	if half != 0 {
		appendUniqueValue(&candidates, atom.NewNumberInt(half))
	}

	step := int64(1)
	if current > 0 {
		for next := current - step; next > 0; step, next = step+1, current-step {
			appendUniqueValue(&candidates, atom.NewNumberInt(next))
		}
	} else {
		for next := current + step; next < 0; step, next = step+1, current+step {
			appendUniqueValue(&candidates, atom.NewNumberInt(next))
		}
	}
	return candidates
}

func shrinkFloat(value types.Value) []types.Value {
	num, ok := value.(types.Number)
	if !ok {
		return []types.Value{}
	}
	current := num.ToFloat64()
	if current == 0 {
		return []types.Value{}
	}

	candidates := make([]types.Value, 0, 3)
	appendUniqueValue(&candidates, atom.NewNumberFloat(0))
	appendUniqueValue(&candidates, atom.NewNumberFloat(current/2))

	if math.Abs(current) > 1 {
		if current > 0 {
			appendUniqueValue(&candidates, atom.NewNumberFloat(current-1))
		} else {
			appendUniqueValue(&candidates, atom.NewNumberFloat(current+1))
		}
	}
	return candidates
}

func shrinkString(value types.Value) []types.Value {
	str, ok := value.(atom.String)
	if !ok {
		return []types.Value{}
	}
	raw := str.Display()
	if raw == "" {
		return []types.Value{}
	}

	runes := []rune(raw)
	candidates := make([]types.Value, 0, 4)
	appendUniqueValue(&candidates, atom.NewString(""))
	if len(runes) > 1 {
		appendUniqueValue(&candidates, atom.NewString(string(runes[:len(runes)/2])))
		appendUniqueValue(&candidates, atom.NewString(string(runes[:len(runes)-1])))
		appendUniqueValue(&candidates, atom.NewString(string(runes[:1])))
	}
	return candidates
}

func shrinkMaybe(ctx context.Context, schema *Schema, value types.Value, path Path) ([]types.Value, error) {
	if isNilValue(value) {
		return []types.Value{}, nil
	}
	candidates := []types.Value{atom.NilValue}
	childCandidates, err := shrinkValue(ctx, schema.element, value, path.Push(atom.NewKeyword("value")))
	if err != nil {
		return nil, err
	}
	for _, candidate := range childCandidates {
		appendUniqueValue(&candidates, candidate)
	}
	return candidates, nil
}

func shrinkVector(ctx context.Context, schema *Schema, value types.Value, path Path) ([]types.Value, error) {
	items, ok := vectorItems(value)
	if !ok {
		return []types.Value{}, nil
	}
	candidates, err := shrinkCollectionLike(ctx, schema, items, path, func(next []types.Value) types.Value {
		return coll.NewVector(next...)
	})
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

func shrinkList(ctx context.Context, schema *Schema, value types.Value, path Path) ([]types.Value, error) {
	items, ok := listItems(value)
	if !ok {
		return []types.Value{}, nil
	}
	candidates, err := shrinkCollectionLike(ctx, schema, items, path, func(next []types.Value) types.Value {
		return coll.NewList(next...)
	})
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

func shrinkSet(ctx context.Context, schema *Schema, value types.Value, path Path) ([]types.Value, error) {
	items, ok := setItems(value)
	if !ok {
		return []types.Value{}, nil
	}

	candidates := make([]types.Value, 0)
	for i := range items {
		candidateItems := removeIndex(items, i)
		candidate := coll.NewSet(candidateItems...)
		if ValidateWithContext(ctx, schema, candidate) {
			appendUniqueValue(&candidates, candidate)
		}
	}

	for i, item := range items {
		childCandidates, err := shrinkValue(ctx, schema.element, item, path.Push(indexValue(i)))
		if err != nil {
			return nil, err
		}
		for _, child := range boundedChildCandidates(childCandidates) {
			next := replaceIndex(items, i, child)
			candidate := coll.NewSet(next...)
			if candidate.Count() != len(next) {
				continue
			}
			if ValidateWithContext(ctx, schema, candidate) {
				appendUniqueValue(&candidates, candidate)
			}
		}
	}
	return candidates, nil
}

func shrinkTuple(ctx context.Context, schema *Schema, value types.Value, path Path) ([]types.Value, error) {
	items, ok := seqLikeItems(value)
	if !ok {
		return []types.Value{}, nil
	}
	candidates := make([]types.Value, 0)
	for i, item := range items {
		childCandidates, err := shrinkValue(ctx, schema.items[i], item, path.Push(indexValue(i)))
		if err != nil {
			return nil, err
		}
		appendChildReplacementCandidates(ctx, &candidates, schema, childCandidates, func(child types.Value) types.Value {
			return coll.NewVector(replaceIndex(items, i, child)...)
		})
	}
	return candidates, nil
}

func shrinkMap(ctx context.Context, schema *Schema, value types.Value, path Path) ([]types.Value, error) {
	m, ok := value.(*coll.Map)
	if !ok {
		return []types.Value{}, nil
	}

	candidates := make([]types.Value, 0)
	for _, entry := range schema.entries {
		found := m.Find(entry.Key)
		if found == nil {
			continue
		}
		if entry.Schema != nil && entry.Schema.kind == KindMaybe && !isNilValue(found.Value) {
			candidate := m.Assoc(entry.Key, atom.NilValue).(*coll.Map)
			if ValidateWithContext(ctx, schema, candidate) {
				appendUniqueValue(&candidates, candidate)
			}
		}
	}

	for _, entry := range schema.entries {
		found := m.Find(entry.Key)
		if found == nil {
			continue
		}
		childCandidates, err := shrinkValue(ctx, entry.Schema, found.Value, path.Push(entry.Key))
		if err != nil {
			return nil, err
		}
		for _, child := range childCandidates {
			candidate := m.Assoc(entry.Key, child).(*coll.Map)
			if ValidateWithContext(ctx, schema, candidate) {
				appendUniqueValue(&candidates, candidate)
			}
		}
	}
	return candidates, nil
}

func shrinkMapOf(ctx context.Context, schema *Schema, value types.Value, path Path) ([]types.Value, error) {
	m, ok := value.(*coll.Map)
	if !ok {
		return []types.Value{}, nil
	}

	candidates := make([]types.Value, 0)
	for _, key := range m.Order {
		candidate := m.Dissoc(key).(*coll.Map)
		if ValidateWithContext(ctx, schema, candidate) {
			appendUniqueValue(&candidates, candidate)
		}
	}

	for _, key := range m.Order {
		found := m.Find(key)
		if found == nil {
			continue
		}

		valueCandidates, err := shrinkValue(ctx, schema.valueSchema, found.Value, path.Push(key).Push(atom.NewKeyword("value")))
		if err != nil {
			return nil, err
		}
		for _, child := range boundedChildCandidates(valueCandidates) {
			candidate := m.Assoc(key, child).(*coll.Map)
			if ValidateWithContext(ctx, schema, candidate) {
				appendUniqueValue(&candidates, candidate)
			}
		}

		keyCandidates, err := shrinkValue(ctx, schema.keySchema, key, path.Push(key).Push(atom.NewKeyword("key")))
		if err != nil {
			return nil, err
		}
		for _, nextKey := range boundedChildCandidates(keyCandidates) {
			if existing := m.Find(nextKey); existing != nil && !types.Equal(existing.Key, key) {
				continue
			}
			candidate := m.Dissoc(key).Assoc(nextKey, found.Value).(*coll.Map)
			if ValidateWithContext(ctx, schema, candidate) {
				appendUniqueValue(&candidates, candidate)
			}
		}
	}
	return candidates, nil
}

func shrinkOr(ctx context.Context, schema *Schema, value types.Value, path Path) ([]types.Value, error) {
	for i, child := range schema.items {
		if !ValidateWithContext(ctx, child, value) {
			continue
		}
		return shrinkValue(ctx, child, value, path.Push(indexValue(i)))
	}
	return []types.Value{}, nil
}

func shrinkEnum(schema *Schema, value types.Value) []types.Value {
	if len(schema.enumValues) == 0 {
		return []types.Value{}
	}
	if types.Equal(schema.enumValues[0], value) {
		return []types.Value{}
	}
	return []types.Value{schema.enumValues[0]}
}

func shrinkCollectionLike(ctx context.Context, schema *Schema, items []types.Value, path Path, build func([]types.Value) types.Value) ([]types.Value, error) {
	candidates := make([]types.Value, 0)

	for i := range items {
		candidate := build(removeIndex(items, i))
		if ValidateWithContext(ctx, schema, candidate) {
			appendUniqueValue(&candidates, candidate)
		}
	}

	for i, item := range items {
		childCandidates, err := shrinkValue(ctx, schema.element, item, path.Push(indexValue(i)))
		if err != nil {
			return nil, err
		}
		appendChildReplacementCandidates(ctx, &candidates, schema, childCandidates, func(child types.Value) types.Value {
			return build(replaceIndex(items, i, child))
		})
	}
	return candidates, nil
}

func appendChildReplacementCandidates(ctx context.Context, dst *[]types.Value, schema *Schema, childCandidates []types.Value, build func(types.Value) types.Value) {
	for _, child := range boundedChildCandidates(childCandidates) {
		candidate := build(child)
		if ValidateWithContext(ctx, schema, candidate) {
			appendUniqueValue(dst, candidate)
		}
	}
}

func boundedChildCandidates(candidates []types.Value) []types.Value {
	if len(candidates) <= maxNestedShrinkCandidatesPerChild {
		return candidates
	}
	return candidates[:maxNestedShrinkCandidatesPerChild]
}

func removeIndex(items []types.Value, idx int) []types.Value {
	next := make([]types.Value, 0, len(items)-1)
	next = append(next, items[:idx]...)
	next = append(next, items[idx+1:]...)
	return next
}

func replaceIndex(items []types.Value, idx int, value types.Value) []types.Value {
	next := make([]types.Value, len(items))
	copy(next, items)
	next[idx] = value
	return next
}

func appendUniqueValue(dst *[]types.Value, candidate types.Value) {
	for _, existing := range *dst {
		if types.Equal(existing, candidate) {
			return
		}
	}
	*dst = append(*dst, candidate)
}
