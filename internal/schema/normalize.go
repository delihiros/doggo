package schema

import (
	"fmt"
	"strings"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type NormalizeError struct {
	Path     Path
	Expected string
	Actual   types.Value
	Message  string
}

func (e *NormalizeError) Error() string {
	if e == nil {
		return "schema normalization failed"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Expected != "" || e.Actual != nil {
		return fmt.Sprintf("schema normalization failed at %s: expected %s, got %s", e.Path.String(), e.Expected, valueTypeName(e.Actual))
	}
	return "schema normalization failed"
}

// ExceptionData projects schema normalization failures into the canonical
// exception-data model so callers can preserve structured details through
// error wrapping and cause chains.
func (e *NormalizeError) ExceptionData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}

	data := map[string]any{}
	if len(e.Path) > 0 {
		data["path"] = pathData(e.Path)
	}
	if e.Expected != "" {
		data["expected"] = e.Expected
	}
	if e.Actual != nil {
		data["actual"] = e.Actual
		data["actual_type"] = e.Actual.Type()
	}

	return schemaExceptionData("SchemaNormalizeError", "schema normalization error", e.Error(), data, nil)
}

func Normalize(value types.Value) (*Schema, error) {
	return normalize(value, nil)
}

func normalize(value types.Value, path Path) (*Schema, error) {
	if value == nil {
		return nil, &NormalizeError{Path: path, Message: "schema cannot be nil"}
	}

	if kw, ok := value.(atom.Keyword); ok {
		return normalizeKeywordSchema(kw, value)
	}

	switch v := value.(type) {
	case *coll.Vector:
		return normalizeSequence(v.Items, path, value)
	case *coll.List:
		return normalizeSequence(v.Items, path, value)
	case types.Seq:
		items, ok := types.ToSlice(v)
		if !ok {
			return nil, &NormalizeError{Path: path, Expected: "schema vector or list", Actual: value}
		}
		return normalizeSequence(items, path, value)
	default:
		return nil, &NormalizeError{Path: path, Expected: "schema keyword or vector form", Actual: value}
	}
}

func normalizeKeywordSchema(kw atom.Keyword, raw types.Value) (*Schema, error) {
	switch string(kw) {
	case "any":
		return &Schema{kind: KindAny, raw: raw}, nil
	case "nil":
		return &Schema{kind: KindNil, raw: raw}, nil
	case "string":
		return &Schema{kind: KindString, raw: raw}, nil
	case "keyword":
		return &Schema{kind: KindKeyword, raw: raw}, nil
	case "symbol":
		return &Schema{kind: KindSymbol, raw: raw}, nil
	case "bool":
		return &Schema{kind: KindBool, raw: raw}, nil
	case "int":
		return &Schema{kind: KindInt, raw: raw}, nil
	case "float":
		return &Schema{kind: KindFloat, raw: raw}, nil
	case "number":
		return &Schema{kind: KindNumber, raw: raw}, nil
	case "map":
		return &Schema{kind: KindMap, raw: raw}, nil
	case "vector":
		return &Schema{kind: KindVector, raw: raw}, nil
	case "set":
		return &Schema{kind: KindSet, raw: raw}, nil
	case "list":
		return &Schema{kind: KindList, raw: raw}, nil
	case "seq":
		return &Schema{kind: KindSeq, raw: raw}, nil
	default:
		return nil, &NormalizeError{Message: fmt.Sprintf("unknown schema keyword %s", kw.String()), Actual: raw}
	}
}

func normalizeSequence(items []types.Value, path Path, raw types.Value) (*Schema, error) {
	if len(items) == 0 {
		return nil, &NormalizeError{Path: path, Message: "schema vector must not be empty"}
	}

	head, ok := items[0].(atom.Keyword)
	if !ok {
		return nil, &NormalizeError{Path: path, Expected: "schema operator keyword", Actual: items[0]}
	}

	switch string(head) {
	case "maybe":
		if len(items) != 2 {
			return nil, &NormalizeError{Path: path, Message: "maybe schema expects exactly one child schema"}
		}
		child, err := normalize(items[1], path)
		if err != nil {
			return nil, err
		}
		return &Schema{kind: KindMaybe, raw: raw, element: child}, nil
	case "vector":
		return normalizeUnaryCollection(KindVector, raw, items, path)
	case "set":
		return normalizeUnaryCollection(KindSet, raw, items, path)
	case "list":
		return normalizeUnaryCollection(KindList, raw, items, path)
	case "seq":
		return normalizeUnaryCollection(KindSeq, raw, items, path)
	case "tuple":
		children := make([]*Schema, 0, len(items)-1)
		for i, item := range items[1:] {
			child, err := normalize(item, path.Push(indexValue(i)))
			if err != nil {
				return nil, err
			}
			children = append(children, child)
		}
		return &Schema{kind: KindTuple, raw: raw, items: children}, nil
	case "map":
		entries := make([]MapEntry, 0, len(items)-1)
		for i, item := range items[1:] {
			pair, ok := schemaItems(item)
			if !ok || len(pair) != 2 {
				return nil, &NormalizeError{Path: path.Push(indexValue(i)), Message: "map schema entries must be 2-element vectors or lists", Actual: item}
			}
			child, err := normalize(pair[1], path.Push(pair[0]))
			if err != nil {
				return nil, err
			}
			entries = append(entries, MapEntry{Key: pair[0], Schema: child})
		}
		return &Schema{kind: KindMap, raw: raw, entries: entries}, nil
	case "map-of":
		if len(items) != 3 {
			return nil, &NormalizeError{Path: path, Message: "map-of schema expects key and value schemas"}
		}
		keySchema, err := normalize(items[1], path.Push(atom.NewKeyword("key")))
		if err != nil {
			return nil, err
		}
		valueSchema, err := normalize(items[2], path.Push(atom.NewKeyword("value")))
		if err != nil {
			return nil, err
		}
		return &Schema{kind: KindMapOf, raw: raw, keySchema: keySchema, valueSchema: valueSchema}, nil
	case "or":
		if len(items) < 2 {
			return nil, &NormalizeError{Path: path, Message: "or schema expects at least one child schema"}
		}
		children, err := normalizeChildren(items[1:], path)
		if err != nil {
			return nil, err
		}
		return &Schema{kind: KindOr, raw: raw, items: children}, nil
	case "and":
		if len(items) < 2 {
			return nil, &NormalizeError{Path: path, Message: "and schema expects at least one child schema"}
		}
		children, err := normalizeChildren(items[1:], path)
		if err != nil {
			return nil, err
		}
		return &Schema{kind: KindAnd, raw: raw, items: children}, nil
	case "enum":
		return &Schema{kind: KindEnum, raw: raw, enumValues: append([]types.Value(nil), items[1:]...)}, nil
	case "predicate":
		if len(items) != 2 {
			return nil, &NormalizeError{Path: path, Message: "predicate schema expects exactly one callable"}
		}
		fn, ok := items[1].(types.Fn)
		if !ok {
			return nil, &NormalizeError{Path: path.Push(atom.NewKeyword("predicate")), Expected: "callable", Actual: items[1]}
		}
		return &Schema{kind: KindPredicate, raw: raw, predicate: fn}, nil
	default:
		return nil, &NormalizeError{Path: path, Message: fmt.Sprintf("unknown schema operator %s", strings.TrimPrefix(head.String(), ":"))}
	}
}

func normalizeUnaryCollection(kind Kind, raw types.Value, items []types.Value, path Path) (*Schema, error) {
	if len(items) != 2 {
		return nil, &NormalizeError{Path: path, Message: fmt.Sprintf("%s schema expects exactly one child schema", kind)}
	}
	child, err := normalize(items[1], path)
	if err != nil {
		return nil, err
	}
	return &Schema{kind: kind, raw: raw, element: child}, nil
}

func normalizeChildren(items []types.Value, path Path) ([]*Schema, error) {
	children := make([]*Schema, 0, len(items))
	for i, item := range items {
		child, err := normalize(item, path.Push(indexValue(i)))
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return children, nil
}

func schemaItems(value types.Value) ([]types.Value, bool) {
	switch v := value.(type) {
	case *coll.Vector:
		items := make([]types.Value, len(v.Items))
		copy(items, v.Items)
		return items, true
	case *coll.List:
		items := make([]types.Value, len(v.Items))
		copy(items, v.Items)
		return items, true
	case types.Seq:
		return types.ToSlice(v)
	default:
		return nil, false
	}
}

func indexValue(i int) types.Value {
	return coll.RangeInt(int64(i))
}

func valueTypeName(value types.Value) string {
	if value == nil {
		return "nil"
	}
	return value.Type()
}
