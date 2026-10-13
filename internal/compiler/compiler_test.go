// Package compiler はCompilerのテスト用共通ヘルパーを提供します。
// テストは機能別に以下のファイルに分割されています：
// - compiler_test_arithmetic.go: 算術演算、比較演算
// - compiler_test_control.go: 制御フロー（if, do）
// - compiler_test_coll.go: コレクション操作
package compiler

import (
	"fmt"
	"testing"

	"doggo/internal/opcode"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// runCompilerTest はコンパイラテストケースを実行します。
func runCompilerTest(t *testing.T, source string, expectedConstants []interface{}, expectedInstructions []opcode.Instructions) {
	t.Helper()

	compiler := New()
	for index, name := range []string{"+", "-", "*", "/"} {
		compiler.symbolTable.DefineBuiltin(index, name)
	}
	bytecode, err := compileTestSource(t, compiler, source)
	if err != nil {
		t.Fatalf("compiler error: %s", err)
	}

	err = testInstructions(expectedInstructions, bytecode.Instructions)
	if err != nil {
		t.Fatalf("testInstructions failed: %s", err)
	}

	err = testConstants(expectedConstants, bytecode.Constants)
	if err != nil {
		t.Fatalf("testConstants failed: %s", err)
	}
}

// testInstructions は生成された命令列を検証します。
func testInstructions(expected []opcode.Instructions, actual opcode.Instructions) error {
	concatted := concatInstructions(expected)

	if len(actual) != len(concatted) {
		return fmt.Errorf("wrong instructions length.\nwant=%q\ngot =%q",
			concatted, actual)
	}

	for i, ins := range concatted {
		if actual[i] != ins {
			return fmt.Errorf("wrong instruction at %d.\nwant=%q\ngot =%q",
				i, concatted, actual)
		}
	}

	return nil
}

// testConstants は生成された定数プールを検証します。
func testConstants(expected []interface{}, actual []types.Value) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("wrong number of constants. got=%d, want=%d",
			len(actual), len(expected))
	}

	for i, constant := range expected {
		switch constant := constant.(type) {
		case int:
			err := testIntegerObject(int64(constant), actual[i])
			if err != nil {
				return fmt.Errorf("constant %d - testIntegerObject failed: %s", i, err)
			}
		}
	}

	return nil
}

// testIntegerObject は整数値を検証します。
func testIntegerObject(expected int64, actual types.Value) error {
	num, ok := actual.(*atom.Number)
	if !ok {
		return fmt.Errorf("object is not Number. got=%T (%+v)", actual, actual)
	}

	if num.IntVal == nil {
		return fmt.Errorf("number has no IntVal")
	}

	if *num.IntVal != expected {
		return fmt.Errorf("object has wrong value. got=%d, want=%d", *num.IntVal, expected)
	}

	return nil
}

// concatInstructions は命令列のスライスを連結します。
func concatInstructions(s []opcode.Instructions) opcode.Instructions {
	out := opcode.Instructions{}
	for _, ins := range s {
		out = append(out, ins...)
	}
	return out
}

func TestBytecodeBuiltinLayoutPreservesShadowedBuiltinNames(t *testing.T) {
	symbolTable := NewSymbolTable()
	symbolTable.DefineBuiltin(0, "count")

	comp := NewWithState(symbolTable, NewConstantStore(nil))
	bytecode, err := compileTestSource(t, comp, "(let [count 1] count)")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	layout := bytecode.BuiltinNames
	if len(layout) != 1 || layout[0] != "count" {
		t.Fatalf("unexpected builtin layout: %#v", layout)
	}
}

func TestNewCompileErrorUsesSourceLineProvider(t *testing.T) {
	comp := New()
	comp.SetSourceFile("demo.dg")
	comp.SetSourceLineProvider(func(file string, line int) (string, bool) {
		if file == "demo.dg" && line == 4 {
			return "(boom)", true
		}
		return "", false
	})
	comp.SetFormSourceSpan(span.New("demo.dg", 4, 2, 4, 9))

	err := comp.NewCompileError("boom")
	compileErr, ok := err.(*CompileError)
	if !ok {
		t.Fatalf("expected *CompileError, got %T", err)
	}
	if compileErr.File != "demo.dg" || compileErr.Line != 4 || compileErr.Column != 2 {
		t.Fatalf("unexpected compile error context: %+v", compileErr)
	}
	if compileErr.SnippetLine != "(boom)" {
		t.Fatalf("unexpected snippet line: %+v", compileErr)
	}
}

func TestCompileSymbolReferenceResolvesAndEmitsLoad(t *testing.T) {
	comp := New()
	symbol := comp.symbolTable.DefineGlobal("answer")

	if err := comp.CompileSymbolReference("answer"); err != nil {
		t.Fatal(err)
	}

	want := opcode.EncodeInstruction(opcode.OpGetGlobal, symbol.Index)
	if err := testInstructions([]opcode.Instructions{want}, comp.Bytecode().Instructions); err != nil {
		t.Fatal(err)
	}
}
