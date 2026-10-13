// ops.go はコレクション操作のヘルパー関数を提供します。
// VM の builtins から呼び出されることを想定しています。
package coll

import (
	"doggo/internal/types"
)

// ----------------------------------------------------------------------------
// Count 操作
// ----------------------------------------------------------------------------

// CountResult は Count 操作の結果を表します。
type CountResult struct {
	Count int64
	Ok    bool
	Err   error
}

// Count は値の要素数を返します。
// 対応する型: String, Vector, List, Map, Set, Seq
// サポートされていない型の場合、Ok は false になります。
func Count(v types.Value) CountResult {
	if v == nil {
		return CountResult{Count: 0, Ok: true}
	}

	switch val := v.(type) {
	case types.Countable:
		count := val.Count()
		if count < 0 {
			return CountResult{Ok: false}
		}
		return CountResult{Count: int64(count), Ok: true}
	}

	seq, ok := SequentialSeq(v)
	if !ok {
		return CountResult{Count: 0, Ok: false}
	}
	// 注意: 無限シーケンスに対しては終了しない
	cnt := int64(0)
	for seq != nil {
		if err := RealizeSequenceNode(seq); err != nil {
			return CountResult{Err: err}
		}
		if seq.First() == nil {
			break
		}
		cnt++
		seq = seq.Next()
	}
	return CountResult{Count: cnt, Ok: true}
}

// ----------------------------------------------------------------------------
// First 操作
// ----------------------------------------------------------------------------

// First は値の最初の要素を返します。
// 空の場合または対応しない型の場合は nil を返します。
func First(v types.Value) types.Value {
	if v == nil {
		return nil
	}

	if seq, ok := SequentialSeq(v); ok {
		if seq == nil {
			return nil
		}
		return seq.First()
	}

	if sequable, ok := v.(types.Sequable); ok {
		if seq, ok := sequable.Seq().(types.Seq); ok {
			return unwrapSeqForCoercion(seq).First()
		}
	}
	return nil
}

// FirstSupported は First 操作がサポートされているかを返します。
func FirstSupported(v types.Value) bool {
	if v == nil {
		return false
	}
	if _, ok := SequentialSeq(v); ok {
		return true
	}
	if _, ok := v.(types.Sequable); ok {
		return true
	}
	return false
}

// ----------------------------------------------------------------------------
// Rest 操作
// ----------------------------------------------------------------------------

// Rest は最初の要素を除いた残りのシーケンスを返します。
// 空の場合は空のシーケンス（List）を返します。
// 対応しない型の場合は nil を返します。
func Rest(v types.Value) types.Value {
	if v == nil {
		return nil
	}

	if seq, ok := SequentialSeq(v); ok {
		if seq == nil {
			return NewList()
		}
		result := seq.Next()
		if result == nil {
			return NewList()
		}
		return result
	}

	if sequable, ok := v.(types.Sequable); ok {
		seqVal := sequable.Seq()
		seq, ok := seqVal.(types.Seq)
		if !ok || seq == nil {
			return NewList()
		}
		rest := unwrapSeqForCoercion(seq).Next()
		if rest == nil {
			return NewList()
		}
		return rest
	}
	return nil
}

// RestSupported は Rest 操作がサポートされているかを返します。
func RestSupported(v types.Value) bool {
	if v == nil {
		return false
	}
	if _, ok := SequentialSeq(v); ok {
		return true
	}
	if _, ok := v.(types.Sequable); ok {
		return true
	}
	return false
}

// ----------------------------------------------------------------------------
// ToSeq 操作
// ----------------------------------------------------------------------------

// ToSeqResult は ToSeq 操作の結果を表します。
type ToSeqResult struct {
	Seq types.Value // nil または types.Seq を実装する値
	Ok  bool
}

func unwrapSeqForCoercion(seq types.Seq) types.Seq {
	if metaSeq, ok := seq.(*MetaSeq); ok {
		return metaSeq.SeqValue
	}
	return seq
}

// ToSeq は値をシーケンスに変換します。
// 空のコレクションの場合は nil を返します（Clojure 互換）。
// 文字列の場合、runes 引数のスライスへの変換結果を返します。
// 対応しない型の場合、Ok は false になります。
func ToSeq(v types.Value) ToSeqResult {
	if v == nil {
		return ToSeqResult{Seq: nil, Ok: true}
	}

	if seq, ok := SequentialSeq(v); ok {
		seq = unwrapSeqForCoercion(seq)
		if seq == nil {
			return ToSeqResult{Seq: nil, Ok: true}
		}
		if seq.First() == nil {
			return ToSeqResult{Seq: nil, Ok: true}
		}
		return ToSeqResult{Seq: seq, Ok: true}
	}

	// types.Sequable を実装している場合（Vector, Map 等）
	if sequable, ok := v.(types.Sequable); ok {
		result := sequable.Seq()
		if result == nil {
			return ToSeqResult{Seq: nil, Ok: true}
		}
		if seq, ok := result.(types.Seq); ok {
			seq = unwrapSeqForCoercion(seq)
			if seq == nil || seq.First() == nil {
				return ToSeqResult{Seq: nil, Ok: true}
			}
			return ToSeqResult{Seq: seq, Ok: true}
		}
		return ToSeqResult{Seq: result, Ok: true}
	}

	return ToSeqResult{Seq: nil, Ok: false}
}

// ----------------------------------------------------------------------------
// AsSeq 操作（VM の toSeq 互換）
// ----------------------------------------------------------------------------

// AsSeq はコレクションをシーケンスに変換します。
// 空のコレクションの場合は nil を返します。
// これは VM の高階関数（map, filter, reduce 等）で使用されます。
func AsSeq(v types.Value) types.Seq {
	if v == nil {
		return nil
	}

	if seq, ok := SequentialSeq(v); ok {
		seq = unwrapSeqForCoercion(seq)
		if seq == nil || seq.First() == nil {
			return nil
		}
		return seq
	}

	switch c := v.(type) {
	case *Set:
		seq := c.Seq()
		if seq == nil {
			return nil
		}
		if s, ok := seq.(types.Seq); ok {
			return unwrapSeqForCoercion(s)
		}
		return nil
	case types.Sequable:
		seqVal := c.Seq()
		if seqVal == nil {
			return nil
		}
		if seq, ok := seqVal.(types.Seq); ok {
			return unwrapSeqForCoercion(seq)
		}
		return nil
	default:
		return nil
	}
}

// ----------------------------------------------------------------------------
// ToVector 操作
// ----------------------------------------------------------------------------

// ToVector はシーケンスをベクターに変換します。
func ToVector(seq types.Seq) *Vector {
	if seq == nil {
		return NewVector()
	}

	var items []types.Value
	for seq != nil {
		first := seq.First()
		if first == nil {
			break
		}
		items = append(items, first)
		seq = seq.Next()
	}
	return NewVector(items...)
}
