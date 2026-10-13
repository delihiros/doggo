package vm

import (
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestFunctionCalls(t *testing.T) {
	tests := []vmTestCase{
		// ((fn [] 5))
		{
			coll.NewList(
				coll.NewList(types.Symbol("fn"), coll.NewVector(), atom.NewNumberInt(5))),
			5,
		},
		// ((fn [] (+ 1 2)))
		{
			coll.NewList(
				coll.NewList(types.Symbol("fn"), coll.NewVector(),
					coll.NewList(types.Symbol("+"), atom.NewNumberInt(1), atom.NewNumberInt(2)))),
			3,
		},
		// ((fn [x] x) 5)
		{
			coll.NewList(
				coll.NewList(types.Symbol("fn"), coll.NewVector(types.Symbol("x")), types.Symbol("x")),
				atom.NewNumberInt(5)),
			5,
		},
		// ((fn [x y] (+ x y)) 1 2)
		{
			coll.NewList(
				coll.NewList(types.Symbol("fn"),
					coll.NewVector(types.Symbol("x"), types.Symbol("y")),
					coll.NewList(types.Symbol("+"), types.Symbol("x"), types.Symbol("y"))),
				atom.NewNumberInt(1),
				atom.NewNumberInt(2)),
			3,
		},
	}

	runVmTests(t, tests)
}

func TestLetBindings(t *testing.T) {
	tests := []vmTestCase{
		// (let [x 5] x)
		{
			coll.NewList(types.Symbol("let"),
				coll.NewVector(types.Symbol("x"), atom.NewNumberInt(5)),
				types.Symbol("x")),
			5,
		},
		// (let [x 5 y 10] (+ x y))
		{
			coll.NewList(types.Symbol("let"),
				coll.NewVector(
					types.Symbol("x"), atom.NewNumberInt(5),
					types.Symbol("y"), atom.NewNumberInt(10)),
				coll.NewList(types.Symbol("+"), types.Symbol("x"), types.Symbol("y"))),
			15,
		},
	}

	runVmTests(t, tests)
}

func TestGlobalDefStatements(t *testing.T) {
	tests := []vmTestCase{
		// (do (def one 1) one)
		{
			coll.NewList(types.Symbol("do"),
				coll.NewList(types.Symbol("def"), types.Symbol("one"), atom.NewNumberInt(1)),
				types.Symbol("one")),
			1,
		},
		// (do (def one 1) (def two 2) (+ one two))
		{
			coll.NewList(types.Symbol("do"),
				coll.NewList(types.Symbol("def"), types.Symbol("one"), atom.NewNumberInt(1)),
				coll.NewList(types.Symbol("def"), types.Symbol("two"), atom.NewNumberInt(2)),
				coll.NewList(types.Symbol("+"), types.Symbol("one"), types.Symbol("two"))),
			3,
		},
	}

	runVmTests(t, tests)
}

func TestVMResetPreservesTopLevelLocalLayout(t *testing.T) {
	form := coll.NewList(types.Symbol("let"),
		coll.NewVector(
			types.Symbol("x"), atom.NewNumberInt(5),
			types.Symbol("y"), atom.NewNumberInt(10)),
		coll.NewList(types.Symbol("+"), types.Symbol("x"), types.Symbol("y")))

	comp := compiler.New()
	if err := compileRuntimeValue(t, comp, form); err != nil {
		t.Fatalf("compiler error: %s", err)
	}

	bytecode := comp.Bytecode()
	if bytecode.NumLocals < 2 {
		t.Fatalf("expected top-level let to allocate local slots, got %d", bytecode.NumLocals)
	}

	vm, err := New(bytecode)
	if err != nil {
		t.Fatalf("new vm: %s", err)
	}
	if err := vm.Run(); err != nil {
		t.Fatalf("first run failed: %s", err)
	}
	testExpectedObject(t, 15, vm.StackTop())

	if err := vm.Reset(bytecode); err != nil {
		t.Fatalf("reset vm: %s", err)
	}
	if err := vm.Run(); err != nil {
		t.Fatalf("second run after reset failed: %s", err)
	}
	testExpectedObject(t, 15, vm.StackTop())
}

func TestVMRejectsInvalidTopLevelLocalCount(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected invalid top-level local count to panic")
		}
	}()

	_, _ = New(&compiler.Bytecode{NumLocals: StackSize + 1})
}
