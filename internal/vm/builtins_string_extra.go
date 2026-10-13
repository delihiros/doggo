// builtins_string_extra.go は追加の文字列操作の組み込み関数を定義します。
// format, replace, blank?, starts-with?, ends-with?, includes?, index-of, last-index-of をサポートします。
package vm

import (
	"fmt"
	"strings"
	"unicode"

	"doggo/internal/textutil"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsStringExtra は追加の文字列操作の組み込み関数を定義します
var builtinsStringExtra = []BuiltinDefinition{
	// format - printf スタイルのフォーマット
	{
		Name: "format",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("format", args, 1); err != nil {
				return err
			}

			formatStr, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("format: first argument must be a string, got %s", args[0].Type())
			}

			// 残りの引数をGoの値に変換
			goArgs := make([]interface{}, len(args)-1)
			for i, arg := range args[1:] {
				goArgs[i] = toGoValue(arg)
			}

			result := fmt.Sprintf(string(formatStr), goArgs...)
			return atom.NewString(result)
		}},
	},

	// replace - 文字列置換（全ての出現箇所）
	{
		Name: "replace",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("replace", args, 3); err != nil {
				return err
			}

			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("replace: first argument must be a string, got %s", args[0].Type())
			}

			match, ok := args[1].(atom.String)
			if !ok {
				return newArgumentError("replace: second argument must be a string, got %s", args[1].Type())
			}

			replacement, ok := args[2].(atom.String)
			if !ok {
				return newArgumentError("replace: third argument must be a string, got %s", args[2].Type())
			}

			result := strings.ReplaceAll(string(s), string(match), string(replacement))
			return atom.NewString(result)
		}},
	},
	{
		Name: "replace-first",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("replace-first", args, 3); err != nil {
				return err
			}

			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("replace-first: first argument must be a string, got %s", args[0].Type())
			}

			match, ok := args[1].(atom.String)
			if !ok {
				return newArgumentError("replace-first: second argument must be a string, got %s", args[1].Type())
			}

			replacement, ok := args[2].(atom.String)
			if !ok {
				return newArgumentError("replace-first: third argument must be a string, got %s", args[2].Type())
			}

			return atom.NewString(strings.Replace(string(s), string(match), string(replacement), 1))
		}},
	},
	{
		Name: "split-lines",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("split-lines", args, 1); err != nil {
				return err
			}

			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("split-lines: argument must be a string, got %s", args[0].Type())
			}

			lines := splitLines(string(s))
			items := make([]types.Value, len(lines))
			for i, line := range lines {
				items[i] = atom.NewString(line)
			}
			return coll.NewVector(items...)
		}},
	},
	{
		Name: "triml",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("triml", args, 1); err != nil {
				return err
			}

			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("triml: argument must be a string, got %s", args[0].Type())
			}

			return atom.NewString(strings.TrimLeftFunc(string(s), unicode.IsSpace))
		}},
	},
	{
		Name: "trimr",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("trimr", args, 1); err != nil {
				return err
			}

			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("trimr: argument must be a string, got %s", args[0].Type())
			}

			return atom.NewString(strings.TrimRightFunc(string(s), unicode.IsSpace))
		}},
	},
	{
		Name: "capitalize",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("capitalize", args, 1); err != nil {
				return err
			}

			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("capitalize: argument must be a string, got %s", args[0].Type())
			}

			runes := []rune(string(s))
			if len(runes) == 0 {
				return atom.NewString("")
			}
			return atom.NewString(strings.ToUpper(string(runes[:1])) + strings.ToLower(string(runes[1:])))
		}},
	},
	{
		Name: "escape",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("escape", args, 2); err != nil {
				return err
			}

			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("escape: first argument must be a string, got %s", args[0].Type())
			}

			cmap, ok := args[1].(types.Map)
			if !ok {
				return newArgumentError("escape: second argument must be a map, got %s", args[1].Type())
			}

			var parts []string
			for _, r := range []rune(string(s)) {
				piece := string(r)
				replaced := false
				for _, key := range []types.Value{
					atom.NewString(piece),
					atom.NewCharacter(r),
				} {
					if key == nil {
						continue
					}
					if repl := cmap.Get(key); repl != nil {
						switch v := repl.(type) {
						case atom.String:
							parts = append(parts, string(v))
						case types.Displayable:
							parts = append(parts, v.Display())
						default:
							parts = append(parts, v.String())
						}
						replaced = true
						break
					}
				}
				if !replaced {
					parts = append(parts, piece)
				}
			}

			return atom.NewString(strings.Join(parts, ""))
		}},
	},

	// blank? - 文字列が空または空白のみか判定
	{
		Name: "blank?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("blank?", args, 1); err != nil {
				return err
			}

			// nil は blank と見なす
			if args[0] == nil || args[0] == atom.NilValue {
				return atom.True
			}

			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("blank?: argument must be a string or nil, got %s", args[0].Type())
			}

			return atom.Boolean(strings.TrimSpace(string(s)) == "")
		}},
	},

	// starts-with? - 文字列が指定のプレフィックスで始まるか
	{
		Name:    "starts-with?",
		Builtin: stringPredicateBuiltin("starts-with?", strings.HasPrefix),
	},

	// ends-with? - 文字列が指定のサフィックスで終わるか
	{
		Name:    "ends-with?",
		Builtin: stringPredicateBuiltin("ends-with?", strings.HasSuffix),
	},

	// includes? - 文字列が部分文字列を含むか
	{
		Name:    "includes?",
		Builtin: stringPredicateBuiltin("includes?", strings.Contains),
	},

	// index-of - 部分文字列の最初の出現位置を返す（見つからない場合は -1）
	{
		Name:    "index-of",
		Builtin: stringIndexBuiltin("index-of", textutil.CodePointIndex),
	},

	// last-index-of - 部分文字列の最後の出現位置を返す（見つからない場合は -1）
	{
		Name:    "last-index-of",
		Builtin: stringIndexBuiltin("last-index-of", textutil.CodePointLastIndex),
	},
}

func stringPredicateBuiltin(fnName string, predicate func(string, string) bool) *Builtin {
	return &Builtin{Fn: func(args ...types.Value) types.Value {
		left, right, errValue := requireTwoStrings(fnName, args)
		if errValue != nil {
			return errValue
		}
		return atom.Boolean(predicate(left, right))
	}}
}

func stringIndexBuiltin(fnName string, index func(string, string) int) *Builtin {
	return &Builtin{Fn: func(args ...types.Value) types.Value {
		left, right, errValue := requireTwoStrings(fnName, args)
		if errValue != nil {
			return errValue
		}
		return atom.NewNumberInt(int64(index(left, right)))
	}}
}

func requireTwoStrings(fnName string, args []types.Value) (string, string, types.Value) {
	if err := CheckArgCount(fnName, args, 2); err != nil {
		return "", "", err
	}

	left, ok := args[0].(atom.String)
	if !ok {
		return "", "", newArgumentError("%s: first argument must be a string, got %s", fnName, args[0].Type())
	}

	right, ok := args[1].(atom.String)
	if !ok {
		return "", "", newArgumentError("%s: second argument must be a string, got %s", fnName, args[1].Type())
	}

	return string(left), string(right), nil
}

// toGoValue は Doggo の値を Go の値に変換します（format 用）
func toGoValue(v types.Value) interface{} {
	if v == nil || v == atom.NilValue {
		return nil
	}

	switch val := v.(type) {
	case atom.String:
		return string(val)
	case types.Number:
		if integer, ok := types.Int64Value(val); ok {
			return integer
		}
		return val.ToFloat64()
	case atom.Boolean:
		return bool(val)
	case atom.Keyword:
		return string(val)
	case types.Symbol:
		return string(val)
	default:
		return val.String()
	}
}

func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}

	lines := make([]string, 0, 8)
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			lines = append(lines, s[start:i])
			start = i + 1
		case '\r':
			lines = append(lines, s[start:i])
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}

	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
