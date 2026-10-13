// builtins_math.go は数値演算の組み込み関数を定義します。
//
// 提供する関数:
//   - 算術演算: +, -, *, /, mod, quot, rem, inc, dec
//   - 比較: <, >, <=, >=
//   - 述語: even?, odd?, zero?, pos?, neg?, int?, float?, number?
//   - 数学関数: abs, max, min, rand, rand-int
package vm

import (
	"context"
	"fmt"
	"math"
	"math/rand"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func absoluteNumber(name string, value types.Value) types.Value {
	number, ok := value.(types.Number)
	if !ok {
		return newArgumentError("%s: argument must be a number, got %s", name, value.Type())
	}
	if integer, ok := types.Int64Value(number); ok {
		if integer < 0 {
			var negated bool
			integer, negated = types.CheckedNegInt64(integer)
			if !negated {
				return newArgumentError("%s: integer overflow", name)
			}
		}
		return atom.NewNumberInt(integer)
	}
	return atom.NewNumberFloat(math.Abs(number.ToFloat64()))
}

// numericBinaryFn2 は2引数の数値演算を最適化した実装を返します。
// RangeInt/atom.Number の組み合わせに対する高速整数演算を共通化。
func numericBinaryFn2(intOp func(int64, int64) (int64, bool), floatOp func(float64, float64) float64, name string) func(a, b types.Value) types.Value {
	return func(a, b types.Value) types.Value {
		left, leftOK := a.(types.Number)
		if !leftOK {
			return newArgumentError("%s: argument must be a number, got %s", name, a.Type())
		}
		right, rightOK := b.(types.Number)
		if !rightOK {
			return newArgumentError("%s: argument must be a number, got %s", name, b.Type())
		}
		leftInt, leftIsInt := types.Int64Value(left)
		rightInt, rightIsInt := types.Int64Value(right)
		if leftIsInt && rightIsInt {
			result, ok := intOp(leftInt, rightInt)
			if !ok {
				return newArgumentError("%s: integer overflow", name)
			}
			return atom.NewNumberInt(result)
		}
		return atom.NewNumberFloat(floatOp(left.ToFloat64(), right.ToFloat64()))
	}
}

func numericStep(name string, arg types.Value, delta int64) types.Value {
	n, ok := arg.(types.Number)
	if !ok {
		return newArgumentError("%s: argument must be a number, got %s", name, arg.Type())
	}
	if integer, ok := types.Int64Value(n); ok {
		result, ok := types.CheckedAddInt64(integer, delta)
		if !ok {
			return newArgumentError("%s: integer overflow", name)
		}
		return atom.NewNumberInt(result)
	}
	return atom.NewNumberFloat(n.ToFloat64() + float64(delta))
}

func reduceNumericArgs(name string, args []types.Value, identity int64, intOp func(int64, int64) (int64, bool), floatOp func(float64, float64) float64) types.Value {
	allIntegers := true
	for _, arg := range args {
		number, ok := arg.(types.Number)
		if !ok {
			return newArgumentError("%s: argument must be a number, got %s", name, arg.Type())
		}
		if _, ok := types.Int64Value(number); !ok {
			allIntegers = false
		}
	}
	if !allIntegers {
		result := float64(identity)
		for _, arg := range args {
			result = floatOp(result, arg.(types.Number).ToFloat64())
		}
		return atom.NewNumberFloat(result)
	}

	result := identity
	for _, arg := range args {
		integer, _ := types.Int64Value(arg.(types.Number))
		var ok bool
		result, ok = intOp(result, integer)
		if !ok {
			return newArgumentError("%s: integer overflow", name)
		}
	}
	return atom.NewNumberInt(result)
}

// builtinsMathは数値演算の組み込み関数を定義します
var builtinsMath = []BuiltinDefinition{
	// + - 加算（可変長引数、ReduceDirect最適化対応）
	{
		Name: "+",
		Builtin: &Builtin{
			// Phase 10k: 2引数専用（variadic アロケーション回避）
			Fn2: numericBinaryFn2(
				types.CheckedAddInt64,
				func(a, b float64) float64 { return a + b },
				"+",
			),
			Fn: func(args ...types.Value) types.Value {
				return reduceNumericArgs("+", args, 0,
					types.CheckedAddInt64,
					func(a, b float64) float64 { return a + b })
			},
		},
	},
	// - - 減算（可変長引数）
	{
		Name: "-",
		Builtin: &Builtin{
			// Phase 10k: 2引数専用（variadic アロケーション回避）
			Fn2: numericBinaryFn2(
				types.CheckedSubInt64,
				func(a, b float64) float64 { return a - b },
				"-",
			),
			Fn: func(args ...types.Value) types.Value {
				if err := CheckArgMin("-", args, 1); err != nil {
					return err
				}
				first, ok := args[0].(types.Number)
				if !ok {
					return newArgumentError("-: argument must be a number, got %s", args[0].Type())
				}
				if len(args) == 1 {
					if integer, ok := types.Int64Value(first); ok {
						result, ok := types.CheckedNegInt64(integer)
						if !ok {
							return newArgumentError("-: integer overflow")
						}
						return atom.NewNumberInt(result)
					}
					return atom.NewNumberFloat(-first.ToFloat64())
				}
				allIntegers := true
				for _, arg := range args[1:] {
					n, ok := arg.(types.Number)
					if !ok {
						return newArgumentError("-: argument must be a number, got %s", arg.Type())
					}
					if _, ok := types.Int64Value(n); !ok {
						allIntegers = false
					}
				}
				if _, ok := types.Int64Value(first); !ok {
					allIntegers = false
				}
				if !allIntegers {
					result := first.ToFloat64()
					for _, arg := range args[1:] {
						result -= arg.(types.Number).ToFloat64()
					}
					return atom.NewNumberFloat(result)
				}

				result, _ := types.Int64Value(first)
				for _, arg := range args[1:] {
					integer, _ := types.Int64Value(arg.(types.Number))
					var ok bool
					result, ok = types.CheckedSubInt64(result, integer)
					if !ok {
						return newArgumentError("-: integer overflow")
					}
				}
				return atom.NewNumberInt(result)
			},
		},
	},
	// * - 乗算（可変長引数、ReduceDirect最適化対応）
	{
		Name: "*",
		Builtin: &Builtin{
			// Phase 10k: 2引数専用（variadic アロケーション回避）
			Fn2: numericBinaryFn2(
				types.CheckedMulInt64,
				func(a, b float64) float64 { return a * b },
				"*",
			),
			Fn: func(args ...types.Value) types.Value {
				return reduceNumericArgs("*", args, 1,
					types.CheckedMulInt64,
					func(a, b float64) float64 { return a * b })
			},
		},
	},
	// / - 除算（可変長引数）
	{
		Name: "/",
		Builtin: &Builtin{
			// Phase 10k: 2引数専用（variadic アロケーション回避）
			Fn2: func(a, b types.Value) types.Value {
				va, ok1 := toNumber(a)
				vb, ok2 := toNumber(b)
				if !ok1 {
					return newArgumentError("/: argument must be a number, got %s", a.Type())
				}
				if !ok2 {
					return newArgumentError("/: argument must be a number, got %s", b.Type())
				}
				if vb == 0 {
					return newArgumentError("/: division by zero")
				}
				return atom.NewNumberFloat(va / vb)
			},
			Fn: func(args ...types.Value) types.Value {
				if err := CheckArgMin("/", args, 1); err != nil {
					return err
				}
				first, ok := toNumber(args[0])
				if !ok {
					return newArgumentError("/: argument must be a number, got %s", args[0].Type())
				}
				if len(args) == 1 {
					// (/ x) = 1/x
					if first == 0 {
						return newArgumentError("/: division by zero")
					}
					return atom.NewNumberFloat(1.0 / first)
				}
				result := first
				for _, arg := range args[1:] {
					n, ok := toNumber(arg)
					if !ok {
						return newArgumentError("/: argument must be a number, got %s", arg.Type())
					}
					if n == 0 {
						return newArgumentError("/: division by zero")
					}
					result /= n
				}
				return atom.NewNumberFloat(result)
			},
		},
	},
	{
		Name: "abs",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("abs", args, 1); err != nil {
				return err
			}
			return absoluteNumber("abs", args[0])
		}},
	},
	{
		Name: "max",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			return numberExtremum("max", args, func(cmp int) bool { return cmp > 0 })
		}},
	},
	{
		Name: "min",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			return numberExtremum("min", args, func(cmp int) bool { return cmp < 0 })
		}},
	},
	{
		Name: "rem",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("rem", args, 2); err != nil {
				return err
			}
			num, ok := integerForOperation(args[0])
			if !ok {
				return newArgumentError("rem: first argument must be a finite number in int64 range, got %s", args[0].Type())
			}
			divisor, ok := integerForOperation(args[1])
			if !ok {
				return newArgumentError("rem: second argument must be a finite number in int64 range, got %s", args[1].Type())
			}
			if divisor == 0 {
				return newArgumentError("rem: division by zero")
			}
			return atom.NewNumberInt(num % divisor)
		}},
	},
	// mod: Clojure-style modulo (result has the same sign as the divisor)
	{
		Name: "mod",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("mod", args, 2); err != nil {
				return err
			}
			num, ok := integerForOperation(args[0])
			if !ok {
				return newArgumentError("mod: first argument must be a finite number in int64 range, got %s", args[0].Type())
			}
			divisor, ok := integerForOperation(args[1])
			if !ok {
				return newArgumentError("mod: second argument must be a finite number in int64 range, got %s", args[1].Type())
			}
			result, ok := types.ModInt64(num, divisor)
			if !ok {
				return newArgumentError("mod: division by zero")
			}
			return atom.NewNumberInt(result)
		}},
	},
	{
		Name: "rand",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("rand", args, 0, 1); err != nil {
				return err
			}
			if len(args) == 0 {
				return atom.NewNumberFloat(rand.Float64())
			}
			number, ok := args[0].(types.Number)
			if !ok {
				return newArgumentError("rand: argument must be a finite number, got %s", args[0].Type())
			}
			n := number.ToFloat64()
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return newArgumentError("rand: argument must be a finite number")
			}
			if n <= 0 {
				return newArgumentError("rand: argument must be positive, got %g", n)
			}
			return atom.NewNumberFloat(rand.Float64() * n)
		}},
	},
	{
		Name: "rand-int",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("rand-int", args, 1); err != nil {
				return err
			}
			n, ok := integerForOperation(args[0])
			if !ok {
				return newArgumentError("rand-int: argument must be a finite number in int64 range, got %s", args[0].Type())
			}
			if n <= 0 {
				return newArgumentError("rand-int: argument must be positive, got %d", n)
			}
			return atom.NewNumberInt(rand.Int63n(n))
		}},
	},
	// inc - 数値に1を加算（最適化対応）
	{
		Name: "inc",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("inc", args, 1); err != nil {
				return err
			}
			return numericStep("inc", args[0], 1)
		}},
	},
	// dec - 数値から1を減算（最適化対応）
	{
		Name: "dec",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("dec", args, 1); err != nil {
				return err
			}
			return numericStep("dec", args[0], -1)
		}},
	},
	// even? - 偶数判定（最適化対応）
	{
		Name: "even?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("even?", args, 1); err != nil {
				return err
			}
			n, ok := integerForOperation(args[0])
			if !ok {
				return newArgumentError("even?: argument must be a finite number in int64 range, got %s", args[0].Type())
			}
			return atom.Boolean(n%2 == 0)
		}},
	},
	// odd? - 奇数判定（最適化対応）
	{
		Name: "odd?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("odd?", args, 1); err != nil {
				return err
			}
			n, ok := integerForOperation(args[0])
			if !ok {
				return newArgumentError("odd?: argument must be a finite number in int64 range, got %s", args[0].Type())
			}
			return atom.Boolean(n%2 != 0)
		}},
	},

	// min-key - Returns the item for which (k item) is minimal
	// (min-key k x) or (min-key k x y) or (min-key k x y & more)
	{
		Name:    "min-key",
		Builtin: &Builtin{CtxFn: keyExtremumCtxFn("min-key", func(cmp int) bool { return cmp < 0 })},
	},

	// max-key - Returns the item for which (k item) is maximal
	// (max-key k x) or (max-key k x y) or (max-key k x y & more)
	{
		Name:    "max-key",
		Builtin: &Builtin{CtxFn: keyExtremumCtxFn("max-key", func(cmp int) bool { return cmp > 0 })},
	},
}

func numberExtremum(fnName string, args []types.Value, better func(cmp int) bool) types.Value {
	if err := CheckArgMin(fnName, args, 1); err != nil {
		return err
	}
	best, ok := args[0].(types.Number)
	if !ok {
		return newArgumentError("%s: all arguments must be numbers, got %s", fnName, args[0].Type())
	}
	for i := 1; i < len(args); i++ {
		num, ok := args[i].(types.Number)
		if !ok {
			return newArgumentError("%s: all arguments must be numbers, got %s", fnName, args[i].Type())
		}
		cmp, ordered := types.CompareNumbers(num, best)
		if ordered && better(cmp) {
			best = num
		}
	}
	return best
}

func keyExtremumCtxFn(fnName string, better func(cmp int) bool) func(context.Context, ...types.Value) (types.Value, error) {
	return func(ctx context.Context, args ...types.Value) (types.Value, error) {
		if err := CheckArgMinCtx(fnName, args, 2); err != nil {
			return nil, err
		}

		vm := GetVM(ctx)
		if vm == nil {
			return nil, fmt.Errorf("%s: VM not available in context", fnName)
		}

		keyFn := args[0]
		items := args[1:]
		bestItem := items[0]
		bestVal, err := keyNumberValue(ctx, vm, fnName, keyFn, bestItem)
		if err != nil {
			return nil, err
		}

		for i := 1; i < len(items); i++ {
			value, err := keyNumberValue(ctx, vm, fnName, keyFn, items[i])
			if err != nil {
				return nil, err
			}
			cmp, ordered := types.CompareNumbers(value, bestVal)
			if ordered && better(cmp) {
				bestVal = value
				bestItem = items[i]
			}
		}

		return bestItem, nil
	}
}

func keyNumberValue(ctx context.Context, vm *VM, fnName string, keyFn, item types.Value) (types.Number, error) {
	keyVal, err := ApplyCallableFast(ctx, vm, keyFn, []types.Value{item})
	if err != nil {
		return nil, err
	}
	num, ok := keyVal.(types.Number)
	if !ok {
		return nil, newArgumentError("%s: key function must return a number, got %s", fnName, keyVal.Type())
	}
	return num, nil
}
