package emit

import (
	"strconv"

	"doggo/internal/compiler/expand"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func MaterializeQuotedLiteral(plan *expand.QuotedLiteralPlan) (types.Value, error) {
	if plan == nil {
		return nil, nil
	}

	switch {
	case plan.Symbol != nil:
		meta, err := MaterializeMetadata(plan.Symbol.Meta)
		if err != nil {
			return nil, err
		}
		if meta != nil {
			return types.NewSymbolWithMeta(plan.Symbol.Name, meta), nil
		}
		return types.NewSymbol(plan.Symbol.Name), nil
	case plan.ResolvedSymbol != nil:
		meta, err := MaterializeMetadata(plan.ResolvedSymbol.Meta)
		if err != nil {
			return nil, err
		}
		resolution := types.SymbolResolution{
			Kind:      types.SymbolResolutionKind(plan.ResolvedSymbol.Resolution.Kind),
			Index:     plan.ResolvedSymbol.Resolution.Index,
			Namespace: plan.ResolvedSymbol.Resolution.Namespace,
		}
		if meta != nil {
			return types.NewResolvedSymbolWithMeta(plan.ResolvedSymbol.Name, resolution, meta), nil
		}
		return types.NewResolvedSymbol(plan.ResolvedSymbol.Name, resolution), nil
	case plan.Number != nil:
		return runtimeNumberFromQuotedLiteral(plan.Number.Literal)
	case plan.String != nil:
		return atom.NewString(plan.String.Value), nil
	case plan.Keyword != nil:
		return atom.NewKeyword(plan.Keyword.Name), nil
	case plan.Regex != nil:
		return atom.NewRegex(plan.Regex.Pattern)
	case plan.Character != nil:
		character, ok := atom.NewCharacterFromLiteral(plan.Character.Literal)
		if !ok {
			return nil, strconv.ErrSyntax
		}
		return character, nil
	case plan.Bool != nil:
		if plan.Bool.Value {
			return atom.True, nil
		} else {
			return atom.False, nil
		}
	case plan.Nil != nil:
		return atom.NilValue, nil
	case plan.List != nil:
		items, err := materializeQuotedLiteralItems(plan.List.Items)
		if err != nil {
			return nil, err
		}
		list := coll.NewListWithSpanAndPos(plan.Span.Start.Line, plan.Span.Start.Column, plan.Span.StartOffset, plan.Span.EndOffset, items...)
		meta, err := MaterializeMetadata(plan.List.Meta)
		if err != nil {
			return nil, err
		}
		list.Metadata = meta
		return list, nil
	case plan.Vector != nil:
		items, err := materializeQuotedLiteralItems(plan.Vector.Items)
		if err != nil {
			return nil, err
		}
		meta, err := MaterializeMetadata(plan.Vector.Meta)
		if err != nil {
			return nil, err
		}
		return coll.NewVectorWithMetadata(items, meta), nil
	case plan.Map != nil:
		pairs := make([]types.Value, 0, len(plan.Map.Entries)*2)
		for _, entry := range plan.Map.Entries {
			key, err := MaterializeQuotedLiteral(entry.Key)
			if err != nil {
				return nil, err
			}
			value, err := MaterializeQuotedLiteral(entry.Value)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, key, value)
		}
		meta, err := MaterializeMetadata(plan.Map.Meta)
		if err != nil {
			return nil, err
		}
		m := coll.NewMapFromPairs(pairs)
		m.Metadata = meta
		return m, nil
	case plan.Set != nil:
		items, err := materializeQuotedLiteralItems(plan.Set.Items)
		if err != nil {
			return nil, err
		}
		meta, err := MaterializeMetadata(plan.Set.Meta)
		if err != nil {
			return nil, err
		}
		return coll.NewSetWithMetadata(items, meta), nil
	default:
		return nil, strconv.ErrSyntax
	}
}

func MaterializeMetadata(plan *expand.MetadataLiteralPlan) (*coll.Map, error) {
	if plan == nil || len(plan.Entries) == 0 {
		return nil, nil
	}

	pairs := make([]types.Value, 0, len(plan.Entries)*2)
	for _, entry := range plan.Entries {
		key, err := MaterializeQuotedLiteral(entry.Key)
		if err != nil {
			return nil, err
		}
		value, err := MaterializeQuotedLiteral(entry.Value)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, key, value)
	}

	return coll.NewMapFromPairs(pairs), nil
}

func runtimeNumberFromQuotedLiteral(literal string) (*atom.Number, error) {
	if intValue, err := strconv.ParseInt(literal, 10, 64); err == nil {
		return atom.NewNumberInt(intValue), nil
	}
	floatValue, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return nil, err
	}
	return atom.NewNumberFloat(floatValue), nil
}

func materializeQuotedLiteralItems(items []*expand.QuotedLiteralPlan) ([]types.Value, error) {
	if len(items) == 0 {
		return nil, nil
	}
	values := make([]types.Value, 0, len(items))
	for _, item := range items {
		value, err := MaterializeQuotedLiteral(item)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}
