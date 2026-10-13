package vm

import (
	"fmt"
	stdmath "math"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

const (
	maxRoundedInt64     = int64(^uint64(0) >> 1)
	minRoundedInt64     = -maxRoundedInt64 - 1
	maxClojureInt32     = int64(1<<31 - 1)
	minClojureInt32     = -1 << 31
	float64ExponentBias = 1023
	float64ExponentMax  = 1024
)

func mathUnaryFloatBuiltin(name string, op func(float64) float64) BuiltinDefinition {
	return BuiltinDefinition{
		Name: name,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount(name, args, 1); err != nil {
				return err
			}
			value, ok := toNumber(args[0])
			if !ok {
				return newArgumentError("%s: argument must be a number, got %s", name, args[0].Type())
			}
			return atom.NewNumberFloat(op(value))
		}},
	}
}

func mathBinaryFloatBuiltin(name string, op func(float64, float64) float64) BuiltinDefinition {
	return BuiltinDefinition{
		Name: name,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount(name, args, 2); err != nil {
				return err
			}
			left, ok := toNumber(args[0])
			if !ok {
				return newArgumentError("%s: argument must be a number, got %s", name, args[0].Type())
			}
			right, ok := toNumber(args[1])
			if !ok {
				return newArgumentError("%s: argument must be a number, got %s", name, args[1].Type())
			}
			return atom.NewNumberFloat(op(left, right))
		}},
	}
}

func mathUnaryIntBuiltin(name string, op func(float64) int64) BuiltinDefinition {
	return BuiltinDefinition{
		Name: name,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount(name, args, 1); err != nil {
				return err
			}
			value, ok := toNumber(args[0])
			if !ok {
				return newArgumentError("%s: argument must be a number, got %s", name, args[0].Type())
			}
			return atom.NewNumberInt(op(value))
		}},
	}
}

func mathBinaryFloatScaleBuiltin(name string, op func(float64, int) float64) BuiltinDefinition {
	return BuiltinDefinition{
		Name: name,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount(name, args, 2); err != nil {
				return err
			}
			value, ok := toNumber(args[0])
			if !ok {
				return newArgumentError("%s: first argument must be a number, got %s", name, args[0].Type())
			}
			scale, ok := toNumber(args[1])
			if !ok {
				return newArgumentError("%s: second argument must be a number, got %s", name, args[1].Type())
			}
			scaleInt, err := mathIntLikeClojure(scale)
			if err != nil {
				return newArgumentError("%s: %s", name, err.Error())
			}
			return atom.NewNumberFloat(op(value, scaleInt))
		}},
	}
}

func mathGetExponent(value float64) int64 {
	bits := stdmath.Float64bits(value)
	exponentBits := int64((bits >> 52) & 0x7ff)
	if exponentBits == 0 {
		return -float64ExponentBias
	}
	if exponentBits == 0x7ff {
		return float64ExponentMax
	}
	return exponentBits - float64ExponentBias
}

func mathULP(value float64) float64 {
	switch {
	case stdmath.IsNaN(value):
		return stdmath.NaN()
	case stdmath.IsInf(value, 0):
		return stdmath.Inf(1)
	}

	exponentBits := int64((stdmath.Float64bits(stdmath.Abs(value)) >> 52) & 0x7ff)
	if exponentBits == 0 {
		return stdmath.SmallestNonzeroFloat64
	}
	return stdmath.Ldexp(1, int(exponentBits-float64ExponentBias-52))
}

func mathIntLikeClojure(value float64) (int, error) {
	switch {
	case stdmath.IsNaN(value):
		return 0, nil
	case stdmath.IsInf(value, 1):
		//lint:ignore ST1005 language-facing diagnostic is covered by stdlib tests
		return 0, fmt.Errorf("Value out of range for long: Infinity")
	case stdmath.IsInf(value, -1):
		//lint:ignore ST1005 language-facing diagnostic matches positive infinity form
		return 0, fmt.Errorf("Value out of range for long: -Infinity")
	case value > float64(maxRoundedInt64) || value < float64(minRoundedInt64):
		//lint:ignore ST1005 language-facing diagnostic is covered by stdlib tests
		return 0, fmt.Errorf("Value out of range for long: %v", value)
	}

	truncated := int64(value)
	if truncated > maxClojureInt32 || truncated < minClojureInt32 {
		return 0, fmt.Errorf("integer overflow")
	}
	return int(truncated), nil
}

var builtinsMathExtra = []BuiltinDefinition{
	{
		Name:       "math-abs*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("math-abs*", args, 1); err != nil {
				return err
			}
			return absoluteNumber("math-abs*", args[0])
		}},
	},
	privateBuiltin(mathUnaryFloatBuiltin("math-sqrt*", stdmath.Sqrt)),
	privateBuiltin(mathBinaryFloatBuiltin("math-pow*", stdmath.Pow)),
	privateBuiltin(mathUnaryFloatBuiltin("math-exp*", stdmath.Exp)),
	privateBuiltin(mathUnaryFloatBuiltin("math-expm1*", stdmath.Expm1)),
	privateBuiltin(mathUnaryFloatBuiltin("math-log*", stdmath.Log)),
	privateBuiltin(mathUnaryFloatBuiltin("math-log10*", stdmath.Log10)),
	privateBuiltin(mathUnaryFloatBuiltin("math-log1p*", stdmath.Log1p)),
	privateBuiltin(mathUnaryFloatBuiltin("math-cbrt*", stdmath.Cbrt)),
	privateBuiltin(mathUnaryFloatBuiltin("math-sin*", stdmath.Sin)),
	privateBuiltin(mathUnaryFloatBuiltin("math-sinh*", stdmath.Sinh)),
	privateBuiltin(mathUnaryFloatBuiltin("math-cos*", stdmath.Cos)),
	privateBuiltin(mathUnaryFloatBuiltin("math-cosh*", stdmath.Cosh)),
	privateBuiltin(mathUnaryFloatBuiltin("math-tan*", stdmath.Tan)),
	privateBuiltin(mathUnaryFloatBuiltin("math-tanh*", stdmath.Tanh)),
	privateBuiltin(mathUnaryFloatBuiltin("math-asin*", stdmath.Asin)),
	privateBuiltin(mathUnaryFloatBuiltin("math-acos*", stdmath.Acos)),
	privateBuiltin(mathUnaryFloatBuiltin("math-atan*", stdmath.Atan)),
	privateBuiltin(mathBinaryFloatBuiltin("math-atan2*", stdmath.Atan2)),
	privateBuiltin(mathBinaryFloatBuiltin("math-hypot*", stdmath.Hypot)),
	privateBuiltin(mathBinaryFloatBuiltin("math-copy-sign*", stdmath.Copysign)),
	privateBuiltin(mathBinaryFloatBuiltin("math-next-after*", stdmath.Nextafter)),
	privateBuiltin(mathUnaryFloatBuiltin("math-floor*", stdmath.Floor)),
	privateBuiltin(mathUnaryFloatBuiltin("math-ceil*", stdmath.Ceil)),
	privateBuiltin(mathUnaryFloatBuiltin("math-rint*", stdmath.RoundToEven)),
	privateBuiltin(mathUnaryFloatBuiltin("math-next-up*", func(value float64) float64 {
		return stdmath.Nextafter(value, stdmath.Inf(1))
	})),
	privateBuiltin(mathUnaryFloatBuiltin("math-next-down*", func(value float64) float64 {
		return stdmath.Nextafter(value, stdmath.Inf(-1))
	})),
	privateBuiltin(mathUnaryFloatBuiltin("math-ulp*", mathULP)),
	privateBuiltin(mathUnaryIntBuiltin("math-get-exponent*", mathGetExponent)),
	privateBuiltin(mathBinaryFloatScaleBuiltin("math-scalb*", stdmath.Ldexp)),
	privateBuiltin(mathUnaryFloatBuiltin("math-to-degrees*", func(value float64) float64 {
		return value * 180.0 / stdmath.Pi
	})),
	privateBuiltin(mathUnaryFloatBuiltin("math-to-radians*", func(value float64) float64 {
		return value * stdmath.Pi / 180.0
	})),
	{
		Name:       "math-round*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("math-round*", args, 1); err != nil {
				return err
			}
			value, ok := toNumber(args[0])
			if !ok {
				return newArgumentError("math-round*: argument must be a number, got %s", args[0].Type())
			}
			switch {
			case stdmath.IsNaN(value):
				return atom.NewNumberInt(0)
			case stdmath.IsInf(value, 1):
				return atom.NewNumberInt(maxRoundedInt64)
			case stdmath.IsInf(value, -1):
				return atom.NewNumberInt(minRoundedInt64)
			}
			return atom.NewNumberInt(int64(stdmath.Round(value)))
		}},
	},
	{
		Name:       "math-signum*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("math-signum*", args, 1); err != nil {
				return err
			}
			value, ok := toNumber(args[0])
			if !ok {
				return newArgumentError("math-signum*: argument must be a number, got %s", args[0].Type())
			}
			switch {
			case stdmath.IsNaN(value):
				return atom.NewNumberFloat(stdmath.NaN())
			case value < 0:
				return atom.NewNumberFloat(-1.0)
			case value > 0:
				return atom.NewNumberFloat(1.0)
			default:
				return atom.NewNumberFloat(0.0)
			}
		}},
	},
}

func privateBuiltin(def BuiltinDefinition) BuiltinDefinition {
	def.Visibility = BuiltinPrivate
	return def
}
