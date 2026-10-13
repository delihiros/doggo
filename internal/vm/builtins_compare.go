package vm

import (
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var builtinsCompare = []BuiltinDefinition{
	{
		Name: "=",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("=", args, 1); err != nil {
				return err
			}
			if len(args) < 2 {
				return atom.True
			}
			for i := 0; i < len(args)-1; i++ {
				equal, err := coll.ValueEqualsResult(args[i], args[i+1])
				if err != nil {
					return newArgumentError("%v", err)
				}
				if !equal {
					return atom.False
				}
			}
			return atom.True
		}},
	},
	{
		Name: ">",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			return orderedComparisonBuiltin(">", args, func(cmp int) bool { return cmp > 0 })
		}},
	},
	{
		Name: ">=",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			return orderedComparisonBuiltin(">=", args, func(cmp int) bool { return cmp >= 0 })
		}},
	},
	{
		Name: "<",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			return orderedComparisonBuiltin("<", args, func(cmp int) bool { return cmp < 0 })
		}},
	},
	{
		Name: "<=",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			return orderedComparisonBuiltin("<=", args, func(cmp int) bool { return cmp <= 0 })
		}},
	},
}

func orderedComparisonBuiltin(name string, args []types.Value, predicate func(int) bool) types.Value {
	if err := CheckArgMin(name, args, 1); err != nil {
		return err
	}
	if len(args) < 2 {
		return atom.True
	}
	for i := 0; i < len(args)-1; i++ {
		cmp, ordered := compareValuesOrdered(args[i], args[i+1])
		if !ordered || !predicate(cmp) {
			return atom.False
		}
	}
	return atom.True
}
