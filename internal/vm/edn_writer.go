package vm

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func ednWriteString(value types.Value) (string, error) {
	return ednWriteStringWithOptions(value, ednWriteOptions{indent: 2})
}

func ednWriteStringWithOptions(value types.Value, options ednWriteOptions) (string, error) {
	if err := validateEDNValue(value); err != nil {
		return "", err
	}

	var builder strings.Builder
	if err := appendEDNValue(&builder, value, options, 0); err != nil {
		return "", err
	}

	encoded := builder.String()
	if options.newline {
		encoded = strings.TrimRight(encoded, "\n") + "\n"
	}
	return encoded, nil
}

func appendEDNValue(builder *strings.Builder, value types.Value, options ednWriteOptions, depth int) error {
	if value == nil || value == atom.NilValue {
		builder.WriteString("nil")
		return nil
	}

	switch v := value.(type) {
	case atom.String:
		appendEDNString(builder, string(v))
		return nil
	case atom.Keyword:
		builder.WriteString(v.String())
		return nil
	case types.Number:
		builder.WriteString(ednNumberString(v))
		return nil
	case atom.Boolean:
		if bool(v) {
			builder.WriteString("true")
		} else {
			builder.WriteString("false")
		}
		return nil
	case atom.Character:
		builder.WriteString(v.String())
		return nil
	case *atom.Instant:
		builder.WriteString(v.String())
		return nil
	case *atom.UUID:
		builder.WriteString(v.String())
		return nil
	case types.Symbol:
		builder.WriteString(v.String())
		return nil
	case *types.ResolvedSymbol:
		builder.WriteString(v.String())
		return nil
	case *coll.List:
		return appendEDNCollection(builder, "(", ")", v.Items, options, depth)
	case *coll.Vector:
		return appendEDNCollection(builder, "[", "]", v.Items, options, depth)
	case *coll.Set:
		return appendEDNCollection(builder, "#{", "}", v.AllElements(), options, depth)
	case types.Map:
		return appendEDNMap(builder, v, options, depth)
	default:
		return fmt.Errorf("value of type %s cannot be encoded as EDN", value.Type())
	}
}

func appendEDNCollection(builder *strings.Builder, open, close string, items []types.Value, options ednWriteOptions, depth int) error {
	builder.WriteString(open)
	if len(items) == 0 {
		builder.WriteString(close)
		return nil
	}

	if !options.pretty {
		for index, item := range items {
			if index > 0 {
				builder.WriteString(" ")
			}
			if err := appendEDNValue(builder, item, options, depth); err != nil {
				return err
			}
		}
		builder.WriteString(close)
		return nil
	}

	for _, item := range items {
		builder.WriteByte('\n')
		appendEDNIndent(builder, options, depth+1)
		if err := appendEDNValue(builder, item, options, depth+1); err != nil {
			return err
		}
	}
	builder.WriteByte('\n')
	appendEDNIndent(builder, options, depth)
	builder.WriteString(close)
	return nil
}

func appendEDNMap(builder *strings.Builder, value types.Map, options ednWriteOptions, depth int) error {
	keys, err := ednMapKeysForWrite(value, options)
	if err != nil {
		return err
	}

	builder.WriteString("{")
	if len(keys) == 0 {
		builder.WriteString("}")
		return nil
	}

	if !options.pretty {
		for index, key := range keys {
			if index > 0 {
				builder.WriteString(", ")
			}
			if err := appendEDNValue(builder, key, options, depth); err != nil {
				return err
			}
			builder.WriteString(" ")
			if err := appendEDNValue(builder, value.Get(key), options, depth); err != nil {
				return err
			}
		}
		builder.WriteString("}")
		return nil
	}

	for _, key := range keys {
		builder.WriteByte('\n')
		appendEDNIndent(builder, options, depth+1)
		if err := appendEDNValue(builder, key, options, depth+1); err != nil {
			return err
		}
		builder.WriteString(" ")
		if err := appendEDNValue(builder, value.Get(key), options, depth+1); err != nil {
			return err
		}
	}
	builder.WriteByte('\n')
	appendEDNIndent(builder, options, depth)
	builder.WriteString("}")
	return nil
}

func ednMapKeysForWrite(value types.Map, options ednWriteOptions) ([]types.Value, error) {
	keys := sequableToSlice(value.Keys())
	if !options.sortKeys {
		return keys, nil
	}

	for _, key := range keys {
		if !ednIsSortableScalarKey(key) {
			return nil, fmt.Errorf("cannot sort map with non-scalar key of type %s", key.Type())
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})
	return keys, nil
}

func appendEDNIndent(builder *strings.Builder, options ednWriteOptions, depth int) {
	if options.indent <= 0 || depth <= 0 {
		return
	}
	builder.WriteString(strings.Repeat(" ", options.indent*depth))
}

func appendEDNString(builder *strings.Builder, value string) {
	builder.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\', '"':
			builder.WriteByte('\\')
			builder.WriteRune(r)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
				builder.WriteString(`\u`)
				builder.WriteString(fmt.Sprintf("%04x", r))
			} else {
				builder.WriteRune(r)
			}
		}
	}
	builder.WriteByte('"')
}

func ednNumberString(value types.Number) string {
	if value == nil {
		return "0"
	}
	if _, isInteger := types.Int64Value(value); !isInteger {
		floatValue := value.ToFloat64()
		switch {
		case math.IsNaN(floatValue):
			return "##NaN"
		case math.IsInf(floatValue, 1):
			return "##Inf"
		case math.IsInf(floatValue, -1):
			return "##-Inf"
		}
	}
	return value.String()
}

func validateEDNValue(v types.Value) error {
	if v == nil || v == atom.NilValue {
		return nil
	}
	if err := validateEDNMetadata(v); err != nil {
		return err
	}

	switch val := v.(type) {
	case types.Nil, atom.Boolean, atom.String, atom.Keyword, atom.Character, types.Number, *atom.Instant, *atom.UUID, types.Symbol:
		return nil
	case *types.SymbolWithMeta:
		return fmt.Errorf("metadata-bearing symbols are not EDN")
	case *types.ResolvedSymbol:
		if val.Meta != nil {
			return fmt.Errorf("metadata-bearing symbols are not EDN")
		}
		return nil
	case *coll.Record:
		return fmt.Errorf("record values are not EDN")
	case *coll.List:
		for _, item := range val.Items {
			if err := validateEDNValue(item); err != nil {
				return err
			}
		}
		return nil
	case *coll.Vector:
		for _, item := range val.Items {
			if err := validateEDNValue(item); err != nil {
				return err
			}
		}
		return nil
	case *coll.Set:
		for _, item := range val.AllElements() {
			if err := validateEDNValue(item); err != nil {
				return err
			}
		}
		return nil
	case types.Map:
		return validateEDNMap(val)
	default:
		return fmt.Errorf("value of type %s is not EDN", v.Type())
	}
}

func validateEDNMetadata(v types.Value) error {
	switch val := v.(type) {
	case *coll.List:
		if val.Metadata != nil {
			return fmt.Errorf("metadata-bearing lists are not EDN")
		}
	case *coll.Vector:
		if val.Metadata != nil {
			return fmt.Errorf("metadata-bearing vectors are not EDN")
		}
	case *coll.Set:
		if val.Metadata != nil {
			return fmt.Errorf("metadata-bearing sets are not EDN")
		}
	case *coll.Map:
		if val.Metadata != nil {
			return fmt.Errorf("metadata-bearing maps are not EDN")
		}
	case *coll.ArrayMap:
		if val.Metadata != nil {
			return fmt.Errorf("metadata-bearing maps are not EDN")
		}
	case *coll.SortedMap:
		if val.Metadata != nil {
			return fmt.Errorf("metadata-bearing maps are not EDN")
		}
	case *coll.StructMap:
		if val.Metadata != nil {
			return fmt.Errorf("metadata-bearing maps are not EDN")
		}
	case *coll.Record:
		if val.Metadata != nil {
			return fmt.Errorf("metadata-bearing records are not EDN")
		}
	}
	return nil
}

func validateEDNMap(m types.Map) error {
	seqVal := m.Seq()
	if seqVal == nil {
		return nil
	}

	seq, ok := seqVal.(types.Seq)
	if !ok {
		return fmt.Errorf("map value of type %s cannot be traversed as EDN", m.Type())
	}

	for seq != nil {
		item := seq.First()
		if item == nil {
			break
		}

		switch entry := item.(type) {
		case *types.MapEntry:
			if err := validateEDNValue(entry.Key); err != nil {
				return err
			}
			if err := validateEDNValue(entry.Value); err != nil {
				return err
			}
		case *coll.Vector:
			if entry.Count() != 2 {
				return fmt.Errorf("map entry vector must contain 2 items, got %d", entry.Count())
			}
			if err := validateEDNValue(entry.Nth(0)); err != nil {
				return err
			}
			if err := validateEDNValue(entry.Nth(1)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("map sequence contained non-entry value of type %s", item.Type())
		}

		seq = seq.Next()
	}

	return nil
}
