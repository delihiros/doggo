package compiler

import (
	"errors"

	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/expand"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func (c *Compiler) compileDefForm(list *sexpr.List) error {
	if len(list.Items) < 2 || len(list.Items) > 3 {
		return c.newCompileError("def requires 1 or 2 arguments")
	}

	symName, ok := formSymbolName(list.Items[1])
	if !ok {
		return c.newCompileError("def name must be a symbol")
	}
	if err := c.symbolTable.ValidateVarGlobalAllowed(symName); err != nil {
		return c.newCompileError("%s", err)
	}

	meta := c.buildDefMetadata(list.Items[1], symName, list)
	metaIndex := c.addConstant(meta) + 1
	symbol, err := c.symbolTable.DefineVarGlobalChecked(symName)
	if err != nil {
		return c.newCompileError("%s", err)
	}
	c.updateGlobalPrivacy(symName, meta)
	qualifiedName := QualifySymbol(c.symbolTable.CurrentNamespace(), symName)

	if len(list.Items) == 3 {
		valueToCompile := c.maybeNameAnonymousFnForm(list.Items[2], symName)
		c.defInitStack = append(c.defInitStack, qualifiedName)
		if err := c.CompileForm(valueToCompile); err != nil {
			c.defInitStack = c.defInitStack[:len(c.defInitStack)-1]
			return err
		}
		c.defInitStack = c.defInitStack[:len(c.defInitStack)-1]
	} else {
		c.emit(opcode.OpNil)
	}
	delete(c.macros, qualifiedName)
	delete(c.macroMetadata, qualifiedName)

	c.emit(opcode.OpDefVar, symbol.Index, metaIndex)
	c.emit(opcode.OpNil)
	return nil
}

func (c *Compiler) buildDefMetadata(symValue any, symName string, sourceForm any) *coll.Map {
	meta := coll.NewEmptyMap()
	if existingMeta := extractSymbolMetadata(symValue); existingMeta != nil {
		existingMeta.Range(func(key, value types.Value) bool {
			meta = meta.Assoc(key, value).(*coll.Map)
			return true
		})
	}
	file, line, column := c.debugMeta.EffectiveSourceLocation()
	if file == "" {
		file = "<unknown>"
	}

	meta = meta.Assoc(atom.NewKeyword("file"), atom.String(file)).(*coll.Map)
	meta = meta.Assoc(atom.NewKeyword("line"), atom.NewNumberInt(int64(line))).(*coll.Map)
	meta = meta.Assoc(atom.NewKeyword("column"), atom.NewNumberInt(int64(column))).(*coll.Map)
	meta = meta.Assoc(atom.NewKeyword("ns"), types.NewSymbol(c.symbolTable.CurrentNamespace())).(*coll.Map)
	meta = meta.Assoc(atom.NewKeyword("name"), types.NewSymbol(symName)).(*coll.Map)
	if source := c.debugMeta.SourceFormText(sourceForm, c.sourceContentProvider); source != "" {
		meta = meta.Assoc(atom.NewKeyword("source"), atom.String(source)).(*coll.Map)
	}
	return meta
}

func extractSymbolMetadata(symValue any) *coll.Map {
	switch sym := symValue.(type) {
	case *types.SymbolWithMeta:
		if meta, ok := sym.Meta.(*coll.Map); ok {
			return meta
		}
	case *types.ResolvedSymbol:
		if meta, ok := sym.Meta.(*coll.Map); ok {
			return meta
		}
	case sexpr.Symbol:
		meta, _ := formMetadataAsRuntime(sym.Meta())
		return meta
	case sexpr.ResolvedSymbol:
		meta, _ := formMetadataAsRuntime(sym.Meta())
		return meta
	}
	return nil
}

func (c *Compiler) updateGlobalPrivacy(symName string, meta *coll.Map) {
	c.symbolTable.SetGlobalPrivateInNamespace(c.symbolTable.CurrentNamespace(), symName, metadataIsPrivate(meta))
}

func metadataIsPrivate(meta *coll.Map) bool {
	if meta == nil {
		return false
	}
	return meta.Get(atom.NewKeyword("private")) == atom.True
}

func (c *Compiler) compileVarForm(list *sexpr.List) error {
	if len(list.Items) != 2 {
		return c.newCompileError("var requires exactly 1 argument")
	}

	symName, ok := formSymbolName(list.Items[1])
	if !ok {
		return c.newCompileError("var argument must be a symbol, got %T", list.Items[1])
	}

	symbol, ok := c.symbolTable.Resolve(symName)
	if !ok {
		return c.newCompileError("undefined variable: %s", symName)
	}
	if symbol.Scope != GlobalScope {
		return c.newCompileError("var can only reference vars, got %s", symName)
	}

	c.emit(opcode.OpGetGlobalRaw, symbol.Index)
	return nil
}

func (c *Compiler) maybeNameAnonymousFnForm(value sexpr.Form, defName string) sexpr.Form {
	fnList, ok := value.(*sexpr.List)
	if !ok || len(fnList.Items) < 2 {
		return value
	}

	fnSymName, ok := formSymbolName(fnList.Items[0])
	if !ok || fnSymName != "fn" {
		return value
	}
	if _, ok := formSymbolName(fnList.Items[1]); ok {
		return value
	}

	newItems := make([]sexpr.Form, 0, len(fnList.Items)+1)
	newItems = append(newItems, fnList.Items[0])
	newItems = append(newItems, sexpr.NewSymbol(defName, span.Unknown(c.debugMeta.EffectiveFile()), nil))
	newItems = append(newItems, fnList.Items[1:]...)
	return sexpr.NewList(newItems, fnList.Span(), fnList.Meta())
}

func (c *Compiler) compileNsForm(list *sexpr.List) error {
	decl, err := c.parseNsDeclForm(list)
	if err != nil {
		return err
	}

	c.symbolTable.SetCurrentNamespace(decl.name)
	if decl.metadata != nil {
		c.symbolTable.SetNamespaceMetadata(decl.name, decl.metadata)
	}

	for _, clause := range decl.clauses {
		clauseKey, ok := clause.Items[0].(sexpr.Keyword)
		if !ok {
			return c.newCompileError("ns clause must start with a keyword, got %T", clause.Items[0])
		}

		switch clauseKey.Name {
		case "require":
			if err := c.compileNsRequireForm(clause); err != nil {
				return err
			}
		case "import":
			if err := c.compileNsImportForm(clause); err != nil {
				return err
			}
		case "refer-clojure":
			if err := c.compileNsReferClojureForm(clause); err != nil {
				return err
			}
		case "use":
			return c.newCompileError(":use is not supported; use (:require [some.lib :refer :all]) or explicit :refer instead")
		case "gen-class":
			return c.newCompileError(":gen-class is not supported; doggo does not generate JVM classes")
		default:
			return c.newCompileError("unsupported ns clause: :%s (supported: :require, :import, :refer-clojure)", clauseKey.Name)
		}
	}

	c.emit(opcode.OpNil)
	return nil
}

type parsedNsDeclForm struct {
	name     string
	metadata *coll.Map
	clauses  []*sexpr.List
}

func (c *Compiler) parseNsDeclForm(list *sexpr.List) (*parsedNsDeclForm, error) {
	if len(list.Items) < 2 {
		return nil, c.newCompileError("ns requires a namespace name")
	}

	nsName, ok := formSymbolName(list.Items[1])
	if !ok {
		return nil, c.newCompileError("ns name must be a symbol, got %T", list.Items[1])
	}

	meta := extractNsSymbolMetadataForm(list.Items[1])
	i := 2
	if i < len(list.Items) {
		if doc, ok := list.Items[i].(sexpr.String); ok {
			meta = mergeNsMetadata(meta, coll.NewMap(map[types.Value]types.Value{
				atom.NewKeyword("doc"): atom.NewString(doc.Value),
			}))
			i++
		}
	}
	if i < len(list.Items) {
		if attrs, ok := list.Items[i].(*sexpr.Map); ok {
			attrsPlan, err := expand.BuildMetadataLiteralPlan(attrs)
			if err != nil {
				return nil, c.newCompileError("ns metadata must be a literal map: %v", err)
			}
			runtimeAttrs, err := emit.MaterializeMetadata(attrsPlan)
			if err != nil {
				return nil, c.newCompileError("ns metadata must be a literal map: %v", err)
			}
			meta = mergeNsMetadata(meta, runtimeAttrs)
			i++
		}
	}

	clauses := make([]*sexpr.List, 0, len(list.Items)-i)
	for ; i < len(list.Items); i++ {
		clause, ok := list.Items[i].(*sexpr.List)
		if !ok {
			return nil, c.newCompileError("ns clause must be a list, got %T", list.Items[i])
		}
		if len(clause.Items) < 1 {
			return nil, c.newCompileError("empty ns clause")
		}
		clauses = append(clauses, clause)
	}

	return &parsedNsDeclForm{name: nsName, metadata: meta, clauses: clauses}, nil
}

func extractNsSymbolMetadataForm(nsVal sexpr.Form) *coll.Map {
	switch sym := nsVal.(type) {
	case sexpr.Symbol:
		meta, _ := formMetadataAsRuntime(sym.Meta())
		return meta
	case sexpr.ResolvedSymbol:
		meta, _ := formMetadataAsRuntime(sym.Meta())
		return meta
	default:
		return nil
	}
}

func formMetadataAsRuntime(meta *sexpr.Map) (*coll.Map, error) {
	if meta == nil || len(meta.Entries) == 0 {
		return nil, nil
	}
	plan, err := expand.BuildMetadataLiteralPlan(meta)
	if err != nil {
		return nil, err
	}
	return emit.MaterializeMetadata(plan)
}

func mergeNsMetadata(parts ...*coll.Map) *coll.Map {
	var merged *coll.Map
	for _, part := range parts {
		if part == nil {
			continue
		}
		if merged == nil {
			merged = coll.NewEmptyMap()
		}
		part.Range(func(key, value types.Value) bool {
			merged = merged.Assoc(key, value).(*coll.Map)
			return true
		})
	}
	return merged
}

func (c *Compiler) compileNsRequireForm(clause *sexpr.List) error {
	requireSym, ok := c.symbolTable.Resolve("require")
	if !ok {
		return c.newCompileError("require function not found")
	}

	for i := 1; i < len(clause.Items); i++ {
		spec := clause.Items[i]
		c.loadSymbol(requireSym)

		switch spec := spec.(type) {
		case sexpr.Symbol, sexpr.ResolvedSymbol, *sexpr.Vector:
			quotedSpec := sexpr.NewList([]sexpr.Form{
				sexpr.NewSymbol("quote", span.Unknown(c.debugMeta.EffectiveFile()), nil),
				spec,
			}, span.Unknown(c.debugMeta.EffectiveFile()), nil)
			if err := c.CompileForm(quotedSpec); err != nil {
				return err
			}
		default:
			return c.newCompileError("invalid require spec in ns: %T", spec)
		}

		c.emit(opcode.OpCall, 1)
		c.emit(opcode.OpPop)
	}

	return nil
}

func (c *Compiler) compileNsImportForm(clause *sexpr.List) error {
	for i := 1; i < len(clause.Items); i++ {
		pkgName, ok := clause.Items[i].(sexpr.String)
		if !ok {
			return c.newCompileError("ns :import entries must be string literals, got %T", clause.Items[i])
		}

		importForm := sexpr.NewList([]sexpr.Form{
			sexpr.NewSymbol("import", span.Unknown(c.debugMeta.EffectiveFile()), nil),
			pkgName,
		}, span.Unknown(c.debugMeta.EffectiveFile()), nil)
		if err := c.compileImportForm(importForm); err != nil {
			return err
		}
		c.emit(opcode.OpPop)
	}
	return nil
}

func (c *Compiler) compileNsReferClojureForm(clause *sexpr.List) error {
	if len(clause.Items) == 1 {
		return nil
	}
	if (len(clause.Items)-1)%2 != 0 {
		return c.newCompileError(":refer-clojure expects keyword/value pairs")
	}

	snapshot := c.symbolTable.Snapshot()
	for i := 1; i < len(clause.Items); i += 2 {
		kw, ok := clause.Items[i].(sexpr.Keyword)
		if !ok {
			c.symbolTable.Restore(snapshot)
			return c.newCompileError(":refer-clojure option key must be a keyword, got %T", clause.Items[i])
		}
		optionValue := clause.Items[i+1]

		switch kw.Name {
		case "exclude":
			if err := c.applyReferClojureExcludeForm(optionValue); err != nil {
				c.symbolTable.Restore(snapshot)
				return err
			}
		case "refer":
			if err := c.applyReferClojureReferForm(optionValue); err != nil {
				c.symbolTable.Restore(snapshot)
				return err
			}
		case "rename":
			if err := c.applyReferClojureRenameForm(optionValue); err != nil {
				c.symbolTable.Restore(snapshot)
				return err
			}
		default:
			c.symbolTable.Restore(snapshot)
			return c.newCompileError("unsupported :refer-clojure option: :%s (supported: :exclude, :refer, :rename)", kw.Name)
		}
	}

	return nil
}

func (c *Compiler) applyReferClojureExcludeForm(value sexpr.Form) error {
	symbols, err := parseCoreSymbolVectorForm(value, ":exclude")
	if err != nil {
		if errors.Is(err, errVectorExpected) {
			return c.newCompileError(":exclude value must be a vector, got %T", value)
		}
		return c.newCompileError("%s", err.Error())
	}
	currentNS := c.symbolTable.CurrentNamespace()
	for _, sym := range symbols {
		c.symbolTable.ExcludeCoreSymbol(currentNS, sym)
	}
	return nil
}

func (c *Compiler) applyReferClojureReferForm(value sexpr.Form) error {
	currentNS := c.symbolTable.CurrentNamespace()
	if kw, ok := value.(sexpr.Keyword); ok {
		if kw.Name != "all" {
			return c.newCompileError(":refer value must be :all or a vector, got :%s", kw.Name)
		}
		c.symbolTable.SetCoreExcludes(currentNS, nil)
		return nil
	}

	symbols, err := parseCoreSymbolVectorForm(value, ":refer")
	if err != nil {
		if errors.Is(err, errVectorExpected) {
			return c.newCompileError(":refer value must be :all or a vector, got %T", value)
		}
		return c.newCompileError("%s", err.Error())
	}
	for _, sym := range symbols {
		c.symbolTable.IncludeCoreSymbol(currentNS, sym)
	}
	return nil
}

func (c *Compiler) applyReferClojureRenameForm(value sexpr.Form) error {
	renameMap, ok := value.(*sexpr.Map)
	if !ok {
		return c.newCompileError(":rename value must be a map, got %T", value)
	}
	for _, entry := range renameMap.Entries {
		from, ok := formSymbolName(entry.Key)
		if !ok {
			return c.newCompileError(":rename map keys must be symbols, got %T", entry.Key)
		}
		to, ok := formSymbolName(entry.Value)
		if !ok {
			return c.newCompileError(":rename map values must be symbols, got %T", entry.Value)
		}
		if err := c.renameCoreSymbol(from, to); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) renameCoreSymbol(from, to string) error {
	currentNS := c.symbolTable.CurrentNamespace()
	registered := false

	// to が既に別物へ束縛されている場合は明示エラーにする
	if existing, ok := c.symbolTable.Resolve(to); ok {
		allowed := false
		if hasBuiltin, hasBuiltinAlias := c.symbolTable.ResolveBuiltin(from); hasBuiltinAlias {
			if existing.Scope == BuiltinScope && existing.Index == hasBuiltin {
				allowed = true
			}
		}
		for _, sourceNS := range []string{CoreNamespace, StdlibCoreNamespace} {
			if source, ok := c.symbolTable.Resolve(QualifySymbol(sourceNS, from)); ok {
				if existing.Scope == GlobalScope && existing.Index == source.Index {
					allowed = true
				}
			}
		}
		if !allowed {
			return c.newCompileError(":rename target already bound: %s", to)
		}
	}

	if idx, ok := c.symbolTable.ResolveBuiltin(from); ok {
		if err := c.symbolTable.AddBuiltinAlias(currentNS, to, idx); err != nil {
			return c.newCompileError(":rename %s -> %s failed: %v", from, to, err)
		}
		registered = true
	}

	// global シンボル rename
	for _, sourceNS := range []string{CoreNamespace, StdlibCoreNamespace} {
		err := c.symbolTable.AddRefer(sourceNS, from, to)
		if err == nil {
			registered = true
			continue
		}
		var referErr *ReferError
		if errors.As(err, &referErr) && referErr.Message == "symbol not found" {
			continue
		}
		return c.newCompileError(":rename %s -> %s failed: %v", from, to, err)
	}

	// macro alias の競合チェック
	existingMacro := c.symbolTable.ResolveMacroRefer(to)
	if existingMacro != "" {
		allowedMacro := false
		for _, sourceNS := range []string{CoreNamespace, StdlibCoreNamespace} {
			if existingMacro == QualifySymbol(sourceNS, from) {
				allowedMacro = true
				break
			}
		}
		if !allowedMacro {
			return c.newCompileError(":rename target already bound as macro: %s", to)
		}
	}

	for _, sourceNS := range []string{CoreNamespace, StdlibCoreNamespace} {
		qualified := QualifySymbol(sourceNS, from)
		if _, ok := c.macros[qualified]; ok {
			if err := c.symbolTable.AddMacroRefer(sourceNS, from, to); err != nil {
				return c.newCompileError(":rename %s -> %s failed: %v", from, to, err)
			}
			registered = true
		}
	}

	if !registered {
		return c.newCompileError(":rename symbol not found in core: %s", from)
	}

	// rename の意味として元名は最後に除外する（失敗時の副作用を避ける）
	c.symbolTable.ExcludeCoreSymbol(currentNS, from)
	return nil
}

var errVectorExpected = errors.New("vector expected")

func parseCoreSymbolVectorForm(value sexpr.Form, option string) ([]string, error) {
	vec, ok := value.(*sexpr.Vector)
	if !ok {
		return nil, errVectorExpected
	}
	symbols := make([]string, 0, len(vec.Items))
	for _, item := range vec.Items {
		symName, ok := formSymbolName(item)
		if !ok {
			return nil, errors.New(option + " vector must contain symbols, got invalid form")
		}
		symbols = append(symbols, symName)
	}
	return symbols, nil
}

func (c *Compiler) compileInNsForm(list *sexpr.List) error {
	if len(list.Items) != 2 {
		return c.newCompileError("in-ns requires exactly 1 argument")
	}

	var nsName string
	if name, ok := formSymbolName(list.Items[1]); ok {
		nsName = name
	} else if quoted, ok := list.Items[1].(*sexpr.List); ok {
		if len(quoted.Items) == 2 {
			if quoteSym, ok := formSymbolName(quoted.Items[0]); ok && quoteSym == "quote" {
				if nsSym, ok := formSymbolName(quoted.Items[1]); ok {
					nsName = nsSym
				}
			}
		}
	}
	if nsName == "" {
		return c.newCompileError("in-ns argument must be a symbol or quoted symbol")
	}

	c.symbolTable.SetCurrentNamespace(nsName)
	c.emit(opcode.OpNil)
	return nil
}
