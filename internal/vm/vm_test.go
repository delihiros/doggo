// Package vm はVMのテスト用共通ヘルパーを提供します。
// テストは機能別に以下のファイルに分割されています：
// - vm_test_arithmetic.go: 算術演算、比較演算
// - vm_test_control.go: 制御フロー（if, do, loop/recur, and/or）
// - vm_test_function.go: 関数呼び出し、let束縛、def
// - vm_test_coll.go: コレクション操作
package vm

import (
	"fmt"
	"strings"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// vmTestCase はVMテストケースの構造体です。
type vmTestCase struct {
	input    types.Value
	expected interface{}
}

// runVmTests はテストケースのスライスを実行します。
func runVmTests(t *testing.T, tests []vmTestCase) {
	t.Helper()

	for _, tt := range tests {
		comp := compiler.New()
		err := compileRuntimeValue(t, comp, tt.input)
		if err != nil {
			t.Fatalf("compiler error: %s", err)
		}

		vm, err := New(comp.Bytecode())
		if err != nil {
			t.Fatalf("new vm: %s", err)
		}
		err = vm.Run()
		if err != nil {
			t.Fatalf("vm error: %s", err)
		}

		stackElem := vm.StackTop()
		testExpectedObject(t, tt.expected, stackElem)
	}
}

func assertRunResult(t *testing.T, runner *Runner, source, want string) {
	t.Helper()

	result, err := runner.Run(source)
	if err != nil {
		t.Fatalf("run %q: %v", source, err)
	}
	if got := result.String(); got != want {
		t.Fatalf("run %q = %q, want %q", source, got, want)
	}
}

func assertRunErrorContains(t *testing.T, runner *Runner, source, want string) {
	t.Helper()

	_, err := runner.Run(source)
	if err == nil {
		t.Fatalf("run %q succeeded, want error containing %q", source, want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("run %q error = %v, want substring %q", source, err, want)
	}
}

// testExpectedObject は期待値と実際の値を比較します。
func testExpectedObject(t *testing.T, expected interface{}, actual types.Value) {
	t.Helper()

	switch expected := expected.(type) {
	case int:
		err := testIntegerObject(int64(expected), actual)
		if err != nil {
			t.Errorf("testIntegerObject failed: %s", err)
		}
	case float64:
		err := testFloatObject(expected, actual)
		if err != nil {
			t.Errorf("testFloatObject failed: %s", err)
		}
	case bool:
		err := testBooleanObject(expected, actual)
		if err != nil {
			t.Errorf("testBooleanObject failed: %s", err)
		}
	case []int:
		vec, ok := actual.(*coll.Vector)
		if !ok {
			t.Errorf("object is not Vector. got=%T (%+v)", actual, actual)
			return
		}
		if len(vec.Items) != len(expected) {
			t.Errorf("wrong num of elements. want=%d, got=%d",
				len(expected), len(vec.Items))
			return
		}
		for i, expectedElem := range expected {
			err := testIntegerObject(int64(expectedElem), vec.Items[i])
			if err != nil {
				t.Errorf("testIntegerObject failed for element %d: %s", i, err)
			}
		}
	case types.Nil:
		if actual != atom.NilValue {
			t.Errorf("object is not Nil. got=%T (%+v)", actual, actual)
		}
	case *Error:
		errObj, ok := actual.(*Error)
		if !ok {
			t.Errorf("object is not Error. got=%T (%+v)", actual, actual)
			return
		}
		if errObj.Message != expected.Message {
			t.Errorf("wrong error message. expected=%q, got=%q",
				expected.Message, errObj.Message)
		}
	default:
		t.Errorf("unsupported expected type: %T", expected)
	}
}

// testIntegerObject は整数値を検証します。
func testIntegerObject(expected int64, actual types.Value) error {
	result, ok := actual.(*atom.Number)
	if !ok {
		return fmt.Errorf("object is not Number. got=%T (%+v)", actual, actual)
	}

	if result.IntVal != nil {
		if *result.IntVal != expected {
			return fmt.Errorf("object has wrong value. got=%d, want=%d",
				*result.IntVal, expected)
		}
		return nil
	}

	// フロートの場合も整数として比較
	if result.FloatVal != nil {
		if int64(*result.FloatVal) != expected {
			return fmt.Errorf("object has wrong value. got=%f, want=%d",
				*result.FloatVal, expected)
		}
		return nil
	}

	return fmt.Errorf("number has no value")
}

// testFloatObject は浮動小数点数値を検証します。
func testFloatObject(expected float64, actual types.Value) error {
	result, ok := actual.(*atom.Number)
	if !ok {
		return fmt.Errorf("object is not Number. got=%T (%+v)", actual, actual)
	}

	if result.ToFloat64() != expected {
		return fmt.Errorf("object has wrong value. got=%f, want=%f",
			result.ToFloat64(), expected)
	}

	return nil
}

// testBooleanObject は真偽値を検証します。
func testBooleanObject(expected bool, actual types.Value) error {
	result, ok := actual.(atom.Boolean)
	if !ok {
		return fmt.Errorf("object is not Boolean. got=%T (%+v)", actual, actual)
	}

	if bool(result) != expected {
		return fmt.Errorf("object has wrong value. got=%t, want=%t",
			result, expected)
	}

	return nil
}
