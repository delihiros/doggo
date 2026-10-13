package lower

import (
	"fmt"

	"doggo/internal/syntax/form"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func ToValue(node *form.Node) (types.Value, error) {
	if node == nil {
		return nil, nil
	}

	var out types.Value
	switch node.Kind {
	case form.KindSymbol:
		out = types.NewSymbol(node.Text)
	case form.KindNumber:
		if node.Number.IsFloat {
			out = atom.NewNumberFloat(node.Number.Float64)
		} else {
			out = atom.NewNumberInt(node.Number.Int64)
		}
	case form.KindString:
		out = atom.NewString(node.Text)
	case form.KindKeyword:
		out = atom.NewKeyword(node.Text)
	case form.KindRegex:
		regex, err := atom.NewRegex(node.Text)
		if err != nil {
			return nil, err
		}
		out = regex
	case form.KindChar:
		out = atom.Character(node.Character.Value)
	case form.KindBoolean:
		if node.Boolean {
			out = atom.True
		} else {
			out = atom.False
		}
	case form.KindNil:
		out = atom.NilValue
	case form.KindList:
		items, err := toValues(node.Items)
		if err != nil {
			return nil, err
		}
		out = coll.NewListWithSpanAndPos(
			node.Span.Start.Line,
			node.Span.Start.Column,
			node.StartOffset,
			node.EndOffset,
			items...,
		)
	case form.KindVector:
		items, err := toValues(node.Items)
		if err != nil {
			return nil, err
		}
		out = coll.NewVector(items...)
	case form.KindSet:
		items, err := toValues(node.Items)
		if err != nil {
			return nil, err
		}
		out = coll.NewSet(items...)
	case form.KindMap:
		pairs := make([]types.Value, 0, len(node.Entries)*2)
		for _, entry := range node.Entries {
			key, err := ToValue(entry.Key)
			if err != nil {
				return nil, err
			}
			value, err := ToValue(entry.Value)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, key, value)
		}
		out = coll.NewMapFromPairs(pairs)
	default:
		return nil, fmt.Errorf("unsupported syntax node kind %q", node.Kind)
	}

	if node.Metadata == nil {
		return out, nil
	}

	metaValue, err := ToValue(node.Metadata)
	if err != nil {
		return nil, err
	}
	metaMap, ok := metaValue.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("metadata must lower to map, got %T", metaValue)
	}
	withMeta, err := coll.CopyWithMetadata(out, metaMap)
	if err != nil {
		return out, nil
	}
	return withMeta, nil
}

func ToValues(nodes []*form.Node) ([]types.Value, error) {
	return toValues(nodes)
}

func toValues(nodes []*form.Node) ([]types.Value, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	out := make([]types.Value, 0, len(nodes))
	for _, node := range nodes {
		value, err := ToValue(node)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}
