package macroconvert

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func FormToValue(form sexpr.Form) (types.Value, error) {
	if form == nil {
		return nil, nil
	}

	meta, err := toRuntimeMetadata(form.Meta())
	if err != nil {
		return nil, err
	}
	sp := form.Span()

	switch value := form.(type) {
	case sexpr.Symbol:
		if meta != nil {
			return types.NewSymbolWithMeta(value.Name, meta), nil
		}
		return types.NewSymbol(value.Name), nil
	case sexpr.ResolvedSymbol:
		resolution := runtimeResolution(value.Resolution)
		if meta != nil {
			return types.NewResolvedSymbolWithMeta(value.Name, resolution, meta), nil
		}
		return types.NewResolvedSymbol(value.Name, resolution), nil
	case sexpr.Number:
		out, err := runtimeNumber(value.Literal)
		if err != nil {
			return nil, err
		}
		return out, nil
	case sexpr.String:
		return atom.NewString(value.Value), nil
	case sexpr.Keyword:
		return atom.NewKeyword(value.Name), nil
	case sexpr.Regex:
		return atom.NewRegex(value.Pattern)
	case sexpr.Character:
		char, ok := atom.NewCharacterFromLiteral(value.Literal)
		if !ok {
			return nil, fmt.Errorf("invalid character literal %q", value.Literal)
		}
		return char, nil
	case sexpr.Bool:
		return atom.NewBoolean(value.Value), nil
	case sexpr.Nil:
		return atom.NilValue, nil
	case *sexpr.List:
		items, err := toRuntimeItems(value.Items)
		if err != nil {
			return nil, err
		}
		list := coll.NewListWithSpanAndPos(sp.Start.Line, sp.Start.Column, sp.StartOffset, sp.EndOffset, items...)
		if meta != nil {
			list.Metadata = meta
		}
		return list, nil
	case *sexpr.Vector:
		items, err := toRuntimeItems(value.Items)
		if err != nil {
			return nil, err
		}
		if meta != nil {
			return coll.NewVectorWithMetadata(items, meta), nil
		}
		return coll.NewVector(items...), nil
	case *sexpr.Set:
		items, err := toRuntimeItems(value.Items)
		if err != nil {
			return nil, err
		}
		if meta != nil {
			return coll.NewSetWithMetadata(items, meta), nil
		}
		return coll.NewSet(items...), nil
	case *sexpr.Map:
		pairs := make([]types.Value, 0, len(value.Entries)*2)
		for _, entry := range value.Entries {
			key, err := FormToValue(entry.Key)
			if err != nil {
				return nil, err
			}
			itemValue, err := FormToValue(entry.Value)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, key, itemValue)
		}
		out := coll.NewMapFromPairs(pairs)
		if meta != nil {
			out.Metadata = meta
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported sexpr form %T", form)
	}
}

func ValueToForm(value types.Value) (sexpr.Form, error) {
	if isNilValue(value) {
		return nil, nil
	}

	switch form := value.(type) {
	case types.Symbol:
		return sexpr.NewSymbol(string(form), span.Unknown(""), nil), nil
	case *types.SymbolWithMeta:
		meta, err := fromRuntimeMetadata(form.Meta)
		if err != nil {
			return nil, err
		}
		return sexpr.NewSymbol(form.Name, span.Unknown(""), meta), nil
	case *types.ResolvedSymbol:
		meta, err := fromRuntimeMetadata(form.Meta)
		if err != nil {
			return nil, err
		}
		return sexpr.NewResolvedSymbol(form.Name, sexprResolution(form.Resolution), span.Unknown(""), meta), nil
	case *atom.Number:
		return sexpr.NewNumber(form.String(), span.Unknown(""), nil), nil
	case atom.String:
		return sexpr.NewString(string(form), span.Unknown(""), nil), nil
	case atom.Keyword:
		return sexpr.NewKeyword(string(form), span.Unknown(""), nil), nil
	case *atom.Regex:
		return sexpr.NewRegex(form.Pattern, span.Unknown(""), nil), nil
	case atom.Character:
		return sexpr.NewCharacter(strings.TrimPrefix(form.String(), `\`), span.Unknown(""), nil), nil
	case atom.Boolean:
		return sexpr.NewBool(bool(form), span.Unknown(""), nil), nil
	case types.Nil:
		return sexpr.NewNil(span.Unknown(""), nil), nil
	case *coll.List:
		meta, err := fromRuntimeMetadata(form.Metadata)
		if err != nil {
			return nil, err
		}
		items, err := fromRuntimeItems(form.Items)
		if err != nil {
			return nil, err
		}
		return sexpr.NewList(items, spanFromPos(form.Line, form.Column), meta), nil
	case *coll.Vector:
		meta, err := fromRuntimeMetadata(form.Metadata)
		if err != nil {
			return nil, err
		}
		items, err := fromRuntimeItems(form.Items)
		if err != nil {
			return nil, err
		}
		return sexpr.NewVector(items, span.Unknown(""), meta), nil
	case *coll.Set:
		meta, err := fromRuntimeMetadata(form.Metadata)
		if err != nil {
			return nil, err
		}
		items, err := fromRuntimeItems(form.Order)
		if err != nil {
			return nil, err
		}
		return sexpr.NewSet(items, span.Unknown(""), meta), nil
	case *coll.Map:
		meta, err := fromRuntimeMetadata(form.Metadata)
		if err != nil {
			return nil, err
		}
		entries := make([]sexpr.MapEntry, 0, len(form.Order))
		for _, key := range form.Order {
			entry := form.Find(key)
			if entry == nil {
				continue
			}
			formKey, err := ValueToForm(entry.Key)
			if err != nil {
				return nil, err
			}
			formValue, err := ValueToForm(entry.Value)
			if err != nil {
				return nil, err
			}
			entries = append(entries, sexpr.MapEntry{Key: formKey, Value: formValue})
		}
		return sexpr.NewMap(entries, span.Unknown(""), meta), nil
	default:
		return nil, fmt.Errorf("unsupported runtime value %T", value)
	}
}

func toRuntimeItems(forms []sexpr.Form) ([]types.Value, error) {
	items := make([]types.Value, 0, len(forms))
	for _, form := range forms {
		value, err := FormToValue(form)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, nil
}

func fromRuntimeItems(values []types.Value) ([]sexpr.Form, error) {
	items := make([]sexpr.Form, 0, len(values))
	for _, value := range values {
		form, err := ValueToForm(value)
		if err != nil {
			return nil, err
		}
		items = append(items, form)
	}
	return items, nil
}

func toRuntimeMetadata(meta *sexpr.Map) (*coll.Map, error) {
	if meta == nil || len(meta.Entries) == 0 {
		return nil, nil
	}
	value, err := FormToValue(meta)
	if err != nil {
		return nil, err
	}
	runtimeMeta, ok := value.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("metadata must convert to *coll.Map, got %T", value)
	}
	return runtimeMeta, nil
}

func fromRuntimeMetadata(meta types.Map) (*sexpr.Map, error) {
	if meta == nil {
		return nil, nil
	}
	value, ok := meta.(types.Value)
	if !ok {
		return nil, fmt.Errorf("metadata must also be a value, got %T", meta)
	}
	if isNilValue(value) {
		return nil, nil
	}
	form, err := ValueToForm(value)
	if err != nil {
		return nil, err
	}
	if form == nil {
		return nil, nil
	}
	formMap, ok := form.(*sexpr.Map)
	if !ok {
		return nil, fmt.Errorf("metadata must convert to *sexpr.Map, got %T", form)
	}
	return formMap, nil
}

func runtimeNumber(literal string) (types.Value, error) {
	if intValue, err := strconv.ParseInt(literal, 10, 64); err == nil {
		return atom.NewNumberInt(intValue), nil
	}
	floatValue, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number literal %q", literal)
	}
	return atom.NewNumberFloat(floatValue), nil
}

func spanFromPos(line, column int) span.Span {
	if line <= 0 {
		line = 1
	}
	if column <= 0 {
		column = 1
	}
	return span.New("", line, column, line, column)
}

func isNilValue(value types.Value) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func runtimeResolution(resolution sexpr.SymbolResolution) types.SymbolResolution {
	return types.SymbolResolution{
		Kind:      types.SymbolResolutionKind(resolution.Kind),
		Index:     resolution.Index,
		Namespace: resolution.Namespace,
	}
}

func sexprResolution(resolution types.SymbolResolution) sexpr.SymbolResolution {
	return sexpr.SymbolResolution{
		Kind:      sexpr.SymbolResolutionKind(resolution.Kind),
		Index:     resolution.Index,
		Namespace: resolution.Namespace,
	}
}
