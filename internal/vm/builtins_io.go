// builtins_io.go は I/O とシリアライズの組み込み関数を定義します。
// prn, pr-str, read-string, doall, dorun, slurp, spit をサポートします。
package vm

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"unicode/utf8"

	"doggo/internal/syntax/frontend"
	"doggo/internal/textutil"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsIO は I/O 関連の組み込み関数を定義します
var builtinsIO = []BuiltinDefinition{
	// prn - 機械可読形式で出力（文字列はクォート付き）
	{
		Name: "prn",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			parts := make([]string, len(args))
			for i, arg := range args {
				parts[i] = prStr(arg)
			}
			fmt.Println(strings.Join(parts, " "))
			return atom.NilValue
		}},
	},

	// pr-str - 機械可読形式の文字列を返す
	{
		Name: "pr-str",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			parts := make([]string, len(args))
			for i, arg := range args {
				parts[i] = prStr(arg)
			}
			return atom.NewString(strings.Join(parts, " "))
		}},
	},

	// read-string - 文字列をパースして Doggo のデータ構造に変換
	{
		Name: "read-string",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("read-string", args, 1); err != nil {
				return err
			}

			s, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("read-string: argument must be a string, got %s", args[0].Type())
			}

			node, err := frontend.ReadFirstForm("<read-string>", string(s))
			if err != nil {
				return newArgumentError("read-string: parse error: %v", err)
			}

			return node
		}},
	},

	// doall - 遅延シーケンスを強制的に実体化
	{
		Name: "doall",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("doall", args, 1); err != nil {
				return nil, err
			}

			// シーケンスをすべて走査してリストに変換
			items, err := materializeValue(args[0])
			if err != nil {
				return nil, err
			}
			if items == nil {
				return coll.NewList(), nil
			}
			return coll.NewList(items...), nil
		}},
	},

	// dorun - 遅延シーケンスを走査して副作用を実行（値は返さない）
	// メモリ効率: 要素を保持せず走査のみ行う（O(1) メモリ）
	{
		Name: "dorun",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("dorun", args, 1); err != nil {
				return nil, err
			}

			// シーケンスをすべて走査（副作用のため）- 要素は保持しない
			if err := drainValue(args[0]); err != nil {
				return nil, err
			}
			return atom.NilValue, nil
		}},
	},

	// slurp - ファイルを読み込んで文字列またはバイト列として返す
	// (slurp path) - UTF-8文字列として読み込み
	// (slurp path :binary true) - バイト列（Vector of numbers 0-255）として読み込み
	{
		Name: "slurp",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("slurp", args, 1, 2); err != nil {
				return err
			}

			// パスを取得
			pathStr, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("slurp: path must be a string, got %s", args[0].Type())
			}
			path := string(pathStr)

			// オプションを解析
			opts, optErr := parseIOOptions("slurp", args[1:], map[string]bool{"binary": true})
			if optErr != nil {
				return optErr
			}

			// ファイルを読み込み
			data, err := os.ReadFile(path)
			if err != nil {
				return RuntimeErrf("slurp", "%v", err)
			}

			// バイナリモードの場合はVectorを返す
			if opts.binary {
				return bytesToVector(data)
			}
			if !utf8.Valid(data) {
				return RuntimeErr("slurp", "text input is not valid UTF-8")
			}

			// 通常モードは文字列を返す
			return atom.NewString(string(data))
		}},
	},

	// spit - ファイルに書き込む
	// (spit path content) - 文字列を書き込み（非文字列は自動的に文字列化）
	// (spit path content :append true) - 追記モード
	// (spit path content {:append true}) - オプションマップ形式
	// (spit path bytes :binary true) - バイナリモード（Vector of numbers 0-255）
	{
		Name: "spit",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("spit", args, 2, 3); err != nil {
				return err
			}

			// パスを取得
			pathStr, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("spit: path must be a string, got %s", args[0].Type())
			}
			path := string(pathStr)

			// コンテンツを取得
			content := args[1]

			// オプションを解析
			opts, optErr := parseIOOptions("spit", args[2:], map[string]bool{"append": true, "binary": true})
			if optErr != nil {
				return optErr
			}

			var data []byte
			if opts.binary {
				// バイナリモード: Vectorをバイト列に変換
				vec, ok := content.(*coll.Vector)
				if !ok {
					return newArgumentError("spit: binary mode requires a vector, got %s", content.Type())
				}
				var err error
				data, err = vectorToBytes(vec)
				if err != nil {
					return RuntimeErrf("spit", "%v", err)
				}
			} else {
				// 通常モード: 値を文字列化
				data = []byte(valueToString(content))
			}

			// ファイルに書き込み
			if opts.append {
				// 追記モード
				f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					return RuntimeErrf("spit", "%v", err)
				}
				defer f.Close()
				if _, err := f.Write(data); err != nil {
					return RuntimeErrf("spit", "%v", err)
				}
			} else {
				// 上書きモード
				if err := os.WriteFile(path, data, 0644); err != nil {
					return RuntimeErrf("spit", "%v", err)
				}
			}

			return atom.NilValue
		}},
	},
}

// prStr は値を機械可読形式の文字列に変換します。
// 文字列はダブルクォート付きで返されます。
func prStr(v types.Value) string {
	if v == nil || v == atom.NilValue {
		return "nil"
	}

	switch val := v.(type) {
	case atom.String:
		return textutil.QuoteDoggoString(string(val))
	case atom.Keyword:
		return val.String()
	case *atom.Number:
		text := val.String()
		if val.IntVal == nil && val.FloatVal != nil &&
			!math.IsInf(*val.FloatVal, 0) && !math.IsNaN(*val.FloatVal) &&
			!strings.ContainsAny(text, ".eE") {
			return text + ".0"
		}
		return text
	case types.Number:
		return val.String()
	case *coll.Vector:
		if val == nil {
			return "[]"
		}
		return prStrItems(val.Items, "[", "]")
	case *coll.List:
		if val == nil {
			return "()"
		}
		return prStrItems(val.Items, "(", ")")
	case *coll.Set:
		if val == nil {
			return "#{}"
		}
		return prStrItems(val.Order, "#{", "}")
	case types.Map:
		keys := sequableToSlice(val.Keys())
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, prStr(key)+" "+prStr(val.Get(key)))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case atom.Boolean:
		if val {
			return "true"
		}
		return "false"
	case types.Symbol:
		return string(val)
	default:
		return val.String()
	}
}

func prStrItems(items []types.Value, open, close string) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = prStr(item)
	}
	return open + strings.Join(parts, " ") + close
}

// ioOptions は slurp/spit のオプションを保持します。
type ioOptions struct {
	append bool
	binary bool
}

// parseIOOptions parses an absent or single final option map.
func parseIOOptions(fnName string, args []types.Value, allowed map[string]bool) (ioOptions, types.Value) {
	opts := ioOptions{}
	if len(args) == 0 {
		return opts, nil
	}
	options, ok := args[0].(types.Map)
	if !ok {
		return opts, newArgumentError("%s: options must be a map, got %s", fnName, args[0].Type())
	}
	for _, key := range sequableToSlice(options.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok {
			return opts, newArgumentError("%s: option keys must be keywords, got %s", fnName, key.Type())
		}
		if !allowed[string(keyword)] {
			return opts, newArgumentError("%s: unsupported option %s", fnName, keyword.String())
		}
		value := options.Get(keyword)
		boolean, ok := value.(atom.Boolean)
		if !ok {
			return opts, newArgumentError("%s: option %s must be boolean, got %s", fnName, keyword.String(), value.Type())
		}
		switch string(keyword) {
		case "append":
			opts.append = bool(boolean)
		case "binary":
			opts.binary = bool(boolean)
		}
	}
	return opts, nil
}

// valueToString は値を文字列に変換します。
// 文字列はそのまま（クォートなし）、それ以外は String() を使用。
func valueToString(v types.Value) string {
	if v == nil || v == atom.NilValue {
		return "nil"
	}
	if s, ok := v.(atom.String); ok {
		// 文字列はクォートなしでそのまま返す
		return string(s)
	}
	// それ以外は String() で文字列化
	return v.String()
}

// bytesToVector はバイト列を Vector (各要素 0-255 の number) に変換します。
func bytesToVector(data []byte) *coll.Vector {
	items := make([]types.Value, len(data))
	for i, b := range data {
		items[i] = atom.NewNumberInt(int64(b))
	}
	return coll.NewVector(items...)
}

// vectorToBytes は Vector (各要素 0-255 の number) をバイト列に変換します。
// 範囲外の値がある場合はエラーを返します。
func vectorToBytes(vec *coll.Vector) ([]byte, error) {
	data := make([]byte, len(vec.Items))
	for i, item := range vec.Items {
		n, ok := integerForOperation(item)
		if !ok {
			return nil, fmt.Errorf("binary mode requires numbers, got %s at index %d", item.Type(), i)
		}
		if n < 0 || n > 255 {
			return nil, fmt.Errorf("byte value out of range (0-255): %d at index %d", n, i)
		}
		data[i] = byte(n)
	}
	return data, nil
}
