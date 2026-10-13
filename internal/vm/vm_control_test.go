package vm

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestConditionals(t *testing.T) {
	tests := []vmTestCase{
		// (if true 10 20)
		{
			coll.NewList(types.Symbol("if"), atom.True, atom.NewNumberInt(10), atom.NewNumberInt(20)),
			10,
		},
		// (if false 10 20)
		{
			coll.NewList(types.Symbol("if"), atom.False, atom.NewNumberInt(10), atom.NewNumberInt(20)),
			20,
		},
		// (if false 10)
		{
			coll.NewList(types.Symbol("if"), atom.False, atom.NewNumberInt(10)),
			atom.NilValue,
		},
		// (if 1 10 20) - truthy test
		{
			coll.NewList(types.Symbol("if"), atom.NewNumberInt(1), atom.NewNumberInt(10), atom.NewNumberInt(20)),
			10,
		},
	}

	runVmTests(t, tests)
}

func TestDoExpressions(t *testing.T) {
	tests := []vmTestCase{
		// (do 1 2 3)
		{
			coll.NewList(types.Symbol("do"),
				atom.NewNumberInt(1),
				atom.NewNumberInt(2),
				atom.NewNumberInt(3)),
			3,
		},
	}

	runVmTests(t, tests)
}

func TestLoopRecur(t *testing.T) {
	tests := []vmTestCase{
		// (loop [sum 0 n 5] (if (> n 0) (recur (+ sum n) (- n 1)) sum))
		// 1+2+3+4+5 = 15
		{
			coll.NewList(types.Symbol("loop"),
				coll.NewVector(
					types.Symbol("sum"), atom.NewNumberInt(0),
					types.Symbol("n"), atom.NewNumberInt(5)),
				coll.NewList(types.Symbol("if"),
					coll.NewList(types.Symbol(">"), types.Symbol("n"), atom.NewNumberInt(0)),
					coll.NewList(types.Symbol("recur"),
						coll.NewList(types.Symbol("+"), types.Symbol("sum"), types.Symbol("n")),
						coll.NewList(types.Symbol("-"), types.Symbol("n"), atom.NewNumberInt(1))),
					types.Symbol("sum"))),
			15,
		},
		// (loop [x 10] x) - 単純なループ（recurなし）
		{
			coll.NewList(types.Symbol("loop"),
				coll.NewVector(types.Symbol("x"), atom.NewNumberInt(10)),
				types.Symbol("x")),
			10,
		},
	}

	runVmTests(t, tests)
}

func TestAndExpression(t *testing.T) {
	tests := []vmTestCase{
		{coll.NewList(types.Symbol("and")), true},
		// A failing expression after false must not run.
		{
			coll.NewList(types.Symbol("and"), atom.False,
				coll.NewList(types.Symbol("/"), atom.NewNumberInt(1), atom.NewNumberInt(0))),
			false,
		},
		{
			coll.NewList(types.Symbol("and"),
				atom.NewNumberInt(1),
				atom.NewNumberInt(2),
				atom.NewNumberInt(3)),
			3,
		},
		{
			coll.NewList(types.Symbol("and"),
				atom.NewNumberInt(1),
				atom.NilValue,
				atom.NewNumberInt(3)),
			atom.NilValue,
		},
	}

	runVmTests(t, tests)
}

func TestOrExpression(t *testing.T) {
	tests := []vmTestCase{
		{coll.NewList(types.Symbol("or")), atom.NilValue},
		// A failing expression after a truthy value must not run.
		{
			coll.NewList(types.Symbol("or"),
				atom.NewNumberInt(42),
				coll.NewList(types.Symbol("/"), atom.NewNumberInt(1), atom.NewNumberInt(0))),
			42,
		},
		{
			coll.NewList(types.Symbol("or"),
				atom.NilValue,
				atom.False,
				atom.NilValue),
			atom.NilValue,
		},
	}

	runVmTests(t, tests)
}
