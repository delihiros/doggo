// builtins_set_ops.go はセット演算の組み込み関数を定義します。
// union, intersection, difference, subset?, superset? をサポートします。
package vm

import (
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsSetOps はセット演算の組み込み関数を定義します
var builtinsSetOps = []BuiltinDefinition{
	// union - 和集合
	{
		Name: "union",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if len(args) == 0 {
				return coll.NewSet()
			}

			result := coll.NewSet()
			for _, arg := range args {
				if arg == nil || arg == atom.NilValue {
					continue
				}
				set, ok := arg.(*coll.Set)
				if !ok {
					return newArgumentError("union: expected set, got %s", arg.Type())
				}
				// 全要素を追加
				for _, elem := range set.AllElements() {
					result = result.Conj(elem).(*coll.Set)
				}
			}
			return result
		}},
	},

	// intersection - 積集合
	{
		Name: "intersection",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if len(args) == 0 {
				return coll.NewSet()
			}
			if len(args) == 1 {
				if args[0] == nil || args[0] == atom.NilValue {
					return coll.NewSet()
				}
				set, ok := args[0].(*coll.Set)
				if !ok {
					return newArgumentError("intersection: expected set, got %s", args[0].Type())
				}
				return set
			}

			// 最初のセットを取得
			first, ok := args[0].(*coll.Set)
			if !ok {
				if args[0] == nil || args[0] == atom.NilValue {
					return coll.NewSet()
				}
				return newArgumentError("intersection: expected set, got %s", args[0].Type())
			}

			// 全セットに含まれる要素のみを残す
			result := coll.NewSet()
			for _, elem := range first.AllElements() {
				inAll := true
				for _, arg := range args[1:] {
					if arg == nil || arg == atom.NilValue {
						inAll = false
						break
					}
					other, ok := arg.(*coll.Set)
					if !ok {
						return newArgumentError("intersection: expected set, got %s", arg.Type())
					}
					if !other.Contains(elem) {
						inAll = false
						break
					}
				}
				if inAll {
					result = result.Conj(elem).(*coll.Set)
				}
			}
			return result
		}},
	},

	// difference - 差集合
	{
		Name: "difference",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if len(args) == 0 {
				return coll.NewSet()
			}

			first, ok := args[0].(*coll.Set)
			if !ok {
				if args[0] == nil || args[0] == atom.NilValue {
					return coll.NewSet()
				}
				return newArgumentError("difference: expected set, got %s", args[0].Type())
			}

			if len(args) == 1 {
				return first
			}

			// 他のセットに含まれない要素のみを残す
			result := coll.NewSet()
			for _, elem := range first.AllElements() {
				inOther := false
				for _, arg := range args[1:] {
					if arg == nil || arg == atom.NilValue {
						continue
					}
					other, ok := arg.(*coll.Set)
					if !ok {
						return newArgumentError("difference: expected set, got %s", arg.Type())
					}
					if other.Contains(elem) {
						inOther = true
						break
					}
				}
				if !inOther {
					result = result.Conj(elem).(*coll.Set)
				}
			}
			return result
		}},
	},

	// subset? - 部分集合判定
	{
		Name: "subset?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("subset?", args, 2); err != nil {
				return err
			}
			return setSubsetValue("subset?", args[0], args[1], "first", "second")
		}},
	},

	// superset? - 上位集合判定
	{
		Name: "superset?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("superset?", args, 2); err != nil {
				return err
			}
			return setSubsetValue("superset?", args[1], args[0], "second", "first")
		}},
	},
}

func setSubsetValue(fnName string, candidateValue, containerValue types.Value, candidateLabel, containerLabel string) types.Value {
	if candidateValue == nil || candidateValue == atom.NilValue {
		return atom.True
	}

	candidate, ok := candidateValue.(*coll.Set)
	if !ok {
		return newArgumentError("%s: %s argument must be a set, got %s", fnName, candidateLabel, candidateValue.Type())
	}

	if containerValue == nil || containerValue == atom.NilValue {
		return atom.Boolean(candidate.Count() == 0)
	}

	container, ok := containerValue.(*coll.Set)
	if !ok {
		return newArgumentError("%s: %s argument must be a set, got %s", fnName, containerLabel, containerValue.Type())
	}

	for _, elem := range candidate.AllElements() {
		if !container.Contains(elem) {
			return atom.False
		}
	}
	return atom.True
}
