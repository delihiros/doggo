package expand

import (
	"fmt"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
)

type DestructurePlan struct {
	Span   span.Span
	Symbol *DestructureSymbolPlan
	Vector *DestructureVectorPlan
	Map    *DestructureMapPlan
}

type DestructureSymbolPlan struct {
	Span span.Span
	Name string
	Meta *MetadataLiteralPlan
}

type DestructureVectorPlan struct {
	Span  span.Span
	Items []*DestructurePlan
	As    *DestructureSymbolPlan
	Rest  *DestructureSymbolPlan
}

type DestructureMapPlan struct {
	Span     span.Span
	As       *DestructureSymbolPlan
	Defaults map[string]*QuotedLiteralPlan
	Keys     []*DestructureSymbolPlan
	Strs     []*DestructureSymbolPlan
	Bindings []DestructureMapBindingPlan
}

type DestructureMapBindingPlan struct {
	Name    string
	KeyPlan *QuotedLiteralPlan
}

func BuildDestructurePlan(form sexpr.Form) (*DestructurePlan, error) {
	if form == nil {
		return nil, nil
	}

	plan := &DestructurePlan{Span: form.Span()}
	switch current := form.(type) {
	case sexpr.Symbol:
		symbolPlan, err := buildDestructureSymbolPlan(current)
		if err != nil {
			return nil, err
		}
		plan.Symbol = symbolPlan
	case sexpr.ResolvedSymbol:
		symbolPlan, err := buildDestructureSymbolPlan(current)
		if err != nil {
			return nil, err
		}
		plan.Symbol = symbolPlan
	case *sexpr.Vector:
		vectorPlan, err := buildDestructureVectorPlan(current)
		if err != nil {
			return nil, err
		}
		plan.Vector = vectorPlan
	case *sexpr.Map:
		mapPlan, err := buildDestructureMapPlan(current)
		if err != nil {
			return nil, err
		}
		plan.Map = mapPlan
	default:
		return nil, fmt.Errorf("unsupported destructuring pattern %T", form)
	}

	return plan, nil
}

func buildDestructureSymbolPlan(form sexpr.Form) (*DestructureSymbolPlan, error) {
	name, ok := destructureSymbolName(form)
	if !ok {
		return nil, fmt.Errorf("unsupported destructuring symbol %T", form)
	}

	meta, err := BuildMetadataLiteralPlan(form.Meta())
	if err != nil {
		return nil, err
	}

	return &DestructureSymbolPlan{
		Span: form.Span(),
		Name: name,
		Meta: meta,
	}, nil
}

func buildDestructureVectorPlan(vec *sexpr.Vector) (*DestructureVectorPlan, error) {
	plan := &DestructureVectorPlan{Span: vec.Span()}
	for i := 0; i < len(vec.Items); i++ {
		item := vec.Items[i]
		if item == nil {
			continue
		}
		if kw, ok := item.(sexpr.Keyword); ok && kw.Name == "as" {
			if i+1 >= len(vec.Items) {
				return nil, fmt.Errorf(":as in destructuring vector requires a symbol")
			}
			asPlan, err := buildDestructureSymbolPlan(vec.Items[i+1])
			if err != nil {
				return nil, err
			}
			plan.As = asPlan
			i++
			continue
		}
		if symName, ok := destructureSymbolName(item); ok && symName == "&" {
			if i+1 >= len(vec.Items) {
				return nil, fmt.Errorf("& in destructuring vector requires a rest symbol")
			}
			restPlan, err := buildDestructureSymbolPlan(vec.Items[i+1])
			if err != nil {
				return nil, err
			}
			plan.Rest = restPlan
			i++
			continue
		}

		itemPlan, err := BuildDestructurePlan(item)
		if err != nil {
			return nil, err
		}
		plan.Items = append(plan.Items, itemPlan)
	}
	return plan, nil
}

func buildDestructureMapPlan(m *sexpr.Map) (*DestructureMapPlan, error) {
	plan := &DestructureMapPlan{Span: m.Span()}
	for _, entry := range m.Entries {
		if entry.Key == nil {
			continue
		}

		if kw, ok := entry.Key.(sexpr.Keyword); ok {
			switch kw.Name {
			case "as":
				asPlan, err := buildDestructureSymbolPlan(entry.Value)
				if err != nil {
					return nil, err
				}
				plan.As = asPlan
				continue
			case "or":
				if orMap, ok := entry.Value.(*sexpr.Map); ok {
					defaults, err := buildDestructureDefaults(orMap)
					if err != nil {
						return nil, err
					}
					plan.Defaults = defaults
				}
				continue
			case "keys":
				vec, ok := entry.Value.(*sexpr.Vector)
				if !ok {
					return nil, fmt.Errorf(":keys in destructuring map requires a vector")
				}
				for _, item := range vec.Items {
					symbolPlan, err := buildDestructureSymbolPlan(item)
					if err != nil {
						return nil, err
					}
					plan.Keys = append(plan.Keys, symbolPlan)
				}
				continue
			case "strs":
				vec, ok := entry.Value.(*sexpr.Vector)
				if !ok {
					return nil, fmt.Errorf(":strs in destructuring map requires a vector")
				}
				for _, item := range vec.Items {
					symbolPlan, err := buildDestructureSymbolPlan(item)
					if err != nil {
						return nil, err
					}
					plan.Strs = append(plan.Strs, symbolPlan)
				}
				continue
			}
		}

		name, ok := destructureSymbolName(entry.Key)
		if !ok {
			continue
		}
		plan.Bindings = append(plan.Bindings, DestructureMapBindingPlan{
			Name: name,
		})
		keyPlan, err := BuildQuotedLiteralPlan(entry.Value)
		if err != nil {
			return nil, err
		}
		plan.Bindings[len(plan.Bindings)-1].KeyPlan = keyPlan
	}
	return plan, nil
}

func buildDestructureDefaults(m *sexpr.Map) (map[string]*QuotedLiteralPlan, error) {
	if m == nil || len(m.Entries) == 0 {
		return nil, nil
	}

	defaults := make(map[string]*QuotedLiteralPlan, len(m.Entries))
	for _, entry := range m.Entries {
		name, ok := destructureSymbolName(entry.Key)
		if !ok {
			continue
		}
		plan, err := BuildQuotedLiteralPlan(entry.Value)
		if err != nil {
			return nil, err
		}
		defaults[name] = plan
	}
	return defaults, nil
}

func destructureSymbolName(form sexpr.Form) (string, bool) {
	switch current := form.(type) {
	case sexpr.Symbol:
		return current.Name, true
	case sexpr.ResolvedSymbol:
		return current.Name, true
	default:
		return "", false
	}
}
