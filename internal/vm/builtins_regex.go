// builtins_regex.go は正規表現操作の組み込み関数を定義します。
//
// 正規表現は Clojure の #"pattern" リテラルに相当します。
// Go の regexp パッケージを使用して実装されています。
//
// 提供する関数:
//   - 作成: re-pattern
//   - マッチ: re-find, re-matches, re-seq
//   - 情報取得: re-groups
//   - 述語: regex?
package vm

import (
	"regexp"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsRegex は正規表現操作の組み込み関数を定義します
var builtinsRegex = []BuiltinDefinition{
	{
		Name: "regex?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("regex?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*atom.Regex)
			return atom.NewBoolean(ok)
		}},
	},
	{
		Name: "re-pattern",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("re-pattern", args, 1); err != nil {
				return err
			}
			switch v := args[0].(type) {
			case *atom.Regex:
				return v
			case atom.String:
				regex, err := atom.NewRegex(string(v))
				if err != nil {
					return newArgumentError("re-pattern: invalid pattern: %v", err)
				}
				return regex
			default:
				return newArgumentError("re-pattern: expected string or regex, got %s", args[0].Type())
			}
		}},
	},
	{
		Name: "re-find",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("re-find", args, 2); err != nil {
				return err
			}
			regex, ok := args[0].(*atom.Regex)
			if !ok {
				return newArgumentError("re-find: first argument must be a regex, got %s", args[0].Type())
			}
			str, ok := args[1].(atom.String)
			if !ok {
				return newArgumentError("re-find: second argument must be a string, got %s", args[1].Type())
			}
			return regexMatchValue(string(str), regex.Re.FindStringSubmatchIndex(string(str)))
		}},
	},
	{
		Name: "re-matches",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("re-matches", args, 2); err != nil {
				return err
			}
			regex, ok := args[0].(*atom.Regex)
			if !ok {
				return newArgumentError("re-matches: first argument must be a regex, got %s", args[0].Type())
			}
			str, ok := args[1].(atom.String)
			if !ok {
				return newArgumentError("re-matches: second argument must be a string, got %s", args[1].Type())
			}
			anchored, err := regexp.Compile(`\A(?:` + regex.Pattern + `)\z`)
			if err != nil {
				return newArgumentError("re-matches: invalid anchored pattern: %v", err)
			}
			s := string(str)
			return regexMatchValue(s, anchored.FindStringSubmatchIndex(s))
		}},
	},
	{
		Name: "re-seq",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("re-seq", args, 2); err != nil {
				return err
			}
			regex, ok := args[0].(*atom.Regex)
			if !ok {
				return newArgumentError("re-seq: first argument must be a regex, got %s", args[0].Type())
			}
			str, ok := args[1].(atom.String)
			if !ok {
				return newArgumentError("re-seq: second argument must be a string, got %s", args[1].Type())
			}
			s := string(str)
			matches := regex.Re.FindAllStringSubmatchIndex(s, -1)
			if matches == nil {
				return coll.NewVector()
			}
			items := make([]types.Value, len(matches))
			for i, indexes := range matches {
				items[i] = regexMatchValue(s, indexes)
			}
			return coll.NewVector(items...)
		}},
	},
}

func regexMatchValue(input string, indexes []int) types.Value {
	if indexes == nil {
		return atom.NilValue
	}
	if len(indexes) == 2 {
		return atom.NewString(input[indexes[0]:indexes[1]])
	}
	groups := make([]types.Value, len(indexes)/2)
	for i := range groups {
		start, end := indexes[i*2], indexes[i*2+1]
		if start < 0 {
			groups[i] = atom.NilValue
		} else {
			groups[i] = atom.NewString(input[start:end])
		}
	}
	return coll.NewVector(groups...)
}
