package compiler

import (
	"fmt"
	"strings"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/coll"
)

type quasiquoteItem struct {
	form   types.Value
	splice bool
}

type MacroNamespaceResolver func(localName string) string

type SyntaxQuoteExpander struct {
	symbolTable   *SymbolTable
	currentNs     string
	gensymStack   []map[string]string
	gensymCount   int
	macroResolver MacroNamespaceResolver
}

func NewSyntaxQuoteExpander(symbolTable *SymbolTable, macroResolver MacroNamespaceResolver) *SyntaxQuoteExpander {
	currentNs := DefaultNamespace
	if symbolTable != nil {
		currentNs = symbolTable.CurrentNamespace()
	}
	return &SyntaxQuoteExpander{
		symbolTable:   symbolTable,
		currentNs:     currentNs,
		macroResolver: macroResolver,
	}
}

// QuasiquoteExpand lowers a quasiquote body into regular Doggo forms.
// The returned form can be compiled or returned from macroexpand.
func QuasiquoteExpand(node types.Value, symbolTable *SymbolTable, macroResolver MacroNamespaceResolver) (types.Value, error) {
	return NewSyntaxQuoteExpander(symbolTable, macroResolver).Expand(node)
}

func (e *SyntaxQuoteExpander) Expand(node types.Value) (types.Value, error) {
	return e.withGensymScope(func() (types.Value, error) {
		return e.expand(node, 0)
	})
}

func (e *SyntaxQuoteExpander) expand(node types.Value, depth int) (types.Value, error) {
	switch n := node.(type) {
	case *types.SymbolWithMeta:
		return e.wrapWithMetadataForm(n.Meta, quoteForm(e.syntaxQuoteSymbol(n.Name)), depth)
	case types.Symbol:
		return quoteForm(e.syntaxQuoteSymbol(string(n))), nil
	case *types.ResolvedSymbol:
		return e.wrapWithMetadataForm(n.Meta, quoteForm(e.syntaxQuoteResolvedSymbol(n)), depth)
	case *coll.List:
		form, err := e.expandList(n, depth)
		if err != nil {
			return nil, err
		}
		return e.wrapWithMetadataForm(listMetadata(n), form, depth)
	case *coll.Vector:
		form, err := e.expandVector(n, depth)
		if err != nil {
			return nil, err
		}
		return e.wrapWithMetadataForm(n.Metadata, form, depth)
	case *coll.Set:
		form, err := e.expandSet(n, depth)
		if err != nil {
			return nil, err
		}
		return e.wrapWithMetadataForm(n.Metadata, form, depth)
	case types.Map:
		form, err := e.expandMap(n, depth)
		if err != nil {
			return nil, err
		}
		return e.wrapWithMetadataForm(mapMetadata(n), form, depth)
	default:
		return node, nil
	}
}

func (e *SyntaxQuoteExpander) expandList(list *coll.List, depth int) (types.Value, error) {
	if list == nil || len(list.Items) == 0 {
		return e.callCoreForm("list"), nil
	}

	if isQuasiquoteForm(list) {
		if len(list.Items) != 2 {
			return nil, fmt.Errorf("quasiquote requires exactly 1 argument")
		}
		body, err := e.withGensymScope(func() (types.Value, error) {
			return e.expand(list.Items[1], depth+1)
		})
		if err != nil {
			return nil, err
		}
		return e.dataListForm(list, body)
	}
	if isUnquoteForm(list) {
		if len(list.Items) != 2 {
			return nil, fmt.Errorf("unquote requires exactly 1 argument")
		}
		if depth == 0 {
			return list.Items[1], nil
		}
		body, err := e.expand(list.Items[1], depth-1)
		if err != nil {
			return nil, err
		}
		return e.dataListForm(list, body)
	}
	if isUnquoteSplicingForm(list) {
		if len(list.Items) != 2 {
			return nil, fmt.Errorf("unquote-splicing requires exactly 1 argument")
		}
		if depth == 0 {
			return nil, fmt.Errorf("unquote-splicing is not allowed at top level of quasiquote")
		}
		body, err := e.expand(list.Items[1], depth-1)
		if err != nil {
			return nil, err
		}
		return e.dataListForm(list, body)
	}

	items, err := e.expandItems(list.Items, depth)
	if err != nil {
		return nil, err
	}
	if !hasSplicing(items) {
		return e.callCoreForm("list", itemForms(items)...), nil
	}
	return e.callCoreForm("apply", e.intrinsicSymbol("list"), e.concatForm(items, true)), nil
}

func (e *SyntaxQuoteExpander) expandVector(vec *coll.Vector, depth int) (types.Value, error) {
	items, err := e.expandItems(vec.Items, depth)
	if err != nil {
		return nil, err
	}
	if !hasSplicing(items) {
		return coll.NewVector(itemForms(items)...), nil
	}
	return e.callCoreForm("vec", e.concatForm(items, false)), nil
}

func (e *SyntaxQuoteExpander) expandSet(set *coll.Set, depth int) (types.Value, error) {
	items, err := e.expandItems(set.AllElements(), depth)
	if err != nil {
		return nil, err
	}
	if !hasSplicing(items) {
		return coll.NewSet(itemForms(items)...), nil
	}
	return e.callCoreForm("set", e.concatForm(items, false)), nil
}

func (e *SyntaxQuoteExpander) expandMap(m types.Map, depth int) (types.Value, error) {
	pairs, err := mapPairs(m)
	if err != nil {
		return nil, err
	}

	forms := make([]types.Value, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		keyForm, keySplice, err := e.expandElement(pairs[i], depth)
		if err != nil {
			return nil, err
		}
		if keySplice {
			return nil, fmt.Errorf("unquote-splicing is not allowed in map literals")
		}

		valueForm, valueSplice, err := e.expandElement(pairs[i+1], depth)
		if err != nil {
			return nil, err
		}
		if valueSplice {
			return nil, fmt.Errorf("unquote-splicing is not allowed in map literals")
		}

		forms = append(forms, keyForm, valueForm)
	}

	return coll.NewMapFromPairs(forms), nil
}

func (e *SyntaxQuoteExpander) expandItems(items []types.Value, depth int) ([]quasiquoteItem, error) {
	result := make([]quasiquoteItem, 0, len(items))
	for _, item := range items {
		form, splice, err := e.expandElement(item, depth)
		if err != nil {
			return nil, err
		}
		result = append(result, quasiquoteItem{form: form, splice: splice})
	}
	return result, nil
}

func (e *SyntaxQuoteExpander) expandElement(node types.Value, depth int) (types.Value, bool, error) {
	list, ok := node.(*coll.List)
	if !ok {
		form, err := e.expand(node, depth)
		return form, false, err
	}

	if isUnquoteSplicingForm(list) {
		if len(list.Items) != 2 {
			return nil, false, fmt.Errorf("unquote-splicing requires exactly 1 argument")
		}
		if depth == 0 {
			return list.Items[1], true, nil
		}
		form, err := e.expand(list.Items[1], depth-1)
		if err != nil {
			return nil, false, err
		}
		dataForm, err := e.dataListForm(list, form)
		return dataForm, false, err
	}

	form, err := e.expand(list, depth)
	return form, false, err
}

func (e *SyntaxQuoteExpander) concatForm(items []quasiquoteItem, listSegments bool) types.Value {
	segments := make([]types.Value, 0, len(items))
	current := make([]types.Value, 0, len(items))

	flush := func() {
		if len(current) == 0 {
			return
		}
		if listSegments {
			segments = append(segments, e.callCoreForm("list", current...))
		} else {
			segment := make([]types.Value, len(current))
			copy(segment, current)
			segments = append(segments, coll.NewVector(segment...))
		}
		current = current[:0]
	}

	for _, item := range items {
		if item.splice {
			flush()
			segments = append(segments, item.form)
			continue
		}
		current = append(current, item.form)
	}

	flush()
	return e.callCoreForm("concat", segments...)
}

func mapPairs(m types.Map) ([]types.Value, error) {
	seqVal := m.Seq()
	if seqVal == nil {
		return nil, nil
	}

	seq, ok := seqVal.(types.Seq)
	if !ok {
		return nil, fmt.Errorf("map value of type %s cannot be traversed", m.Type())
	}

	pairs := []types.Value{}
	for seq != nil {
		item := seq.First()
		if item == nil {
			break
		}
		switch entry := item.(type) {
		case *types.MapEntry:
			pairs = append(pairs, entry.Key, entry.Value)
		case *coll.Vector:
			if entry.Count() != 2 {
				return nil, fmt.Errorf("map entry vector must contain 2 items, got %d", entry.Count())
			}
			pairs = append(pairs, entry.Nth(0), entry.Nth(1))
		default:
			return nil, fmt.Errorf("map sequence contained non-entry value of type %s", item.Type())
		}
		seq = seq.Next()
	}

	return pairs, nil
}

func (e *SyntaxQuoteExpander) dataListForm(list *coll.List, body types.Value) (types.Value, error) {
	if list == nil || len(list.Items) == 0 {
		return nil, fmt.Errorf("quoted data list requires a head symbol")
	}
	head, ok := types.AsSymbol(list.Items[0])
	if !ok {
		return nil, fmt.Errorf("quoted data list head must be a symbol")
	}
	return e.callCoreForm("list", quoteForm(head), body), nil
}

func quoteForm(value types.Value) types.Value {
	return coll.NewList(types.Symbol("quote"), value)
}

func (e *SyntaxQuoteExpander) callCoreForm(name string, args ...types.Value) types.Value {
	items := make([]types.Value, 0, len(args)+1)
	items = append(items, e.intrinsicSymbol(name))
	items = append(items, args...)
	return coll.NewList(items...)
}

func (e *SyntaxQuoteExpander) intrinsicSymbol(name string) types.Value {
	qualified := QualifySymbol(StdlibCoreNamespace, name)
	if e.symbolTable == nil {
		return types.Symbol(qualified)
	}
	if sym, ok := e.symbolTable.ResolveIntrinsic(name); ok {
		return resolvedSymbolFor(qualified, sym)
	}
	return types.Symbol(qualified)
}

func (e *SyntaxQuoteExpander) syntaxQuoteResolvedSymbol(sym *types.ResolvedSymbol) types.Value {
	if sym == nil {
		return nil
	}
	clone := *sym
	clone.Meta = nil
	return &clone
}

func (e *SyntaxQuoteExpander) syntaxQuoteSymbol(name string) types.Value {
	if name == "" {
		return types.Symbol("")
	}

	if ns, local := ParseQualifiedSymbol(name); ns != "" {
		resolvedNs := ns
		if e.symbolTable != nil {
			resolvedNs = e.symbolTable.ResolveAlias(ns)
		}
		qualified := QualifySymbol(resolvedNs, local)
		if e.symbolTable != nil {
			if sym, ok := e.symbolTable.Resolve(name); ok {
				return resolvedSymbolFor(qualified, sym)
			}
		}
		return types.Symbol(qualified)
	}

	if isAutoGensym(name) {
		return types.Symbol(e.resolveAutoGensym(name))
	}

	if sourceQualified := e.resolveMacroOrigin(name); sourceQualified != "" {
		return types.Symbol(sourceQualified)
	}
	if sourceQualified := e.resolveVarOrigin(name); sourceQualified != "" {
		if e.symbolTable != nil {
			if sym, ok := e.symbolTable.Resolve(name); ok {
				return resolvedSymbolFor(sourceQualified, sym)
			}
		}
		return types.Symbol(sourceQualified)
	}

	if e.symbolTable != nil {
		if sym, ok := e.symbolTable.Resolve(name); ok {
			switch sym.Scope {
			case BuiltinScope:
				return resolvedSymbolFor(QualifySymbol(StdlibCoreNamespace, name), sym)
			case GlobalScope, ProtectedGlobalScope:
				targetNs := sym.Namespace
				if targetNs == CoreNamespace || targetNs == StdlibCoreNamespace {
					targetNs = StdlibCoreNamespace
				}
				if targetNs == "" {
					targetNs = e.currentNs
				}
				return resolvedSymbolFor(QualifySymbol(targetNs, name), sym)
			case LocalScope, FreeScope:
				return types.Symbol(QualifySymbol(e.currentNs, name))
			}
		}
	}

	if isLiteralSyntaxQuoteSymbol(name) {
		return types.Symbol(name)
	}
	return types.Symbol(QualifySymbol(e.currentNs, name))
}

func (e *SyntaxQuoteExpander) resolveVarOrigin(localName string) string {
	if e.symbolTable == nil {
		return ""
	}
	return e.symbolTable.ResolveVarRefer(localName)
}

func (e *SyntaxQuoteExpander) resolveMacroOrigin(localName string) string {
	if e.macroResolver != nil {
		if qualified := e.macroResolver(localName); qualified != "" {
			return qualified
		}
	}
	if e.symbolTable == nil {
		return ""
	}
	return e.symbolTable.ResolveMacroRefer(localName)
}

func (e *SyntaxQuoteExpander) withGensymScope(fn func() (types.Value, error)) (types.Value, error) {
	e.gensymStack = append(e.gensymStack, make(map[string]string))
	defer func() {
		e.gensymStack = e.gensymStack[:len(e.gensymStack)-1]
	}()
	return fn()
}

func (e *SyntaxQuoteExpander) resolveAutoGensym(name string) string {
	if len(e.gensymStack) == 0 {
		e.gensymCount++
		return fmt.Sprintf("%s__%d__auto__", strings.TrimSuffix(name, "#"), e.gensymCount)
	}

	scope := e.gensymStack[len(e.gensymStack)-1]
	if generated, ok := scope[name]; ok {
		return generated
	}

	e.gensymCount++
	generated := fmt.Sprintf("%s__%d__auto__", strings.TrimSuffix(name, "#"), e.gensymCount)
	scope[name] = generated
	return generated
}

func resolvedSymbolFor(name string, sym Symbol) types.Value {
	switch sym.Scope {
	case BuiltinScope:
		return types.NewResolvedSymbol(name, types.SymbolResolution{
			Kind:      types.BuiltinSymbolResolution,
			Index:     sym.Index,
			Namespace: StdlibCoreNamespace,
		})
	case GlobalScope, ProtectedGlobalScope:
		ns := parseResolvedNamespace(name)
		return types.NewResolvedSymbol(name, types.SymbolResolution{
			Kind:      types.GlobalSymbolResolution,
			Index:     sym.Index,
			Namespace: ns,
		})
	default:
		return types.Symbol(name)
	}
}

func parseResolvedNamespace(name string) string {
	ns, _ := ParseQualifiedSymbol(name)
	return ns
}

func itemForms(items []quasiquoteItem) []types.Value {
	forms := make([]types.Value, 0, len(items))
	for _, item := range items {
		forms = append(forms, item.form)
	}
	return forms
}

func hasSplicing(items []quasiquoteItem) bool {
	for _, item := range items {
		if item.splice {
			return true
		}
	}
	return false
}

func isQuasiquoteForm(list *coll.List) bool {
	return hasHeadSymbol(list, "quasiquote")
}

func isUnquoteForm(list *coll.List) bool {
	return hasHeadSymbol(list, "unquote", "~")
}

func isUnquoteSplicingForm(list *coll.List) bool {
	return hasHeadSymbol(list, "unquote-splicing", "~@", "splice-unquote")
}

func hasHeadSymbol(list *coll.List, names ...string) bool {
	if list == nil || len(list.Items) == 0 {
		return false
	}
	headName, ok := types.SymbolName(list.Items[0])
	if !ok {
		return false
	}
	for _, name := range names {
		if headName == name {
			return true
		}
	}
	return false
}

func (e *SyntaxQuoteExpander) wrapWithMetadataForm(metadata types.Map, form types.Value, depth int) (types.Value, error) {
	if isNilMetadata(metadata) {
		return form, nil
	}
	metaForm, err := e.expand(metadata, depth)
	if err != nil {
		return nil, err
	}
	return e.callCoreForm("with-meta", form, metaForm), nil
}

func listMetadata(list *coll.List) types.Map {
	if list == nil {
		return nil
	}
	return list.Metadata
}

func mapMetadata(m types.Map) types.Map {
	switch val := m.(type) {
	case *coll.Map:
		if val == nil {
			return nil
		}
		return val.Metadata
	case *coll.ArrayMap:
		if val == nil {
			return nil
		}
		return val.Metadata
	case *coll.SortedMap:
		if val == nil {
			return nil
		}
		return val.Metadata
	case *coll.StructMap:
		if val == nil {
			return nil
		}
		return val.Metadata
	default:
		return nil
	}
}

func isNilMetadata(metadata types.Map) bool {
	switch val := metadata.(type) {
	case nil:
		return true
	case *coll.Map:
		return val == nil
	case *coll.ArrayMap:
		return val == nil
	case *coll.SortedMap:
		return val == nil
	case *coll.StructMap:
		return val == nil
	default:
		return false
	}
}

func isLiteralSyntaxQuoteSymbol(name string) bool {
	switch name {
	case "quote", "quasiquote", "unquote", "unquote-splicing", "splice-unquote",
		"&",
		"if", "do", "let", "letfn", "loop", "recur",
		"and", "or",
		"def", "defmacro", "fn",
		"throw", "try", "catch", "finally",
		"ns", "in-ns",
		"import", ".", "new", "reify",
		"go", "dosync",
		"defmulti", "defmethod",
		"defrecord", "deftype",
		"var":
		return true
	default:
		return false
	}
}

func isAutoGensym(name string) bool {
	return strings.HasSuffix(name, "#") && !strings.Contains(name, "/")
}

type formQuasiquoteItem struct {
	form   sexpr.Form
	splice bool
}

type SyntaxQuoteFormExpander struct {
	symbolTable   *SymbolTable
	currentNs     string
	gensymStack   []map[string]string
	gensymCount   int
	macroResolver MacroNamespaceResolver
}

func NewSyntaxQuoteFormExpander(symbolTable *SymbolTable, macroResolver MacroNamespaceResolver) *SyntaxQuoteFormExpander {
	currentNs := DefaultNamespace
	if symbolTable != nil {
		currentNs = symbolTable.CurrentNamespace()
	}
	return &SyntaxQuoteFormExpander{
		symbolTable:   symbolTable,
		currentNs:     currentNs,
		macroResolver: macroResolver,
	}
}

func QuasiquoteExpandForm(node sexpr.Form, symbolTable *SymbolTable, macroResolver MacroNamespaceResolver) (sexpr.Form, error) {
	return NewSyntaxQuoteFormExpander(symbolTable, macroResolver).Expand(node)
}

func (e *SyntaxQuoteFormExpander) Expand(node sexpr.Form) (sexpr.Form, error) {
	return e.withGensymScope(func() (sexpr.Form, error) {
		return e.expand(node, 0)
	})
}

func (e *SyntaxQuoteFormExpander) expand(node sexpr.Form, depth int) (sexpr.Form, error) {
	switch n := node.(type) {
	case sexpr.Symbol:
		return e.wrapWithMetadataForm(n.Meta(), quoteFormForm(e.syntaxQuoteSymbol(n.Name)), depth)
	case sexpr.ResolvedSymbol:
		return e.wrapWithMetadataForm(n.Meta(), quoteFormForm(e.syntaxQuoteResolvedSymbol(n)), depth)
	case sexpr.Number, sexpr.String, sexpr.Keyword, sexpr.Regex, sexpr.Character, sexpr.Bool, sexpr.Nil:
		return node, nil
	case *sexpr.List:
		form, err := e.expandList(n, depth)
		if err != nil {
			return nil, err
		}
		return e.wrapWithMetadataForm(n.Meta(), form, depth)
	case *sexpr.Vector:
		form, err := e.expandVector(n, depth)
		if err != nil {
			return nil, err
		}
		return e.wrapWithMetadataForm(n.Meta(), form, depth)
	case *sexpr.Set:
		form, err := e.expandSet(n, depth)
		if err != nil {
			return nil, err
		}
		return e.wrapWithMetadataForm(n.Meta(), form, depth)
	case *sexpr.Map:
		form, err := e.expandMap(n, depth)
		if err != nil {
			return nil, err
		}
		return e.wrapWithMetadataForm(n.Meta(), form, depth)
	default:
		return node, nil
	}
}

func (e *SyntaxQuoteFormExpander) expandList(list *sexpr.List, depth int) (sexpr.Form, error) {
	if list == nil || len(list.Items) == 0 {
		return e.callCoreForm("list"), nil
	}

	if isQuasiquoteFormForm(list) {
		if len(list.Items) != 2 {
			return nil, fmt.Errorf("quasiquote requires exactly 1 argument")
		}
		body, err := e.withGensymScope(func() (sexpr.Form, error) {
			return e.expand(list.Items[1], depth+1)
		})
		if err != nil {
			return nil, err
		}
		return e.dataListForm(list, body)
	}
	if isUnquoteFormForm(list) {
		if len(list.Items) != 2 {
			return nil, fmt.Errorf("unquote requires exactly 1 argument")
		}
		if depth == 0 {
			return list.Items[1], nil
		}
		body, err := e.expand(list.Items[1], depth-1)
		if err != nil {
			return nil, err
		}
		return e.dataListForm(list, body)
	}
	if isUnquoteSplicingFormForm(list) {
		if len(list.Items) != 2 {
			return nil, fmt.Errorf("unquote-splicing requires exactly 1 argument")
		}
		if depth == 0 {
			return nil, fmt.Errorf("unquote-splicing is not allowed at top level of quasiquote")
		}
		body, err := e.expand(list.Items[1], depth-1)
		if err != nil {
			return nil, err
		}
		return e.dataListForm(list, body)
	}

	items, err := e.expandItems(list.Items, depth)
	if err != nil {
		return nil, err
	}
	if !hasSplicingForms(items) {
		return e.callCoreForm("list", itemFormsFromPlans(items)...), nil
	}
	return e.callCoreForm("apply", e.intrinsicFormSymbol("list"), e.concatForm(items, true)), nil
}

func (e *SyntaxQuoteFormExpander) expandVector(vec *sexpr.Vector, depth int) (sexpr.Form, error) {
	items, err := e.expandItems(vec.Items, depth)
	if err != nil {
		return nil, err
	}
	if !hasSplicingForms(items) {
		return sexpr.NewVector(itemFormsFromPlans(items), span.Unknown(""), nil), nil
	}
	return e.callCoreForm("vec", e.concatForm(items, false)), nil
}

func (e *SyntaxQuoteFormExpander) expandSet(set *sexpr.Set, depth int) (sexpr.Form, error) {
	items, err := e.expandItems(set.Items, depth)
	if err != nil {
		return nil, err
	}
	if !hasSplicingForms(items) {
		return sexpr.NewSet(itemFormsFromPlans(items), span.Unknown(""), nil), nil
	}
	return e.callCoreForm("set", e.concatForm(items, false)), nil
}

func (e *SyntaxQuoteFormExpander) expandMap(m *sexpr.Map, depth int) (sexpr.Form, error) {
	entries := make([]sexpr.MapEntry, 0, len(m.Entries))
	for _, entry := range m.Entries {
		keyForm, keySplice, err := e.expandElement(entry.Key, depth)
		if err != nil {
			return nil, err
		}
		if keySplice {
			return nil, fmt.Errorf("unquote-splicing is not allowed in map literals")
		}

		valueForm, valueSplice, err := e.expandElement(entry.Value, depth)
		if err != nil {
			return nil, err
		}
		if valueSplice {
			return nil, fmt.Errorf("unquote-splicing is not allowed in map literals")
		}

		entries = append(entries, sexpr.MapEntry{Key: keyForm, Value: valueForm})
	}

	return sexpr.NewMap(entries, span.Unknown(""), nil), nil
}

func (e *SyntaxQuoteFormExpander) expandItems(items []sexpr.Form, depth int) ([]formQuasiquoteItem, error) {
	result := make([]formQuasiquoteItem, 0, len(items))
	for _, item := range items {
		form, splice, err := e.expandElement(item, depth)
		if err != nil {
			return nil, err
		}
		result = append(result, formQuasiquoteItem{form: form, splice: splice})
	}
	return result, nil
}

func (e *SyntaxQuoteFormExpander) expandElement(node sexpr.Form, depth int) (sexpr.Form, bool, error) {
	list, ok := node.(*sexpr.List)
	if !ok {
		form, err := e.expand(node, depth)
		return form, false, err
	}

	if isUnquoteSplicingFormForm(list) {
		if len(list.Items) != 2 {
			return nil, false, fmt.Errorf("unquote-splicing requires exactly 1 argument")
		}
		if depth == 0 {
			return list.Items[1], true, nil
		}
		form, err := e.expand(list.Items[1], depth-1)
		if err != nil {
			return nil, false, err
		}
		dataForm, err := e.dataListForm(list, form)
		return dataForm, false, err
	}

	form, err := e.expand(list, depth)
	return form, false, err
}

func (e *SyntaxQuoteFormExpander) concatForm(items []formQuasiquoteItem, listSegments bool) sexpr.Form {
	segments := make([]sexpr.Form, 0, len(items))
	current := make([]sexpr.Form, 0, len(items))

	flush := func() {
		if len(current) == 0 {
			return
		}
		if listSegments {
			segments = append(segments, e.callCoreForm("list", current...))
		} else {
			segment := make([]sexpr.Form, len(current))
			copy(segment, current)
			segments = append(segments, sexpr.NewVector(segment, span.Unknown(""), nil))
		}
		current = current[:0]
	}

	for _, item := range items {
		if item.splice {
			flush()
			segments = append(segments, item.form)
			continue
		}
		current = append(current, item.form)
	}

	flush()
	return e.callCoreForm("concat", segments...)
}

func (e *SyntaxQuoteFormExpander) dataListForm(list *sexpr.List, body sexpr.Form) (sexpr.Form, error) {
	if list == nil || len(list.Items) == 0 {
		return nil, fmt.Errorf("quoted data list requires a head symbol")
	}
	return e.callCoreForm("list", quoteFormForm(list.Items[0]), body), nil
}

func quoteFormForm(value sexpr.Form) sexpr.Form {
	return sexpr.NewList([]sexpr.Form{sexpr.NewSymbol("quote", span.Unknown(""), nil), value}, span.Unknown(""), nil)
}

func (e *SyntaxQuoteFormExpander) callCoreForm(name string, args ...sexpr.Form) sexpr.Form {
	items := make([]sexpr.Form, 0, len(args)+1)
	items = append(items, e.intrinsicFormSymbol(name))
	items = append(items, args...)
	return sexpr.NewList(items, span.Unknown(""), nil)
}

func (e *SyntaxQuoteFormExpander) intrinsicFormSymbol(name string) sexpr.Form {
	qualified := QualifySymbol(StdlibCoreNamespace, name)
	if e.symbolTable == nil {
		return sexpr.NewSymbol(qualified, span.Unknown(""), nil)
	}
	if sym, ok := e.symbolTable.ResolveIntrinsic(name); ok {
		return resolvedFormSymbolFor(qualified, sym)
	}
	return sexpr.NewSymbol(qualified, span.Unknown(""), nil)
}

func (e *SyntaxQuoteFormExpander) syntaxQuoteResolvedSymbol(sym sexpr.ResolvedSymbol) sexpr.Form {
	return sexpr.NewResolvedSymbol(sym.Name, sexpr.SymbolResolution{
		Kind:      sym.Resolution.Kind,
		Index:     sym.Resolution.Index,
		Namespace: sym.Resolution.Namespace,
	}, sym.Span(), nil)
}

func (e *SyntaxQuoteFormExpander) syntaxQuoteSymbol(name string) sexpr.Form {
	if name == "" {
		return sexpr.NewSymbol("", span.Unknown(""), nil)
	}

	if ns, local := ParseQualifiedSymbol(name); ns != "" {
		resolvedNs := ns
		if e.symbolTable != nil {
			resolvedNs = e.symbolTable.ResolveAlias(ns)
		}
		qualified := QualifySymbol(resolvedNs, local)
		if e.symbolTable != nil {
			if sym, ok := e.symbolTable.Resolve(name); ok {
				return resolvedFormSymbolFor(qualified, sym)
			}
		}
		return sexpr.NewSymbol(qualified, span.Unknown(""), nil)
	}

	if isAutoGensym(name) {
		return sexpr.NewSymbol(e.resolveAutoGensym(name), span.Unknown(""), nil)
	}

	if sourceQualified := e.resolveMacroOrigin(name); sourceQualified != "" {
		return sexpr.NewSymbol(sourceQualified, span.Unknown(""), nil)
	}
	if sourceQualified := e.resolveVarOrigin(name); sourceQualified != "" {
		if e.symbolTable != nil {
			if sym, ok := e.symbolTable.Resolve(name); ok {
				return resolvedFormSymbolFor(sourceQualified, sym)
			}
		}
		return sexpr.NewSymbol(sourceQualified, span.Unknown(""), nil)
	}

	if e.symbolTable != nil {
		if sym, ok := e.symbolTable.Resolve(name); ok {
			switch sym.Scope {
			case BuiltinScope:
				return resolvedFormSymbolFor(QualifySymbol(StdlibCoreNamespace, name), sym)
			case GlobalScope, ProtectedGlobalScope:
				targetNs := sym.Namespace
				if targetNs == CoreNamespace || targetNs == StdlibCoreNamespace {
					targetNs = StdlibCoreNamespace
				}
				if targetNs == "" {
					targetNs = e.currentNs
				}
				return resolvedFormSymbolFor(QualifySymbol(targetNs, name), sym)
			case LocalScope, FreeScope:
				return sexpr.NewSymbol(QualifySymbol(e.currentNs, name), span.Unknown(""), nil)
			}
		}
	}

	if isLiteralSyntaxQuoteSymbol(name) {
		return sexpr.NewSymbol(name, span.Unknown(""), nil)
	}
	return sexpr.NewSymbol(QualifySymbol(e.currentNs, name), span.Unknown(""), nil)
}

func (e *SyntaxQuoteFormExpander) resolveVarOrigin(localName string) string {
	if e.symbolTable == nil {
		return ""
	}
	return e.symbolTable.ResolveVarRefer(localName)
}

func (e *SyntaxQuoteFormExpander) resolveMacroOrigin(localName string) string {
	if e.macroResolver != nil {
		if qualified := e.macroResolver(localName); qualified != "" {
			return qualified
		}
	}
	if e.symbolTable == nil {
		return ""
	}
	return e.symbolTable.ResolveMacroRefer(localName)
}

func (e *SyntaxQuoteFormExpander) withGensymScope(fn func() (sexpr.Form, error)) (sexpr.Form, error) {
	e.gensymStack = append(e.gensymStack, make(map[string]string))
	defer func() {
		e.gensymStack = e.gensymStack[:len(e.gensymStack)-1]
	}()
	return fn()
}

func (e *SyntaxQuoteFormExpander) resolveAutoGensym(name string) string {
	if len(e.gensymStack) == 0 {
		e.gensymCount++
		return fmt.Sprintf("%s__%d__auto__", strings.TrimSuffix(name, "#"), e.gensymCount)
	}

	scope := e.gensymStack[len(e.gensymStack)-1]
	if generated, ok := scope[name]; ok {
		return generated
	}

	e.gensymCount++
	generated := fmt.Sprintf("%s__%d__auto__", strings.TrimSuffix(name, "#"), e.gensymCount)
	scope[name] = generated
	return generated
}

func resolvedFormSymbolFor(name string, sym Symbol) sexpr.Form {
	switch sym.Scope {
	case BuiltinScope:
		return sexpr.NewResolvedSymbol(name, sexpr.SymbolResolution{
			Kind:      sexpr.BuiltinSymbolResolution,
			Index:     sym.Index,
			Namespace: StdlibCoreNamespace,
		}, span.Unknown(""), nil)
	case GlobalScope, ProtectedGlobalScope:
		ns := parseResolvedNamespace(name)
		return sexpr.NewResolvedSymbol(name, sexpr.SymbolResolution{
			Kind:      sexpr.GlobalSymbolResolution,
			Index:     sym.Index,
			Namespace: ns,
		}, span.Unknown(""), nil)
	default:
		return sexpr.NewSymbol(name, span.Unknown(""), nil)
	}
}

func itemFormsFromPlans(items []formQuasiquoteItem) []sexpr.Form {
	forms := make([]sexpr.Form, 0, len(items))
	for _, item := range items {
		forms = append(forms, item.form)
	}
	return forms
}

func hasSplicingForms(items []formQuasiquoteItem) bool {
	for _, item := range items {
		if item.splice {
			return true
		}
	}
	return false
}

func isQuasiquoteFormForm(list *sexpr.List) bool {
	return hasHeadFormSymbol(list, "quasiquote")
}

func isUnquoteFormForm(list *sexpr.List) bool {
	return hasHeadFormSymbol(list, "unquote", "~")
}

func isUnquoteSplicingFormForm(list *sexpr.List) bool {
	return hasHeadFormSymbol(list, "unquote-splicing", "~@", "splice-unquote")
}

func hasHeadFormSymbol(list *sexpr.List, names ...string) bool {
	if list == nil || len(list.Items) == 0 {
		return false
	}
	headName, ok := formSymbolName(list.Items[0])
	if !ok {
		return false
	}
	for _, name := range names {
		if headName == name {
			return true
		}
	}
	return false
}

func (e *SyntaxQuoteFormExpander) wrapWithMetadataForm(metadata *sexpr.Map, form sexpr.Form, depth int) (sexpr.Form, error) {
	if metadata == nil || len(metadata.Entries) == 0 {
		return form, nil
	}
	metaForm, err := e.expand(metadata, depth)
	if err != nil {
		return nil, err
	}
	return e.callCoreForm("with-meta", form, metaForm), nil
}
