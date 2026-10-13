// builtins_core.go はコア組み込み関数を定義します。
//
// 提供する関数:
//   - コレクション基本操作: count, first, rest, next, seq, conj, cons, peek, pop
//   - 型変換: vec, list, set, hash-map, keyword, symbol, name
//   - 述語: nil?, some?, empty?, sequential?, coll?, counted?
//   - イテレーション: doall, dorun
//   - range: range（遅延シーケンス生成）
package vm

import (
	"context"
	"fmt"
	"time"

	"doggo/internal/textutil"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var nanoTimeOrigin = time.Now()

// builtinsCoreは基本的な組み込み関数を定義します
var builtinsCore = []BuiltinDefinition{
	// count - コレクションの要素数を返す（Clojure 互換）
	{
		Name: "count",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("count", args, 1); err != nil {
				return err
			}
			// nil の場合は 0 を返す
			if args[0] == nil {
				return atom.NewNumberInt(0)
			}
			if _, ok := args[0].(types.Nil); ok {
				return atom.NewNumberInt(0)
			}
			// Strings are sequences of Unicode code points.
			if str, ok := args[0].(atom.String); ok {
				return atom.NewNumberInt(int64(textutil.CodePointLen(string(str))))
			}
			// coll.Count ヘルパーを使用
			result := coll.Count(args[0])
			if result.Err != nil {
				return RuntimeErrf("count", "%v", result.Err)
			}
			if !result.Ok {
				return newArgumentError("argument to `count` not supported, got %s", args[0].Type())
			}
			return atom.NewNumberInt(result.Count)
		}},
	},
	{
		Name: "first",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("first", args, 1); err != nil {
				return err
			}
			if str, ok := args[0].(atom.String); ok {
				items := []rune(string(str))
				if len(items) == 0 {
					return atom.NilValue
				}
				return atom.NewCharacter(items[0])
			}
			// coll.First ヘルパーを使用
			if !coll.FirstSupported(args[0]) {
				return newArgumentError("argument to `first` must be a sequence, got %s", args[0].Type())
			}
			result := coll.First(args[0])
			if seq, ok := args[0].(types.Seq); ok {
				if err := sequenceEvalError(seq); err != nil {
					return RuntimeErrf("first", "%v", err)
				}
			}
			if result == nil {
				return atom.NilValue
			}
			return result
		}},
	},
	// seq - コレクションをシーケンスに変換（空の場合は nil を返す）
	// Clojure 互換: (seq coll) -> nil (空) or seq (非空)
	{
		Name: "seq",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("seq", args, 1); err != nil {
				return err
			}
			if args[0] == nil {
				return atom.NilValue
			}
			// nil 値の場合
			if _, ok := args[0].(types.Nil); ok {
				return atom.NilValue
			}
			if seq, ok := args[0].(types.Seq); ok {
				if err := sequenceEvalError(seq); err != nil {
					return RuntimeErrf("seq", "%v", err)
				}
			}
			// String sequences contain Character values.
			if str, ok := args[0].(atom.String); ok {
				items := []rune(string(str))
				if len(items) == 0 {
					return atom.NilValue
				}
				values := make([]types.Value, len(items))
				for i, item := range items {
					values[i] = atom.NewCharacter(item)
				}
				return coll.NewList(values...)
			}
			// coll.ToSeq ヘルパーを使用
			result := coll.ToSeq(args[0])
			if !result.Ok {
				return newArgumentError("don't know how to create seq from: %s", args[0].Type())
			}
			if result.Seq == nil {
				return atom.NilValue
			}
			return result.Seq
		}},
	},
	{
		Name: "rest",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("rest", args, 1); err != nil {
				return err
			}
			if str, ok := args[0].(atom.String); ok {
				return stringRestSeq(str, false)
			}
			if seq, ok := args[0].(types.Seq); ok {
				if err := sequenceEvalError(seq); err != nil {
					return RuntimeErrf("rest", "%v", err)
				}
			}
			// coll.Rest ヘルパーを使用
			if !coll.RestSupported(args[0]) {
				return newArgumentError("argument to `rest` must be a sequence, got %s", args[0].Type())
			}
			return coll.Rest(args[0])
		}},
	},
	// next - Like rest but returns nil for empty sequences
	// (next [1 2 3]) => (2 3)
	// (next [1]) => nil
	// (next []) => nil
	{
		Name: "next",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("next", args, 1); err != nil {
				return err
			}
			if str, ok := args[0].(atom.String); ok {
				return stringRestSeq(str, true)
			}
			if input, ok := args[0].(types.Seq); ok {
				if err := sequenceEvalError(input); err != nil {
					return RuntimeErrf("next", "%v", err)
				}
			}
			seq := coll.AsSeq(args[0])
			if seq == nil {
				return types.Nil{}
			}
			rest := seq.Next()
			if rest == nil {
				return types.Nil{}
			}
			return rest
		}},
	},
	{
		Name: "cons",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("cons", args, 2); err != nil {
				return err
			}
			switch seq := args[1].(type) {
			case *coll.List:
				newElements := make([]types.Value, len(seq.Items)+1)
				newElements[0] = args[0]
				copy(newElements[1:], seq.Items)
				return coll.NewList(newElements...)
			case *coll.Vector:
				newElements := make([]types.Value, len(seq.Items)+1)
				newElements[0] = args[0]
				copy(newElements[1:], seq.Items)
				return coll.NewList(newElements...)
			case types.Nil:
				return coll.NewList(args[0])
			case types.Seq:
				return coll.NewConsSeq(args[0], seq)
			default:
				return newArgumentError("second argument to `cons` must be a sequence, got %s", args[1].Type())
			}
		}},
	},
	{
		Name: "conj",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("conj", args, 2); err != nil {
				return err
			}
			result, err := conjValues(args[0], args[1:]...)
			if err != nil {
				return newArgumentError("%v", err)
			}
			return result
		}},
	},
	{
		Name: "disj",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("disj", args, 2); err != nil {
				return err
			}
			set, ok := args[0].(*coll.Set)
			if !ok {
				return newArgumentError("disj: first argument must be a set, got %s", args[0].Type())
			}
			return set.Disj(args[1])
		}},
	},
	{
		Name: "print",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			for _, arg := range args {
				fmt.Print(arg.String())
			}
			return atom.NilValue
		}},
	},
	{
		Name: "println",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			for i, arg := range args {
				if i > 0 {
					fmt.Print(" ")
				}
				fmt.Print(arg.String())
			}
			fmt.Println()
			return atom.NilValue
		}},
	},
	{
		Name: "list",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			return coll.NewList(args...)
		}},
	},
	{
		Name: "vec",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if len(args) == 1 {
				switch c := args[0].(type) {
				case *coll.Vector:
					return c
				case *coll.List:
					return coll.NewVector(c.Items...)
				case *coll.Set:
					return coll.NewVector(c.AllElements()...)
				case types.Seq:
					items, err := materializeSeq(c)
					if err != nil {
						return RuntimeErrf("vec", "%v", err)
					}
					return coll.NewVector(items...)
				case types.Sequable:
					items, err := materializeValue(args[0])
					if err != nil {
						return RuntimeErrf("vec", "%v", err)
					}
					if items == nil {
						return newArgumentError("vec: cannot convert %s to vector", args[0].Type())
					}
					return coll.NewVector(items...)
				default:
					return newArgumentError("vec: cannot convert %s to vector", args[0].Type())
				}
			}
			return coll.NewVector(args...)
		}},
	},
	{
		Name: "set",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if len(args) == 1 {
				switch c := args[0].(type) {
				case *coll.Set:
					return c
				case *coll.Vector:
					return coll.NewSet(c.Items...)
				case *coll.List:
					return coll.NewSet(c.Items...)
				case types.Seq:
					items, err := materializeSeq(c)
					if err != nil {
						return RuntimeErrf("set", "%v", err)
					}
					return coll.NewSet(items...)
				case types.Sequable:
					items, err := materializeValue(args[0])
					if err != nil {
						return RuntimeErrf("set", "%v", err)
					}
					if items == nil {
						return newArgumentError("set: cannot convert %s to set", args[0].Type())
					}
					return coll.NewSet(items...)
				default:
					return newArgumentError("set: cannot convert %s to set", args[0].Type())
				}
			}
			return coll.NewSet(args...)
		}},
	},
	// 型判定関数
	{
		Name: "list?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("list?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*coll.List)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "seq?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("seq?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(types.Seq)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "vector?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("vector?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*coll.Vector)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "set?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("set?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*coll.Set)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "string?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("string?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(atom.String)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "char?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("char?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(atom.Character)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "number?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("number?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(types.Number)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "map?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("map?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*coll.Map)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "symbol?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("symbol?", args, 1); err != nil {
				return err
			}
			return atom.Boolean(types.IsSymbol(args[0]))
		}},
	},
	{
		Name: "keyword?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("keyword?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(atom.Keyword)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "fn?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("fn?", args, 1); err != nil {
				return err
			}
			// 関数型: function (stdlib), builtin (VM組み込み), closure (VM lambda), compiled-function
			t := args[0].Type()
			return atom.Boolean(t == "function" || t == "builtin" || t == "closure" || t == "compiled-function")
		}},
	},
	{
		Name: "coll?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("coll?", args, 1); err != nil {
				return err
			}
			// list, vector, map, set がコレクション
			switch args[0].(type) {
			case *coll.List, *coll.Vector, *coll.Map, *coll.Set:
				return atom.True
			default:
				return atom.False
			}
		}},
	},
	// ifn? - 呼び出し可能か判定（IFn インターフェース実装）
	// Clojure 互換: fn? とは異なり、Keyword, Map, Vector, Set も true を返す
	{
		Name: "ifn?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("ifn?", args, 1); err != nil {
				return err
			}
			// types.Fn インターフェースを実装しているか
			if _, ok := args[0].(types.Fn); ok {
				return atom.True
			}
			// Closure と CompiledFunction は types.Fn を実装していないが呼び出し可能
			t := args[0].Type()
			if t == "closure" || t == "compiled-function" {
				return atom.True
			}
			return atom.False
		}},
	},
	// sequential? - 順序を保持するコレクションか判定
	{
		Name: "sequential?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("sequential?", args, 1); err != nil {
				return err
			}
			_, ok := coll.SequentialSeq(args[0])
			return atom.Boolean(ok)
		}},
	},
	// associative? - キー・値の関連付けをサポートするか判定
	// Clojure 互換: Map, Vector（インデックスがキー）, Record が true
	{
		Name: "associative?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("associative?", args, 1); err != nil {
				return err
			}
			switch args[0].(type) {
			case *coll.Map, *coll.ArrayMap, *coll.SortedMap, *coll.StructMap,
				*coll.Vector, *coll.Record:
				return atom.True
			default:
				return atom.False
			}
		}},
	},
	// counted? - O(1) で count 可能か判定
	// Clojure 互換: Countable インターフェース実装
	{
		Name: "counted?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("counted?", args, 1); err != nil {
				return err
			}
			if _, ok := args[0].(types.Countable); ok {
				return atom.True
			}
			return atom.False
		}},
	},
	// nil? - nil かどうか判定
	{
		Name: "nil?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("nil?", args, 1); err != nil {
				return err
			}
			if args[0] == nil {
				return atom.True
			}
			if _, ok := args[0].(types.Nil); ok {
				return atom.True
			}
			if seq, ok := args[0].(types.Seq); ok {
				if err := sequenceEvalError(seq); err != nil {
					return RuntimeErrf("nil?", "%v", err)
				}
			}
			return atom.False
		}},
	},
	// some? - nil でないかどうか判定 (nil? の逆)
	{
		Name: "some?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("some?", args, 1); err != nil {
				return err
			}
			if args[0] == nil {
				return atom.False
			}
			if _, ok := args[0].(types.Nil); ok {
				return atom.False
			}
			return atom.True
		}},
	},
	// true? - 値が正確に true かどうか判定
	{
		Name: "true?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("true?", args, 1); err != nil {
				return err
			}
			return atom.Boolean(args[0] == atom.True)
		}},
	},
	// false? - 値が正確に false かどうか判定
	{
		Name: "false?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("false?", args, 1); err != nil {
				return err
			}
			return atom.Boolean(args[0] == atom.False)
		}},
	},
	// boolean? - 値がブール型かどうか判定
	{
		Name: "boolean?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("boolean?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(atom.Boolean)
			return atom.Boolean(ok)
		}},
	},
	// pos? - 正の数かどうか判定
	{
		Name: "pos?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("pos?", args, 1); err != nil {
				return err
			}
			num, ok := args[0].(types.Number)
			if !ok {
				return newArgumentError("pos?: argument must be a number, got %s", args[0].Type())
			}
			if num.ToFloat64() > 0 {
				return atom.True
			}
			return atom.False
		}},
	},
	// int? - 数値が整数かどうか判定
	{
		Name: "int?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("int?", args, 1); err != nil {
				return err
			}
			num, ok := args[0].(types.Number)
			if !ok {
				return atom.False
			}
			_, isInt := types.Int64Value(num)
			return atom.Boolean(isInt)
		}},
	},
	// float? - 数値が浮動小数点かどうか判定
	{
		Name: "float?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("float?", args, 1); err != nil {
				return err
			}
			num, ok := args[0].(types.Number)
			if !ok {
				return atom.False
			}
			_, isInt := types.Int64Value(num)
			return atom.Boolean(!isInt)
		}},
	},
	// nat-int? - 非負整数かどうか判定 (>= 0)
	{
		Name: "nat-int?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("nat-int?", args, 1); err != nil {
				return err
			}
			num, ok := args[0].(types.Number)
			if !ok {
				return atom.False
			}
			integer, isInt := types.Int64Value(num)
			if !isInt {
				return atom.False
			}
			return atom.Boolean(integer >= 0)
		}},
	},
	// pos-int? - 正の整数かどうか判定 (> 0)
	{
		Name: "pos-int?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("pos-int?", args, 1); err != nil {
				return err
			}
			num, ok := args[0].(types.Number)
			if !ok {
				return atom.False
			}
			integer, isInt := types.Int64Value(num)
			if !isInt {
				return atom.False
			}
			return atom.Boolean(integer > 0)
		}},
	},
	// neg-int? - 負の整数かどうか判定 (< 0)
	{
		Name: "neg-int?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("neg-int?", args, 1); err != nil {
				return err
			}
			num, ok := args[0].(types.Number)
			if !ok {
				return atom.False
			}
			integer, isInt := types.Int64Value(num)
			if !isInt {
				return atom.False
			}
			return atom.Boolean(integer < 0)
		}},
	},
	// empty? - コレクションが空かどうか判定
	{
		Name: "empty?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("empty?", args, 1); err != nil {
				return err
			}
			if args[0] == nil {
				return atom.True
			}
			if _, ok := args[0].(types.Nil); ok {
				return atom.True
			}
			// 文字列の場合
			if s, ok := args[0].(atom.String); ok {
				return atom.Boolean(textutil.CodePointLen(string(s)) == 0)
			}
			// Countable インターフェースを実装している場合
			if countable, ok := args[0].(types.Countable); ok {
				return atom.Boolean(countable.Count() == 0)
			}
			// Sequable インターフェースを実装している場合
			if sequable, ok := args[0].(types.Sequable); ok {
				seqValue := sequable.Seq()
				if lazyErr, ok := seqValue.(*coll.LazySeqError); ok {
					return RuntimeErrf("empty?", "%v", lazyErr.Cause())
				}
				return atom.Boolean(seqValue == nil)
			}
			if seq, ok := coll.SequentialSeq(args[0]); ok {
				if seq == nil {
					return atom.True
				}
				return atom.Boolean(seq.First() == nil)
			}
			return atom.False
		}},
	},
	// seqable? - シーケンスに変換可能かどうか判定
	{
		Name: "seqable?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("seqable?", args, 1); err != nil {
				return err
			}
			// nil is seqable (seq returns nil)
			if args[0] == nil {
				return atom.True
			}
			if _, ok := args[0].(types.Nil); ok {
				return atom.True
			}
			// Sequable interface
			if _, ok := args[0].(types.Sequable); ok {
				return atom.True
			}
			// String is seqable
			if _, ok := args[0].(atom.String); ok {
				return atom.True
			}
			if _, ok := coll.SequentialSeq(args[0]); ok {
				return atom.True
			}
			return atom.False
		}},
	},
	// realized? - 遅延計算が完了済みかどうか判定
	{
		Name: "realized?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("realized?", args, 1); err != nil {
				return err
			}
			if pending, ok := args[0].(types.Pending); ok {
				return atom.Boolean(pending.Realized())
			}
			// Eager values have no outstanding computation.
			return atom.True
		}},
	},
	// 時間関数
	{
		Name: "nano-time",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("nano-time", args, 0); err != nil {
				return err
			}
			return atom.NewNumberInt(time.Since(nanoTimeOrigin).Nanoseconds())
		}},
	},
	// apply - 関数にコレクションの要素を展開して渡す
	// (apply f coll) or (apply f arg1 arg2 ... coll)
	{
		Name: "apply",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("apply", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("apply: VM not available in context")
			}

			fn := args[0]
			// 最後の引数はコレクション
			lastArg := args[len(args)-1]

			// コレクションを引数リストに変換
			seq, err := asSeq(lastArg, "apply")
			if err != nil {
				return nil, err
			}
			collArgs, err := materializeSeq(seq)
			if err != nil {
				return nil, err
			}

			// 中間引数とコレクションの要素を結合
			var allArgs []types.Value
			if len(args) > 2 {
				// (apply f arg1 arg2 ... coll) の場合
				allArgs = append(allArgs, args[1:len(args)-1]...)
			}
			allArgs = append(allArgs, collArgs...)

			return ApplyCallableFast(ctx, vm, fn, allArgs)
		}},
	},
	// range - 遅延シーケンスを生成
	// (range) -> 無限シーケンス (0 1 2 ...)
	// (range end) -> (0 1 ... end-1)
	// (range start end) -> (start start+1 ... end-1)
	// (range start end step) -> (start start+step ... )
	{
		Name: "range",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("range", args, 0, 3); err != nil {
				return err
			}
			rangeInt := func(arg types.Value, name string) (int64, *Error) {
				number, ok := arg.(types.Number)
				if !ok {
					return 0, newArgumentError("range: %s must be a number, got %s", name, arg.Type())
				}
				integer, ok := types.TruncateInt64Value(number)
				if !ok {
					return 0, newArgumentError("range: %s must be a finite number in int64 range", name)
				}
				return integer, nil
			}

			// 0引数の場合は無限シーケンスを返す
			if len(args) == 0 {
				return coll.NewInfiniteRangeLazySeq()
			}

			var start, end, step int64 = 0, 0, 1

			switch len(args) {
			case 1:
				// (range end)
				var err *Error
				end, err = rangeInt(args[0], "end")
				if err != nil {
					return err
				}
			case 2:
				// (range start end)
				var err *Error
				start, err = rangeInt(args[0], "start")
				if err != nil {
					return err
				}
				end, err = rangeInt(args[1], "end")
				if err != nil {
					return err
				}
			case 3:
				// (range start end step)
				var err *Error
				start, err = rangeInt(args[0], "start")
				if err != nil {
					return err
				}
				end, err = rangeInt(args[1], "end")
				if err != nil {
					return err
				}
				step, err = rangeInt(args[2], "step")
				if err != nil {
					return err
				}
				if step == 0 {
					return newArgumentError("range: step cannot be zero")
				}
			}

			return coll.NewRangeLazySeq(start, end, step)
		}},
	},
}

func stringRestSeq(str atom.String, nilOnEmpty bool) types.Value {
	items := []rune(string(str))
	if len(items) <= 1 {
		if nilOnEmpty {
			return types.Nil{}
		}
		return coll.NewList()
	}
	values := make([]types.Value, len(items)-1)
	for i, item := range items[1:] {
		values[i] = atom.NewCharacter(item)
	}
	return coll.NewList(values...)
}
