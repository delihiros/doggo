// builtins_string.go は基本的な文字列操作の組み込み関数を定義します。
//
// 提供する関数:
//   - 変換: str, subs
//   - 操作: split, join, trim, upper-case, lower-case
//   - 述語: string?
package vm

import (
	"strings"

	"doggo/internal/textutil"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsStringは文字列操作の組み込み関数を定義します
var builtinsString = []BuiltinDefinition{
	{
		Name: "str",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if len(args) == 0 {
				return atom.NewString("")
			}
			parts := make([]string, 0, len(args))
			for _, arg := range args {
				switch v := arg.(type) {
				case atom.String:
					parts = append(parts, string(v))
				case types.Nil:
					// nil は空文字列として扱う
				case types.Displayable:
					// Displayable を実装している型は Display() を使用（人間が読みやすい形式）
					parts = append(parts, v.Display())
				default:
					parts = append(parts, v.String())
				}
			}
			return atom.NewString(strings.Join(parts, ""))
		}},
	},
	{
		Name: "keyword",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			// (keyword name) または (keyword ns name)
			switch len(args) {
			case 1:
				switch v := args[0].(type) {
				case atom.String:
					return atom.NewKeyword(string(v))
				case types.Symbol:
					return atom.NewKeyword(string(v))
				case atom.Keyword:
					return v
				default:
					return newArgumentError("keyword: argument must be string, symbol or keyword, got %s", args[0].Type())
				}
			case 2:
				ns, ok := args[0].(atom.String)
				if !ok {
					return newArgumentError("keyword: first argument must be string, got %s", args[0].Type())
				}
				name, ok := args[1].(atom.String)
				if !ok {
					return newArgumentError("keyword: second argument must be string, got %s", args[1].Type())
				}
				return atom.NewKeyword(string(ns) + "/" + string(name))
			default:
				return newArgumentError("keyword: wrong number of arguments. got=%d, want=1 or 2", len(args))
			}
		}},
	},
	{
		Name: "split",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("split", args, 2); err != nil {
				return err
			}
			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("split: first argument must be a string, got %s", args[0].Type())
			}
			sep, ok := args[1].(atom.String)
			if !ok {
				return newArgumentError("split: second argument must be a string, got %s", args[1].Type())
			}
			parts := strings.Split(string(s), string(sep))
			items := make([]types.Value, len(parts))
			for i, p := range parts {
				items[i] = atom.NewString(p)
			}
			return coll.NewVector(items...)
		}},
	},
	{
		Name: "join",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("join", args, 2); err != nil {
				return err
			}
			sep, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("join: first argument must be a string, got %s", args[0].Type())
			}

			var items []types.Value
			switch c := args[1].(type) {
			case *coll.Vector:
				items = c.Items
			case *coll.List:
				items = c.Items
			case types.Nil:
				return atom.NewString("")
			default:
				return newArgumentError("join: second argument must be a sequence, got %s", args[1].Type())
			}

			strs := make([]string, len(items))
			for i, item := range items {
				switch v := item.(type) {
				case atom.String:
					strs[i] = string(v)
				case types.Nil:
					strs[i] = ""
				default:
					strs[i] = v.String()
				}
			}
			return atom.NewString(strings.Join(strs, string(sep)))
		}},
	},
	{
		Name: "subs",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("subs", args, 2, 3); err != nil {
				return err
			}
			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("subs: first argument must be a string, got %s", args[0].Type())
			}
			startValue, ok := integerForOperation(args[1])
			if !ok {
				return newArgumentError("subs: second argument must be a number, got %s", args[1].Type())
			}
			start := int(startValue)
			length := textutil.CodePointLen(string(s))

			if start < 0 || start > length {
				return newArgumentError("subs: start index out of bounds: %d (length: %d)", start, length)
			}

			end := length
			if len(args) == 3 {
				endValue, ok := integerForOperation(args[2])
				if !ok {
					return newArgumentError("subs: third argument must be a number, got %s", args[2].Type())
				}
				end = int(endValue)
				if end < start || end > length {
					return newArgumentError("subs: end index out of bounds: %d (start: %d, length: %d)", end, start, length)
				}
			}

			substr, ok := textutil.CodePointSlice(string(s), start, end)
			if !ok {
				return newArgumentError("subs: invalid code-point slice %d:%d", start, end)
			}
			return atom.NewString(substr)
		}},
	},
	{
		Name: "trim",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("trim", args, 1); err != nil {
				return err
			}
			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("trim: argument must be a string, got %s", args[0].Type())
			}
			return atom.NewString(strings.TrimSpace(string(s)))
		}},
	},
	{
		Name: "upper-case",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("upper-case", args, 1); err != nil {
				return err
			}
			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("upper-case: argument must be a string, got %s", args[0].Type())
			}
			return atom.NewString(strings.ToUpper(string(s)))
		}},
	},
	{
		Name: "lower-case",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("lower-case", args, 1); err != nil {
				return err
			}
			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("lower-case: argument must be a string, got %s", args[0].Type())
			}
			return atom.NewString(strings.ToLower(string(s)))
		}},
	},
}
