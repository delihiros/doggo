package compiler

import (
	"fmt"
	"sync/atomic"

	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/expand"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// gensymCounter は一意のシンボル名を生成するためのカウンター
var gensymCounter uint64

// gensym は一意のシンボル名を生成
func gensym(prefix string) string {
	n := atomic.AddUint64(&gensymCounter, 1)
	return fmt.Sprintf("__%s_%d__", prefix, n)
}

// emitSetSymbol はシンボルのスコープに応じた Set 命令を発行
func (c *Compiler) emitSetSymbol(sym Symbol) {
	if sym.Scope == GlobalScope {
		c.emit(opcode.OpSetGlobal, sym.Index)
	} else {
		c.emit(opcode.OpSetLocal, sym.Index)
	}
}

// emitDefineAndSet はシンボルを定義し Set 命令を発行
func (c *Compiler) emitDefineAndSet(name string) error {
	sym, err := c.symbolTable.DefineChecked(name)
	if err != nil {
		return c.newCompileError("%s", err)
	}
	c.emitSetSymbol(sym)
	return nil
}

// emitMapGet は (get map key) または (get map key default) をコンパイル
func (c *Compiler) emitMapGet(mapSym Symbol, key types.Value, defaultVal types.Value) error {
	getSym, ok := c.symbolTable.Resolve("get")
	if !ok {
		return c.newCompileError("get not found in symbol table")
	}
	c.loadSymbol(getSym)
	c.loadSymbol(mapSym)
	c.emit(opcode.OpConst, c.addConstant(key))

	if defaultVal != nil {
		c.emitLiteralDefaultValue(defaultVal)
		c.emit(opcode.OpCall, 3)
	} else {
		c.emit(opcode.OpCall, 2)
	}
	return nil
}

func (c *Compiler) emitLiteralDefaultValue(value types.Value) {
	switch current := value.(type) {
	case nil, types.Nil:
		c.emit(opcode.OpNil)
	case atom.Boolean:
		if bool(current) {
			c.emit(opcode.OpTrue)
		} else {
			c.emit(opcode.OpFalse)
		}
	default:
		c.emit(opcode.OpConst, c.addConstant(value))
	}
}

// compileDestructuringBindForm lowers form-native destructuring through a plan.
func (c *Compiler) compileDestructuringBindForm(pattern sexpr.Form) error {
	if pattern == nil {
		return c.newCompileError("invalid destructuring pattern: <nil>")
	}

	c.setSourceSpan(pattern.Span())

	plan, err := expand.BuildDestructurePlan(pattern)
	if err != nil {
		return c.newCompileError("destructuring conversion failed: %v", err)
	}
	return c.compileDestructuringBindPlan(plan)
}

func (c *Compiler) compileDestructuringBindPlan(pattern *expand.DestructurePlan) error {
	if pattern == nil {
		return c.newCompileError("invalid destructuring pattern: <nil>")
	}

	switch {
	case pattern.Symbol != nil:
		return c.emitDefineAndSet(pattern.Symbol.Name)
	case pattern.Vector != nil:
		return c.compileSeqDestructurePlan(pattern.Vector)
	case pattern.Map != nil:
		return c.compileMapDestructurePlan(pattern.Map)
	default:
		return c.newCompileError("invalid destructuring pattern: %T", pattern)
	}
}

// compileSeqDestructurePlan is the form-native version of sequence destructuring.
func (c *Compiler) compileSeqDestructurePlan(vec *expand.DestructureVectorPlan) error {
	if len(vec.Items) == 0 {
		c.emit(opcode.OpPop)
		return nil
	}

	tempSym, err := c.symbolTable.DefineChecked(gensym("seq"))
	if err != nil {
		return c.newCompileError("%s", err)
	}
	c.emitSetSymbol(tempSym)

	if vec.As != nil {
		c.loadSymbol(tempSym)
		if err := c.emitDefineAndSet(vec.As.Name); err != nil {
			return err
		}
	}

	for i, item := range vec.Items {
		nthSym, ok := c.symbolTable.Resolve("nth")
		if !ok {
			return c.newCompileError("nth not found in symbol table")
		}
		c.loadSymbol(nthSym)
		c.loadSymbol(tempSym)
		c.emit(opcode.OpConst, c.addConstant(atom.NewNumberInt(int64(i))))
		c.emit(opcode.OpCall, 2)

		if err := c.compileDestructuringBindPlan(item); err != nil {
			return err
		}
	}

	if vec.Rest != nil {
		seqSym, ok := c.symbolTable.Resolve("seq")
		if !ok {
			return c.newCompileError("seq not found in symbol table")
		}
		c.loadSymbol(seqSym)

		dropSym, ok := c.symbolTable.Resolve("drop")
		if !ok {
			return c.newCompileError("drop not found in symbol table")
		}
		c.loadSymbol(dropSym)
		c.emit(opcode.OpConst, c.addConstant(atom.NewNumberInt(int64(len(vec.Items)))))
		c.loadSymbol(tempSym)
		c.emit(opcode.OpCall, 2)
		c.emit(opcode.OpCall, 1)

		if err := c.emitDefineAndSet(vec.Rest.Name); err != nil {
			return err
		}
	}

	return nil
}

// compileMapDestructurePlan is the form-native version of map destructuring.
func (c *Compiler) compileMapDestructurePlan(plan *expand.DestructureMapPlan) error {
	tempSym, err := c.symbolTable.DefineChecked(gensym("map"))
	if err != nil {
		return c.newCompileError("%s", err)
	}
	c.emitSetSymbol(tempSym)

	if plan.As != nil {
		c.loadSymbol(tempSym)
		if err := c.emitDefineAndSet(plan.As.Name); err != nil {
			return err
		}
	}

	for _, item := range plan.Keys {
		defaultVal, err := c.materializeDestructureDefault(plan.Defaults, item.Name)
		if err != nil {
			return err
		}
		if err := c.emitMapGet(tempSym, atom.Keyword(item.Name), defaultVal); err != nil {
			return err
		}
		if err := c.emitDefineAndSet(item.Name); err != nil {
			return err
		}
	}

	for _, item := range plan.Strs {
		defaultVal, err := c.materializeDestructureDefault(plan.Defaults, item.Name)
		if err != nil {
			return err
		}
		if err := c.emitMapGet(tempSym, atom.String(item.Name), defaultVal); err != nil {
			return err
		}
		if err := c.emitDefineAndSet(item.Name); err != nil {
			return err
		}
	}

	for _, binding := range plan.Bindings {
		keyValue, err := emit.MaterializeQuotedLiteral(binding.KeyPlan)
		if err != nil {
			return err
		}
		defaultVal, err := c.materializeDestructureDefault(plan.Defaults, binding.Name)
		if err != nil {
			return err
		}
		if err := c.emitMapGet(tempSym, keyValue, defaultVal); err != nil {
			return err
		}
		if err := c.emitDefineAndSet(binding.Name); err != nil {
			return err
		}
	}

	return nil
}

func (c *Compiler) materializeDestructureDefault(defaults map[string]*expand.QuotedLiteralPlan, names ...string) (types.Value, error) {
	if len(defaults) == 0 {
		return nil, nil
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if plan, ok := defaults[name]; ok {
			return emit.MaterializeQuotedLiteral(plan)
		}
	}
	return nil, nil
}
