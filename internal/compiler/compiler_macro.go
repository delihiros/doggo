package compiler

import (
	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/expand"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func (c *Compiler) parseDefMacroDeclForm(list *sexpr.List) (string, *coll.Map, *sexpr.Vector, int, error) {
	symName, ok := formSymbolName(list.Items[1])
	if !ok {
		return "", nil, nil, 0, c.newCompileError("defmacro name must be a symbol")
	}

	meta := c.buildDefMetadata(list.Items[1], symName, list)

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
				return "", nil, nil, 0, c.newCompileError("defmacro attributes must be metadata-compatible: %v", err)
			}
			runtimeAttrs, err := emit.MaterializeMetadata(attrsPlan)
			if err != nil {
				return "", nil, nil, 0, c.newCompileError("defmacro attributes must be metadata-compatible: %v", err)
			}
			meta = mergeNsMetadata(meta, runtimeAttrs)
			i++
		}
	}

	if i >= len(list.Items) {
		return "", nil, nil, 0, c.newCompileError("defmacro parameters must be a vector")
	}
	params, ok := list.Items[i].(*sexpr.Vector)
	if !ok {
		return "", nil, nil, 0, c.newCompileError("defmacro parameters must be a vector")
	}

	return symName, meta, params, i + 1, nil
}

func (c *Compiler) expandMacroForm(list *sexpr.List) (sexpr.Form, error) {
	if len(list.Items) == 0 {
		return nil, nil
	}

	symName, ok := formSymbolName(list.Items[0])
	if !ok {
		return nil, nil
	}

	macro := c.resolveMacro(symName)
	if macro == nil {
		return nil, nil
	}
	if c.macroEvaluator == nil {
		return nil, c.newCompileError("macro evaluator not set")
	}

	return expand.Expander{
		LookupRuntimeMacro: c.lookupRuntimeMacro,
		RuntimeMacros: runtimeMacroExpander{
			evaluator: c.macroEvaluator,
			lookup:    c.lookupMacroClosure,
		},
	}.Macroexpand1(list)
}

func (c *Compiler) lookupRuntimeMacro(sym sexpr.Symbol) (expand.RuntimeMacroHandle, bool) {
	if _, ok := c.lookupMacroClosure(sym); !ok {
		return nil, false
	}
	return sym, true
}

func (c *Compiler) lookupMacroClosure(sym sexpr.Symbol) (*Closure, bool) {
	macro := c.resolveMacro(sym.Name)
	if macro == nil {
		return nil, false
	}
	return macro, true
}

func (c *Compiler) compileDefMacroForm(list *sexpr.List) error {
	if len(list.Items) < 3 {
		return c.newCompileError("defmacro requires name, parameters, and body")
	}

	symName, meta, params, bodyIndex, err := c.parseDefMacroDeclForm(list)
	if err != nil {
		return err
	}
	if err := c.symbolTable.ValidateVarGlobalAllowed(symName); err != nil {
		return c.newCompileError("%s", err)
	}

	c.enterScope()

	var variadic bool
	var numRequiredParams int

	for i, param := range params.Items {
		paramName, ok := formSymbolName(param)
		if !ok {
			return c.newCompileError("defmacro parameter must be a symbol")
		}

		if paramName == "&" {
			if i+1 >= len(params.Items) {
				return c.newCompileError("& must be followed by a rest parameter name")
			}
			restParamName, ok := formSymbolName(params.Items[i+1])
			if !ok {
				return c.newCompileError("rest parameter must be a symbol")
			}
			if _, err := c.symbolTable.DefineChecked(restParamName); err != nil {
				return c.newCompileError("%s", err)
			}
			variadic = true
			numRequiredParams = i
			break
		}
		if _, err := c.symbolTable.DefineChecked(paramName); err != nil {
			return c.newCompileError("%s", err)
		}
	}

	if !variadic {
		numRequiredParams = len(params.Items)
	}

	if bodyIndex >= len(list.Items) {
		c.emit(opcode.OpNil)
	} else {
		for i := bodyIndex; i < len(list.Items); i++ {
			if err := c.CompileForm(list.Items[i]); err != nil {
				return err
			}
			if i < len(list.Items)-1 {
				c.emit(opcode.OpPop)
			}
		}
	}

	if c.lastInstructionIs(opcode.OpPop) {
		c.replaceLastPopWithReturn()
	} else if !c.lastInstructionIs(opcode.OpReturnValue) {
		c.emit(opcode.OpReturnValue)
	}

	freeSymbols := c.symbolTable.FreeSymbols
	numLocals := c.symbolTable.NumLocals()
	instructions, lines, columns := c.leaveScope()
	if len(freeSymbols) > 0 {
		return c.newCompileError("defmacro cannot capture free variables")
	}

	closure := &Closure{
		Fn: newCompiledFunction(
			bytecodeMetadata{SourceFile: c.EffectiveSourceFile(), Lines: lines, Columns: columns},
			instructions,
			numLocals,
			numRequiredParams,
			variadic,
			c.symbolTable.CurrentNamespace(),
			"",
			nil,
		),
		Free: []types.Value{},
	}

	qualifiedName := QualifySymbol(c.symbolTable.CurrentNamespace(), symName)
	c.macros[qualifiedName] = closure
	c.macroMetadata[qualifiedName] = meta
	c.emit(opcode.OpNil)
	return nil
}

// resolveMacro はシンボル名からマクロを解決します。
// 解決順序:
//  1. 修飾シンボル: alias を解決して ns/name で検索
//  2. 非修飾シンボル:
//     a. currentNs/name
//     b. macro refer で取り込まれたマクロ
//     c. core/name（core名前空間のマクロ）
func (c *Compiler) resolveMacro(symStr string) *Closure {
	qualified := c.resolveMacroQualified(symStr)
	if qualified == "" {
		return nil
	}
	return c.macros[qualified]
}

func (c *Compiler) resolveMacroQualified(symStr string) string {
	qualified := c.lookupMacroQualified(symStr)
	if qualified == "" {
		return ""
	}
	if c.isPrivateMacroQualified(qualified) {
		ns, _ := ParseQualifiedSymbol(qualified)
		if ns != c.symbolTable.CurrentNamespace() {
			return ""
		}
	}
	if sym, ok := c.symbolTable.Resolve(symStr); ok && c.bindingShadowsMacro(symStr, sym, qualified) {
		return ""
	}
	return qualified
}

func (c *Compiler) lookupMacroQualified(symStr string) string {
	ns, localName := ParseQualifiedSymbol(symStr)

	if ns != "" {
		// 修飾シンボル: alias を解決
		resolvedNs := c.symbolTable.ResolveAlias(ns)
		qualifiedName := QualifySymbol(resolvedNs, localName)
		if _, ok := c.macros[qualifiedName]; ok {
			return qualifiedName
		}
		return ""
	}

	// 非修飾シンボル: 複数の場所を検索
	currentNs := c.symbolTable.CurrentNamespace()

	// 1. 現在の名前空間
	qualifiedName := QualifySymbol(currentNs, symStr)
	if _, ok := c.macros[qualifiedName]; ok {
		return qualifiedName
	}

	// 2. macro refer で取り込まれたマクロ
	if sourceQualified := c.symbolTable.ResolveMacroRefer(symStr); sourceQualified != "" {
		if _, ok := c.macros[sourceQualified]; ok {
			return sourceQualified
		}
	}
	if c.symbolTable.IsCoreSymbolExcluded(symStr) {
		return ""
	}

	// 3. core 名前空間（doggo.core）
	if currentNs != CoreNamespace {
		coreQualified := QualifySymbol(CoreNamespace, symStr)
		if _, ok := c.macros[coreQualified]; ok {
			return coreQualified
		}
	}

	// 4. doggo.core 名前空間も試す（StdlibCoreNamespace）
	if currentNs != StdlibCoreNamespace {
		doggoCoreName := QualifySymbol(StdlibCoreNamespace, symStr)
		if _, ok := c.macros[doggoCoreName]; ok {
			return doggoCoreName
		}
	}

	return ""
}

func (c *Compiler) isPrivateMacroQualified(qualifiedName string) bool {
	meta, ok := c.macroMetadata[qualifiedName]
	if !ok || meta == nil {
		return false
	}
	return meta.Get(atom.NewKeyword("private")) == atom.True
}

func (c *Compiler) bindingShadowsMacro(requested string, sym Symbol, macroQualified string) bool {
	if c.isDefInitBinding(sym) {
		return false
	}
	switch sym.Scope {
	case GlobalScope:
		return c.symbolTable.EffectiveQualifiedName(requested, sym) != macroQualified
	case BuiltinScope:
		return false
	default:
		return true
	}
}

func (c *Compiler) isDefInitBinding(sym Symbol) bool {
	if len(c.defInitStack) == 0 || sym.Scope != GlobalScope {
		return false
	}
	qualified := QualifySymbol(sym.Namespace, sym.Name)
	return qualified == c.defInitStack[len(c.defInitStack)-1]
}

func (c *Compiler) compileQuasiquoteForm(list *sexpr.List) error {
	if len(list.Items) != 2 {
		return c.newCompileError("quasiquote requires exactly 1 argument")
	}
	expanded, err := QuasiquoteExpandForm(list.Items[1], c.symbolTable, c.resolveMacroQualified)
	if err != nil {
		return c.newCompileError("%s", err)
	}
	return c.CompileForm(expanded)
}
