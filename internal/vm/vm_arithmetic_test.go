package vm

import (
	"strings"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestFractionalModuloDivisorReturnsError(t *testing.T) {
	assertRunErrorContains(t, MustNewRunner(), `(mod 1 0.5)`, "division by zero")
}

func TestVariadicMixedArithmeticPromotesBeforeIntegerOverflow(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[
		(pos? (+ 9223372036854775807 1 0.5))
		(pos? (* 3037000500 3037000500 1.0))
		(neg? (- -9223372036854775808 1 0.5))
	]`, "[true true true]")
}

func TestAbsMinInt64ReturnsOverflowError(t *testing.T) {
	assertRunErrorContains(t, MustNewRunner(), `(abs -9223372036854775808)`, "abs: integer overflow")
}

func TestIntegerArithmeticOverflowReturnsErrorAcrossCallPaths(t *testing.T) {
	runner := MustNewRunner()
	for _, expression := range []string{
		`(+ 9223372036854775807 1)`,
		`(let [f +] (f 9223372036854775807 1))`,
		`(+ 9223372036854775807 1 0)`,
		`(- -9223372036854775808 1)`,
		`(* 3037000500 3037000500)`,
		`(inc 9223372036854775807)`,
		`(dec -9223372036854775808)`,
		`(- -9223372036854775808)`,
	} {
		_, err := runner.Run(expression)
		if err == nil || !strings.Contains(err.Error(), "integer overflow") {
			t.Fatalf("Run(%q) error = %v, want integer overflow", expression, err)
		}
	}
}

func TestOperatorCallsRespectBindingsAndBuiltinSemantics(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[
		(let [+ (fn [& xs] 99)] [(+ 1 2) (+ 1 2 3)])
		(let [< (fn [a b] :shadowed)] (< 1 2))
		(= 1 1 1)
		(not= 1 1 2)
		(< "a" "b")
	]`, "[[99 99] :shadowed true true true]")
	assertRunResult(t, runner, `(do (def + (fn [& xs] 77)) [(+ 1 2) (+ 1 2 3)])`, "[77 77]")
}

func TestIntegerOperationsRejectOutOfRangeFloats(t *testing.T) {
	runner := MustNewRunner()
	for _, expr := range []string{
		`(mod 1 1e100)`,
		`(let [f mod] (f 1 1e100))`,
		`(rem 1 1e100)`,
		`(even? 1e100)`,
		`(rand-int 1e100)`,
	} {
		if _, err := runner.Run(expr); err == nil {
			t.Fatalf("Run(%q) succeeded, want range error", expr)
		}
	}
}

func TestModuloReturnsIntegerAcrossCallPaths(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		(int? (mod 5 2.0))
		(int? (let [f mod] (f 5 2.0)))
		(rem 10 3)
		(rem -10 3)
		(mod 10 3)
		(mod -10 3)
		(mod 10 -3)
	]`, "[true true 1 -1 1 2 -2]")
}

func TestUnaryNegationSupportsAllNumberImplementations(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[(int? (- (first (range 1 2)))) (- (first (range 1 2)))]`, "[true -1]")
}

func TestReduceDirectPropagatesBuiltinReducerErrors(t *testing.T) {
	runner := MustNewRunner()
	for _, expression := range []string{
		`(reduce + 0 (map (fn [x] (if (= x 1) "bad" x)) (range 3)))`,
		`(reduce + (map (fn [x] (if (= x 1) "bad" x)) (range 3)))`,
	} {
		_, err := runner.Run(expression)
		if err == nil {
			t.Fatalf("Run(%q) succeeded, want reducer error", expression)
		}
		if !strings.Contains(err.Error(), "+: argument must be a number, got string") {
			t.Fatalf("Run(%q) error = %q, want original reducer error", expression, err)
		}
		if strings.Contains(err.Error(), "got error") {
			t.Fatalf("Run(%q) reduced an error as a value: %q", expression, err)
		}
	}
}

func TestIntegerArithmetic(t *testing.T) {
	tests := []vmTestCase{
		// 単純な数値
		{atom.NewNumberInt(1), 1},
		// 加算
		{
			coll.NewList(types.Symbol("+"), atom.NewNumberInt(1), atom.NewNumberInt(2)),
			3,
		},
		// 減算
		{
			coll.NewList(types.Symbol("-"), atom.NewNumberInt(1), atom.NewNumberInt(2)),
			-1,
		},
		// 乗算
		{
			coll.NewList(types.Symbol("*"), atom.NewNumberInt(2), atom.NewNumberInt(3)),
			6,
		},
		// 除算
		{
			coll.NewList(types.Symbol("/"), atom.NewNumberInt(6), atom.NewNumberInt(2)),
			3.0,
		},
		// 単項マイナス
		{
			coll.NewList(types.Symbol("-"), atom.NewNumberInt(5)),
			-5,
		},
	}

	runVmTests(t, tests)
}

func TestLargeIntegerArithmeticAndComparisonRemainExact(t *testing.T) {
	runner := MustNewRunner()

	tests := []struct {
		name string
		code string
		want string
	}{
		{name: "opcode addition", code: `(+ 9007199254740992 1)`, want: "9007199254740993"},
		{name: "dynamic addition", code: `(let [f +] (f 9007199254740992 1))`, want: "9007199254740993"},
		{name: "variadic addition", code: `(+ 9007199254740992 1 0)`, want: "9007199254740993"},
		{name: "opcode subtraction", code: `(- 9007199254740993 1)`, want: "9007199254740992"},
		{name: "dynamic subtraction", code: `(let [f -] (f 9007199254740993 1))`, want: "9007199254740992"},
		{name: "unary subtraction", code: `(let [f -] [(int? (f 9007199254740993)) (f 9007199254740993)])`, want: "[true -9007199254740993]"},
		{name: "variadic multiplication", code: `(* 9007199254740993 1 1)`, want: "9007199254740993"},
		{name: "exact equality", code: `(= 9007199254740992 9007199254740993)`, want: "false"},
		{name: "dynamic ordering", code: `(let [f <] (f 9007199254740992 9007199254740993))`, want: "true"},
		{name: "exact sort", code: `(vec (sort [9007199254740993 9007199254740992]))`, want: "[9007199254740992 9007199254740993]"},
		{name: "set equality", code: `(count #{9007199254740992 9007199254740993})`, want: "2"},
		{name: "exact modulo", code: `(mod 9007199254740993 2)`, want: "1"},
		{name: "exact remainder", code: `(rem 9007199254740993 2)`, want: "1"},
		{name: "exact parity", code: `[(even? 9007199254740993) (odd? 9007199254740993)]`, want: "[false true]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, runner, tt.code, tt.want)
		})
	}
}

func TestBooleanExpressions(t *testing.T) {
	tests := []vmTestCase{
		{atom.True, true},
		{atom.False, false},
		// 比較演算
		{
			coll.NewList(types.Symbol("="), atom.NewNumberInt(1), atom.NewNumberInt(1)),
			true,
		},
		{
			coll.NewList(types.Symbol("="), atom.NewNumberInt(1), atom.NewNumberInt(2)),
			false,
		},
		{
			coll.NewList(types.Symbol(">"), atom.NewNumberInt(2), atom.NewNumberInt(1)),
			true,
		},
		{
			coll.NewList(types.Symbol("<"), atom.NewNumberInt(2), atom.NewNumberInt(1)),
			false,
		},
		// 論理否定
		{
			coll.NewList(types.Symbol("not"), atom.True),
			false,
		},
		{
			coll.NewList(types.Symbol("not"), atom.False),
			true,
		},
	}

	runVmTests(t, tests)
}
