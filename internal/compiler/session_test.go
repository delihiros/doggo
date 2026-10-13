package compiler

import (
	"reflect"
	"strings"
	"testing"

	"doggo/internal/compiler/opt"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestCompileSourcePreservesDefinitionFormAndOriginalMacroCallViaDebugmeta(t *testing.T) {
	session := NewSession(SessionConfig{
		MacroEvaluator: func(fn *Closure, args []types.Value) (types.Value, error) {
			return coll.NewList(types.Symbol("def"), args[0], args[1]), nil
		},
	})

	if _, err := session.CompileSource("demo/macros.dog", "(defmacro defconst [name value] nil)"); err != nil {
		t.Fatalf("defmacro compile failed: %v", err)
	}

	bytecode, err := session.CompileSource("demo/consts.dog", "(defconst answer 42)")
	if err != nil {
		t.Fatal(err)
	}

	meta := findDefMetadataByName(bytecode.Constants, "answer")
	if meta == nil {
		t.Fatal("expected definition metadata to be emitted")
	}

	assertMapValueString(t, meta, "file", `"demo/consts.dog"`)
	assertMapValueString(t, meta, "line", "1")
	assertMapValueString(t, meta, "column", "1")
	assertMapValueString(t, meta, "source", `"(defconst answer 42)"`)
}

func TestSessionCompileFilePreservesNamespaceAndMacroState(t *testing.T) {
	session := NewSession(SessionConfig{
		MacroEvaluator: func(fn *Closure, args []types.Value) (types.Value, error) {
			return atom.NewNumberInt(42), nil
		},
	})

	first, err := session.CompileSource("demo/one.dog", "(ns demo.core) (defmacro twice [x] 1)")
	if err != nil {
		t.Fatal(err)
	}
	second, err := session.CompileSource("demo/two.dog", "(twice 21)")
	if err != nil {
		t.Fatal(err)
	}

	if first.Namespace != "demo.core" {
		t.Fatalf("first namespace = %q", first.Namespace)
	}
	if len(second.Instructions) == 0 {
		t.Fatal("expected bytecode from second compile")
	}
	if _, ok := session.Macros()["demo.core/twice"]; !ok {
		t.Fatalf("expected session to preserve compiled macro state: %+v", session.Macros())
	}
}

func TestSessionCompileSourceReportsFrontendDiagnostics(t *testing.T) {
	session := NewSession(SessionConfig{})

	_, err := session.CompileSource("demo/bad.dog", "(")
	if err == nil || !strings.Contains(err.Error(), "frontend diagnostics") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionCompileSourceReturnsScopedConstants(t *testing.T) {
	session := NewSession(SessionConfig{})

	first, err := session.CompileSource("demo/one.dog", `"alpha"`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := session.CompileSource("demo/two.dog", `"beta"`)
	if err != nil {
		t.Fatal(err)
	}

	if len(first.Constants) != 1 {
		t.Fatalf("first constants = %+v", first.Constants)
	}
	firstConst, ok := first.Constants[0].(atom.String)
	if !ok || string(firstConst) != "alpha" {
		t.Fatalf("first constant = %#v", first.Constants[0])
	}

	if len(second.Constants) != 1 {
		t.Fatalf("second constants = %+v", second.Constants)
	}
	secondConst, ok := second.Constants[0].(atom.String)
	if !ok || string(secondConst) != "beta" {
		t.Fatalf("second constant = %#v", second.Constants[0])
	}
}

func TestSessionCompileSourceEmitsOperandFreeReify(t *testing.T) {
	symbolTable := NewSymbolTable()
	target := symbolTable.DefineGlobal("target")
	session := NewSession(SessionConfig{
		SymbolTable:   symbolTable,
		ConstantStore: NewConstantStore(nil),
	})

	if _, err := session.CompileSource("seed.dg", `"seed"`); err != nil {
		t.Fatalf("seed compile failed: %v", err)
	}
	bytecode, err := session.CompileSource("reify.dg", `(reify target {})`)
	if err != nil {
		t.Fatal(err)
	}

	want := opcode.Instructions{
		byte(opcode.OpGetGlobal), byte(target.Index >> 8), byte(target.Index),
		byte(opcode.OpHash), 0, 0,
		byte(opcode.OpReify),
	}
	if !reflect.DeepEqual(bytecode.Instructions, want) {
		t.Fatalf("reify instructions = %v, want %v", []byte(bytecode.Instructions), []byte(want))
	}
}

func TestSessionCompileSourceRestoresStateOnFailure(t *testing.T) {
	session := NewSession(SessionConfig{})

	if _, err := session.CompileSource("demo/one.dog", "(ns demo.core) (def answer 21)"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CompileSource("demo/bad.dog", `(ns leaked.core) (do "leaked" missing)`); err == nil {
		t.Fatal("expected compile failure")
	}

	next, err := session.CompileSource("demo/next.dog", `(do "ok" answer)`)
	if err != nil {
		t.Fatalf("expected session state to roll back after failed compile, got %v", err)
	}
	if len(next.Constants) != 1 {
		t.Fatalf("next constants = %+v", next.Constants)
	}
	nextConst, ok := next.Constants[0].(atom.String)
	if !ok || string(nextConst) != "ok" {
		t.Fatalf("next constant = %#v", next.Constants[0])
	}
}

func TestSessionCompileSourcePreservesLoopFusionOptimization(t *testing.T) {
	newState := func() (*SymbolTable, *ConstantStore) {
		symbolTable := NewSymbolTable()
		symbolTable.DefineBuiltin(0, "+")
		symbolTable.DefineBuiltin(1, "reduce")
		symbolTable.DefineBuiltin(2, "map")
		symbolTable.DefineBuiltin(3, "count")
		symbolTable.DefineBuiltin(4, "nth")
		symbolTable.DefineBuiltin(5, "inc")
		symbolTable.DefineGlobal("<")
		return symbolTable, NewConstantStore(nil)
	}

	sessionSymbols, sessionConstants := newState()
	session := NewSession(SessionConfig{
		SymbolTable:        sessionSymbols,
		ConstantStore:      sessionConstants,
		OptimizationConfig: opt.DefaultOptimizationConfig(),
	})
	fromSource, err := session.CompileSource("demo/opt.dog", "(reduce + 0 (map inc [1 2 3]))")
	if err != nil {
		t.Fatal(err)
	}

	directSymbols, directConstants := newState()
	comp := NewWithState(directSymbols, directConstants)
	comp.SetOptimizationConfig(opt.DefaultOptimizationConfig())
	if err := compileTestForm(t, comp, compileTestSourceForm(t, "(reduce + 0 (map inc [1 2 3]))")); err != nil {
		t.Fatal(err)
	}
	direct := comp.Bytecode()

	if !reflect.DeepEqual(fromSource.Instructions, direct.Instructions) {
		t.Fatalf("instructions differ:\nsource=%v\ndirect=%v", []byte(fromSource.Instructions), []byte(direct.Instructions))
	}
	if len(fromSource.Constants) != len(direct.Constants) {
		t.Fatalf("constants len = %d, want %d", len(fromSource.Constants), len(direct.Constants))
	}
}
