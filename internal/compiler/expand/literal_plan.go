package expand

import (
	"fmt"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
)

type QuotedLiteralPlan struct {
	Span           span.Span
	Symbol         *QuotedLiteralSymbol
	ResolvedSymbol *QuotedLiteralResolvedSymbol
	Number         *QuotedLiteralNumber
	String         *QuotedLiteralString
	Keyword        *QuotedLiteralKeyword
	Regex          *QuotedLiteralRegex
	Character      *QuotedLiteralCharacter
	Bool           *QuotedLiteralBool
	Nil            *QuotedLiteralNil
	List           *QuotedLiteralList
	Vector         *QuotedLiteralVector
	Map            *QuotedLiteralMap
	Set            *QuotedLiteralSet
}

type QuotedLiteralSymbol struct {
	Name string
	Meta *MetadataLiteralPlan
}

type QuotedLiteralResolvedSymbol struct {
	Name       string
	Resolution sexpr.SymbolResolution
	Meta       *MetadataLiteralPlan
}

type QuotedLiteralNumber struct {
	Literal string
}

type QuotedLiteralString struct {
	Value string
}

type QuotedLiteralKeyword struct {
	Name string
}

type QuotedLiteralRegex struct {
	Pattern string
}

type QuotedLiteralCharacter struct {
	Literal string
}

type QuotedLiteralBool struct {
	Value bool
}

type QuotedLiteralNil struct{}

type QuotedLiteralList struct {
	Items []*QuotedLiteralPlan
	Meta  *MetadataLiteralPlan
}

type QuotedLiteralVector struct {
	Items []*QuotedLiteralPlan
	Meta  *MetadataLiteralPlan
}

type QuotedLiteralMapEntry struct {
	Key   *QuotedLiteralPlan
	Value *QuotedLiteralPlan
}

type QuotedLiteralMap struct {
	Entries []QuotedLiteralMapEntry
	Meta    *MetadataLiteralPlan
}

type QuotedLiteralSet struct {
	Items []*QuotedLiteralPlan
	Meta  *MetadataLiteralPlan
}

type MetadataLiteralPlan struct {
	Entries []MetadataLiteralEntry
}

type MetadataLiteralEntry struct {
	Key   *QuotedLiteralPlan
	Value *QuotedLiteralPlan
}

func BuildQuotedLiteralPlan(form sexpr.Form) (*QuotedLiteralPlan, error) {
	if form == nil {
		return &QuotedLiteralPlan{Nil: &QuotedLiteralNil{}}, nil
	}

	plan := &QuotedLiteralPlan{Span: form.Span()}
	meta, err := BuildMetadataLiteralPlan(form.Meta())
	if err != nil {
		return nil, err
	}

	switch current := form.(type) {
	case sexpr.Symbol:
		plan.Symbol = &QuotedLiteralSymbol{Name: current.Name, Meta: meta}
	case sexpr.ResolvedSymbol:
		plan.ResolvedSymbol = &QuotedLiteralResolvedSymbol{
			Name:       current.Name,
			Resolution: current.Resolution,
			Meta:       meta,
		}
	case sexpr.Number:
		plan.Number = &QuotedLiteralNumber{Literal: current.Literal}
	case sexpr.String:
		plan.String = &QuotedLiteralString{Value: current.Value}
	case sexpr.Keyword:
		plan.Keyword = &QuotedLiteralKeyword{Name: current.Name}
	case sexpr.Regex:
		plan.Regex = &QuotedLiteralRegex{Pattern: current.Pattern}
	case sexpr.Character:
		plan.Character = &QuotedLiteralCharacter{Literal: current.Literal}
	case sexpr.Bool:
		plan.Bool = &QuotedLiteralBool{Value: current.Value}
	case sexpr.Nil:
		plan.Nil = &QuotedLiteralNil{}
	case *sexpr.List:
		items, err := buildQuotedLiteralPlans(current.Items)
		if err != nil {
			return nil, err
		}
		plan.List = &QuotedLiteralList{Items: items, Meta: meta}
	case *sexpr.Vector:
		items, err := buildQuotedLiteralPlans(current.Items)
		if err != nil {
			return nil, err
		}
		plan.Vector = &QuotedLiteralVector{Items: items, Meta: meta}
	case *sexpr.Map:
		entries := make([]QuotedLiteralMapEntry, 0, len(current.Entries))
		for _, entry := range current.Entries {
			key, err := BuildQuotedLiteralPlan(entry.Key)
			if err != nil {
				return nil, err
			}
			value, err := BuildQuotedLiteralPlan(entry.Value)
			if err != nil {
				return nil, err
			}
			entries = append(entries, QuotedLiteralMapEntry{Key: key, Value: value})
		}
		plan.Map = &QuotedLiteralMap{Entries: entries, Meta: meta}
	case *sexpr.Set:
		items, err := buildQuotedLiteralPlans(current.Items)
		if err != nil {
			return nil, err
		}
		plan.Set = &QuotedLiteralSet{Items: items, Meta: meta}
	default:
		return nil, fmt.Errorf("unsupported quoted literal form %T", form)
	}

	return plan, nil
}

func BuildMetadataLiteralPlan(meta *sexpr.Map) (*MetadataLiteralPlan, error) {
	if meta == nil || len(meta.Entries) == 0 {
		return nil, nil
	}

	entries := make([]MetadataLiteralEntry, 0, len(meta.Entries))
	for _, entry := range meta.Entries {
		key, err := BuildQuotedLiteralPlan(entry.Key)
		if err != nil {
			return nil, err
		}
		value, err := BuildQuotedLiteralPlan(entry.Value)
		if err != nil {
			return nil, err
		}
		entries = append(entries, MetadataLiteralEntry{Key: key, Value: value})
	}

	return &MetadataLiteralPlan{Entries: entries}, nil
}

func buildQuotedLiteralPlans(forms []sexpr.Form) ([]*QuotedLiteralPlan, error) {
	if len(forms) == 0 {
		return nil, nil
	}
	plans := make([]*QuotedLiteralPlan, 0, len(forms))
	for _, form := range forms {
		plan, err := BuildQuotedLiteralPlan(form)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}
