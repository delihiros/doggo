package resolve

import (
	"fmt"
	"strings"

	"doggo/internal/compiler/hir"
	"doggo/internal/compiler/sexpr"
)

type Config struct{}

type scope struct {
	id       int
	parent   *scope
	depth    int
	nextSlot int
	bindings map[string]hir.Binding
}

type resolver struct {
	program       *hir.Program
	aliases       map[string]string
	scope         *scope
	nextScopeID   int
	nextBindingID int
}

func File(_ Config, file *sexpr.File) (*hir.Program, error) {
	program := &hir.Program{
		Namespace: "",
		Aliases:   map[string]string{},
		Forms:     []hir.Node{},
		Bindings:  []hir.Binding{},
	}
	if file == nil {
		return program, nil
	}

	program.Namespace = file.Namespace
	rootScope := &scope{
		id:       0,
		depth:    0,
		nextSlot: 0,
		bindings: map[string]hir.Binding{},
	}
	r := &resolver{
		program:     program,
		aliases:     program.Aliases,
		scope:       rootScope,
		nextScopeID: 1,
	}

	for _, form := range file.Forms {
		resolved, err := r.resolveTopLevel(form)
		if err != nil {
			return nil, err
		}
		program.Forms = append(program.Forms, hir.FormNode{Value: resolved})
	}

	return program, nil
}

func (r *resolver) resolveTopLevel(form sexpr.Form) (sexpr.Form, error) {
	list, ok := form.(*sexpr.List)
	if ok && len(list.Items) > 0 {
		if head, ok := list.Items[0].(sexpr.Symbol); ok && head.Name == "ns" {
			return r.resolveNS(list)
		}
	}
	return r.resolveForm(form)
}

func (r *resolver) resolveForm(form sexpr.Form) (sexpr.Form, error) {
	switch value := form.(type) {
	case sexpr.Symbol:
		return r.resolveSymbol(value), nil
	case sexpr.ResolvedSymbol, sexpr.Number, sexpr.String, sexpr.Keyword, sexpr.Regex, sexpr.Character, sexpr.Bool, sexpr.Nil:
		return form, nil
	case *sexpr.List:
		return r.resolveList(value)
	case *sexpr.Vector:
		items := make([]sexpr.Form, 0, len(value.Items))
		for _, item := range value.Items {
			resolved, err := r.resolveForm(item)
			if err != nil {
				return nil, err
			}
			items = append(items, resolved)
		}
		return sexpr.NewVector(items, value.Span(), normalizeMeta(value.Meta())), nil
	case *sexpr.Set:
		items := make([]sexpr.Form, 0, len(value.Items))
		for _, item := range value.Items {
			resolved, err := r.resolveForm(item)
			if err != nil {
				return nil, err
			}
			items = append(items, resolved)
		}
		return sexpr.NewSet(items, value.Span(), normalizeMeta(value.Meta())), nil
	case *sexpr.Map:
		entries := make([]sexpr.MapEntry, 0, len(value.Entries))
		for _, entry := range value.Entries {
			key, err := r.resolveForm(entry.Key)
			if err != nil {
				return nil, err
			}
			itemValue, err := r.resolveForm(entry.Value)
			if err != nil {
				return nil, err
			}
			entries = append(entries, sexpr.MapEntry{Key: key, Value: itemValue})
		}
		return sexpr.NewMap(entries, value.Span(), normalizeMeta(value.Meta())), nil
	default:
		return nil, fmt.Errorf("unsupported form %T", form)
	}
}

func (r *resolver) resolveList(list *sexpr.List) (sexpr.Form, error) {
	if list == nil || len(list.Items) == 0 {
		return list, nil
	}

	if head, ok := list.Items[0].(sexpr.Symbol); ok {
		switch head.Name {
		case "quote", "quasiquote":
			return list, nil
		case "let":
			return r.resolveLet(list)
		case "fn":
			return r.resolveFn(list)
		case "ns":
			return r.resolveNS(list)
		}
	}

	items := make([]sexpr.Form, 0, len(list.Items))
	for _, item := range list.Items {
		resolved, err := r.resolveForm(item)
		if err != nil {
			return nil, err
		}
		items = append(items, resolved)
	}
	return sexpr.NewList(items, list.Span(), normalizeMeta(list.Meta())), nil
}

func (r *resolver) resolveLet(list *sexpr.List) (sexpr.Form, error) {
	if len(list.Items) < 2 {
		return nil, fmt.Errorf("let requires bindings vector")
	}

	bindings, ok := list.Items[1].(*sexpr.Vector)
	if !ok {
		return nil, fmt.Errorf("let bindings must be a vector")
	}
	if len(bindings.Items)%2 != 0 {
		return nil, fmt.Errorf("let bindings must contain even pairs")
	}

	scope := r.pushBlockScope()
	defer r.popBlockScope(scope, true)

	resolvedBindings := make([]sexpr.Form, 0, len(bindings.Items))
	for i := 0; i < len(bindings.Items); i += 2 {
		value, err := r.resolveForm(bindings.Items[i+1])
		if err != nil {
			return nil, err
		}
		r.bindPattern(bindings.Items[i], hir.ScopeLocal)
		resolvedBindings = append(resolvedBindings, bindings.Items[i], value)
	}

	items := []sexpr.Form{
		list.Items[0],
		sexpr.NewVector(resolvedBindings, bindings.Span(), normalizeMeta(bindings.Meta())),
	}
	for _, bodyForm := range list.Items[2:] {
		resolved, err := r.resolveForm(bodyForm)
		if err != nil {
			return nil, err
		}
		items = append(items, resolved)
	}

	return sexpr.NewList(items, list.Span(), normalizeMeta(list.Meta())), nil
}

func (r *resolver) resolveFn(list *sexpr.List) (sexpr.Form, error) {
	if len(list.Items) < 2 {
		return nil, fmt.Errorf("fn requires parameters")
	}

	start := 1
	if _, ok := list.Items[1].(sexpr.Symbol); ok {
		start = 2
	}
	if start >= len(list.Items) {
		return nil, fmt.Errorf("fn requires parameters")
	}

	if params, ok := list.Items[start].(*sexpr.Vector); ok {
		arity, err := r.resolveFnArity(params, list.Items[start+1:])
		if err != nil {
			return nil, err
		}
		items := append([]sexpr.Form{}, list.Items[:start]...)
		items = append(items, arity[0])
		items = append(items, arity[1:]...)
		return sexpr.NewList(items, list.Span(), normalizeMeta(list.Meta())), nil
	}

	resolvedArities := append([]sexpr.Form{}, list.Items[:start]...)
	for _, item := range list.Items[start:] {
		arity, ok := item.(*sexpr.List)
		if !ok || len(arity.Items) == 0 {
			return nil, fmt.Errorf("fn arity must be a list")
		}
		params, ok := arity.Items[0].(*sexpr.Vector)
		if !ok {
			return nil, fmt.Errorf("fn parameters must be a vector")
		}
		resolved, err := r.resolveFnArity(params, arity.Items[1:])
		if err != nil {
			return nil, err
		}
		resolvedArities = append(resolvedArities, sexpr.NewList(resolved, arity.Span(), normalizeMeta(arity.Meta())))
	}

	return sexpr.NewList(resolvedArities, list.Span(), normalizeMeta(list.Meta())), nil
}

func (r *resolver) resolveFnArity(params *sexpr.Vector, body []sexpr.Form) ([]sexpr.Form, error) {
	scope := r.pushFunctionScope()
	defer r.popBlockScope(scope, false)

	r.bindPattern(params, hir.ScopeParam)

	items := []sexpr.Form{params}
	for _, bodyForm := range body {
		resolved, err := r.resolveForm(bodyForm)
		if err != nil {
			return nil, err
		}
		items = append(items, resolved)
	}
	return items, nil
}

func (r *resolver) resolveNS(list *sexpr.List) (sexpr.Form, error) {
	if len(list.Items) < 2 {
		return nil, fmt.Errorf("ns requires a namespace symbol")
	}

	name, ok := symbolName(list.Items[1])
	if !ok {
		return nil, fmt.Errorf("ns name must be a symbol")
	}
	r.program.Namespace = name

	for _, clauseForm := range list.Items[2:] {
		clause, ok := clauseForm.(*sexpr.List)
		if !ok || len(clause.Items) == 0 {
			continue
		}
		keyword, ok := clause.Items[0].(sexpr.Keyword)
		if !ok || keyword.Name != "require" {
			continue
		}
		r.recordRequireAliases(clause)
	}

	return list, nil
}

func (r *resolver) recordRequireAliases(clause *sexpr.List) {
	for _, specForm := range clause.Items[1:] {
		spec, ok := specForm.(*sexpr.Vector)
		if !ok || len(spec.Items) < 3 {
			continue
		}
		nsName, ok := symbolName(spec.Items[0])
		if !ok {
			continue
		}
		for i := 1; i+1 < len(spec.Items); i++ {
			keyword, ok := spec.Items[i].(sexpr.Keyword)
			if !ok || keyword.Name != "as" {
				continue
			}
			alias, ok := symbolName(spec.Items[i+1])
			if ok {
				r.aliases[alias] = nsName
			}
		}
	}
}

func (r *resolver) bindPattern(form sexpr.Form, kind hir.ScopeKind) {
	switch value := form.(type) {
	case sexpr.Symbol:
		r.bind(value.Name, kind)
	case sexpr.ResolvedSymbol:
		r.bind(value.Name, kind)
	case *sexpr.Vector:
		r.bindVectorPattern(value, kind)
	case *sexpr.Map:
		r.bindMapPattern(value, kind)
	}
}

func (r *resolver) bindVectorPattern(pattern *sexpr.Vector, kind hir.ScopeKind) {
	if pattern == nil {
		return
	}
	for i := 0; i < len(pattern.Items); i++ {
		if name, ok := symbolName(pattern.Items[i]); ok && name == "&" {
			if i+1 < len(pattern.Items) {
				r.bindPattern(pattern.Items[i+1], kind)
				i++
			}
			continue
		}
		if name, ok := keywordName(pattern.Items[i]); ok && name == "as" {
			if i+1 < len(pattern.Items) {
				r.bindPattern(pattern.Items[i+1], kind)
				i++
			}
			continue
		}
		r.bindPattern(pattern.Items[i], kind)
	}
}

func (r *resolver) bindMapPattern(pattern *sexpr.Map, kind hir.ScopeKind) {
	if pattern == nil {
		return
	}
	for _, entry := range pattern.Entries {
		if name, ok := keywordName(entry.Key); ok {
			switch name {
			case "as":
				r.bindPattern(entry.Value, kind)
			case "keys", "strs", "syms":
				if vec, ok := entry.Value.(*sexpr.Vector); ok {
					for _, item := range vec.Items {
						r.bindPattern(item, kind)
					}
				}
			case "or":
				// Defaults describe fallback values, not additional bindings.
			}
			continue
		}
		r.bindPattern(entry.Key, kind)
	}
}

func (r *resolver) bind(name string, kind hir.ScopeKind) {
	binding := hir.Binding{
		ID:      r.nextBindingID,
		ScopeID: r.scope.id,
		Name:    name,
		Depth:   r.scope.depth,
		Slot:    r.scope.nextSlot,
		Kind:    kind,
	}
	r.nextBindingID++
	r.scope.nextSlot++
	r.scope.bindings[name] = binding
	r.program.Bindings = append(r.program.Bindings, binding)
}

func (r *resolver) pushBlockScope() *scope {
	child := &scope{
		id:       r.nextScopeID,
		parent:   r.scope,
		depth:    r.scope.depth,
		nextSlot: r.scope.nextSlot,
		bindings: map[string]hir.Binding{},
	}
	r.nextScopeID++
	r.scope = child
	return child
}

func (r *resolver) pushFunctionScope() *scope {
	child := &scope{
		id:       r.nextScopeID,
		parent:   r.scope,
		depth:    r.scope.depth + 1,
		nextSlot: 0,
		bindings: map[string]hir.Binding{},
	}
	r.nextScopeID++
	r.scope = child
	return child
}

func (r *resolver) popBlockScope(current *scope, propagateSlots bool) {
	if current == nil || current.parent == nil {
		return
	}
	parent := current.parent
	if propagateSlots && current.nextSlot > parent.nextSlot {
		parent.nextSlot = current.nextSlot
	}
	r.scope = parent
}

func (r *resolver) resolveSymbol(sym sexpr.Symbol) sexpr.Form {
	ns, local, ok := splitQualifiedSymbol(sym.Name)
	if !ok {
		return sym
	}
	if resolved, ok := r.aliases[ns]; ok {
		return sexpr.NewSymbol(joinQualifiedSymbol(resolved, local), sym.Span(), normalizeMeta(sym.Meta()))
	}
	return sym
}

func normalizeMeta(meta *sexpr.Map) *sexpr.Map {
	if meta == nil || len(meta.Entries) == 0 {
		return nil
	}
	return meta
}

func splitQualifiedSymbol(name string) (string, string, bool) {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func joinQualifiedSymbol(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "/" + name
}

func symbolName(form sexpr.Form) (string, bool) {
	switch value := form.(type) {
	case sexpr.Symbol:
		return value.Name, true
	case sexpr.ResolvedSymbol:
		return value.Name, true
	default:
		return "", false
	}
}

func keywordName(form sexpr.Form) (string, bool) {
	value, ok := form.(sexpr.Keyword)
	if !ok {
		return "", false
	}
	return value.Name, true
}
