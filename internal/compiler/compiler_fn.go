package compiler

import (
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
)

func (c *Compiler) compileFnForm(list *sexpr.List) error {
	if len(list.Items) < 2 {
		return c.newCompileError("fn requires parameters")
	}

	startIdx := 1
	fnName := ""
	if name, ok := formSymbolName(list.Items[1]); ok {
		fnName = name
		startIdx = 2
	}
	if startIdx >= len(list.Items) {
		return c.newCompileError("fn requires parameters")
	}

	return c.compileFnFormDecl(fnName, list.Items[startIdx:])
}

type fnFormArity struct {
	params *sexpr.Vector
	body   []sexpr.Form
}

type fnFormDestructurePattern struct {
	localIndex int
	pattern    sexpr.Form
}

func (c *Compiler) compileFnFormDecl(fnName string, declItems []sexpr.Form) error {
	if len(declItems) == 0 {
		return c.newCompileError("fn requires parameters")
	}

	c.enterScope()

	arityForms, err := c.parseFnFormArities(declItems)
	if err != nil {
		return err
	}

	var arities []ArityInfo
	var maxLocals int

	for _, arityForm := range arityForms {
		c.symbolTable.ResetLocals()

		var variadic bool
		var numRequiredParams int
		destructurePatterns := make([]fnFormDestructurePattern, 0)

	paramLoop:
		for i, param := range arityForm.params.Items {
			if symName, ok := formSymbolName(param); ok {
				if symName == "&" {
					if i+1 >= len(arityForm.params.Items) {
						return c.newCompileError("& must be followed by a rest parameter name")
					}
					restParam := arityForm.params.Items[i+1]
					if restName, ok := formSymbolName(restParam); ok {
						if _, err := c.symbolTable.DefineChecked(restName); err != nil {
							return c.newCompileError("%s", err)
						}
					} else {
						syntheticName := gensym("rest")
						if _, err := c.symbolTable.DefineChecked(syntheticName); err != nil {
							return c.newCompileError("%s", err)
						}
						destructurePatterns = append(destructurePatterns, fnFormDestructurePattern{
							localIndex: i + 1,
							pattern:    restParam,
						})
					}
					variadic = true
					numRequiredParams = i
					break paramLoop
				}

				if _, err := c.symbolTable.DefineChecked(symName); err != nil {
					return c.newCompileError("%s", err)
				}
				continue
			}

			switch param.(type) {
			case *sexpr.Vector, *sexpr.Map:
				syntheticName := gensym("p")
				if _, err := c.symbolTable.DefineChecked(syntheticName); err != nil {
					return c.newCompileError("%s", err)
				}
				destructurePatterns = append(destructurePatterns, fnFormDestructurePattern{
					localIndex: i,
					pattern:    param,
				})
			default:
				return c.newCompileError("fn parameter must be a symbol or destructure pattern")
			}
		}

		if !variadic {
			numRequiredParams = len(arityForm.params.Items)
		}

		entryIP := len(c.currentInstructions())

		for _, destructure := range destructurePatterns {
			c.setSourceSpan(destructure.pattern.Span())
			c.emit(opcode.OpGetLocal, destructure.localIndex)
			if err := c.compileDestructuringBindForm(destructure.pattern); err != nil {
				return err
			}
		}

		if err := c.compileFormBody(arityForm.body); err != nil {
			return err
		}

		if c.lastInstructionIs(opcode.OpPop) {
			c.replaceLastPopWithReturn()
		} else if !c.lastInstructionIs(opcode.OpReturnValue) {
			c.emit(opcode.OpReturnValue)
		}

		if c.symbolTable.NumLocals() > maxLocals {
			maxLocals = c.symbolTable.NumLocals()
		}

		arities = append(arities, ArityInfo{
			NumParameters: numRequiredParams,
			Variadic:      variadic,
			EntryIP:       entryIP,
		})
	}

	freeSymbols := c.symbolTable.FreeSymbols
	instructions, lines, columns := c.leaveScope()

	for _, s := range freeSymbols {
		c.loadSymbol(s)
	}

	defaultArity := ArityInfo{NumParameters: 0}
	var otherArities []ArityInfo
	if len(arities) > 0 {
		defaultArity = arities[0]
		if len(arities) > 1 {
			otherArities = arities[1:]
		}
	}

	compiledFn := newCompiledFunction(
		bytecodeMetadata{SourceFile: c.EffectiveSourceFile(), Lines: lines, Columns: columns},
		instructions,
		maxLocals,
		defaultArity.NumParameters,
		defaultArity.Variadic,
		c.symbolTable.CurrentNamespace(),
		fnName,
		otherArities,
	)
	fnIndex := c.addConstant(compiledFn)

	c.emit(opcode.OpClosure, fnIndex, len(freeSymbols))

	return nil
}

func (c *Compiler) parseFnFormArities(declItems []sexpr.Form) ([]fnFormArity, error) {
	if len(declItems) == 0 {
		return nil, c.newCompileError("fn requires parameters")
	}

	if params, ok := declItems[0].(*sexpr.Vector); ok {
		return []fnFormArity{{
			params: params,
			body:   declItems[1:],
		}}, nil
	}

	arities := make([]fnFormArity, 0, len(declItems))
	for _, item := range declItems {
		arityList, ok := item.(*sexpr.List)
		if !ok {
			return nil, c.newCompileError("fn arity must be a list")
		}
		if len(arityList.Items) == 0 {
			return nil, c.newCompileError("arity must have parameter vector")
		}
		params, ok := arityList.Items[0].(*sexpr.Vector)
		if !ok {
			return nil, c.newCompileError("fn parameters must be a vector")
		}
		arities = append(arities, fnFormArity{
			params: params,
			body:   arityList.Items[1:],
		})
	}
	return arities, nil
}
