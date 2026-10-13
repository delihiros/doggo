package types

import (
	"context"
	"fmt"
)

// Reduced は早期終了のためのラッパー型
type Reduced struct {
	Val Value
}

func (r *Reduced) String() string { return fmt.Sprintf("#reduced[%s]", r.Val.String()) }
func (r *Reduced) Type() string   { return "reduced" }
func (r *Reduced) Hash() uint32   { return r.Val.Hash() }
func (r *Reduced) Equals(other Value) bool {
	otherReduced, ok := other.(*Reduced)
	return ok && r == otherReduced
}
func (r *Reduced) IsSelfEvaluating() bool { return false }

// IsReduced は値が Reduced でラップされているかチェック
func IsReduced(v Value) bool {
	_, ok := v.(*Reduced)
	return ok
}

// Unreduced は Reduced をアンラップ、そうでなければそのまま返す
func Unreduced(v Value) Value {
	if r, ok := v.(*Reduced); ok {
		return r.Val
	}
	return v
}

// ReducingFn は還元関数の型
// ctx: コンテキスト（VM呼び出し用）
// acc: アキュムレータ
// input: 入力値
// 戻り値: (結果, エラー)
type ReducingFn func(ctx context.Context, acc, input Value) (Value, error)

// TransducerFn はトランスデューサの変換関数型
type TransducerFn func(rf ReducingFn) ReducingFn

// Transducer はコレクション変換を表す型
// Clojure のトランスデューサと互換性のある実装
type Transducer struct {
	// XForm は変換関数
	XForm TransducerFn
	// Name はデバッグ用の名前
	Name string
}

// NewTransducer は新しい Transducer を作成
func NewTransducer(name string, xform TransducerFn) *Transducer {
	return &Transducer{
		XForm: xform,
		Name:  name,
	}
}

// String は Transducer の文字列表現を返す
func (t *Transducer) String() string {
	return fmt.Sprintf("#<transducer: %s>", t.Name)
}

// Type は型名を返す
func (t *Transducer) Type() string {
	return "transducer"
}

// Hash はハッシュ値を返す
func (t *Transducer) Hash() uint32 {
	// トランスデューサはアイデンティティベースのハッシュ
	return 0
}

// Equals は等価性を判定
func (t *Transducer) Equals(other Value) bool {
	return t == other
}

// IsSelfEvaluating は自己評価型かどうかを返す
func (t *Transducer) IsSelfEvaluating() bool {
	return true
}

// Apply は還元関数に変換を適用
func (t *Transducer) Apply(rf ReducingFn) ReducingFn {
	return t.XForm(rf)
}

// Compose は2つのトランスデューサを合成
// (comp (map f) (filter p)) のように左から右への適用順
func (t *Transducer) Compose(other *Transducer) *Transducer {
	return &Transducer{
		XForm: func(rf ReducingFn) ReducingFn {
			// other を先に適用し、その結果に t を適用
			return t.XForm(other.XForm(rf))
		},
		Name: fmt.Sprintf("comp(%s, %s)", t.Name, other.Name),
	}
}

// ComposeTransducers は複数のトランスデューサを合成
func ComposeTransducers(xforms ...*Transducer) *Transducer {
	if len(xforms) == 0 {
		// identity transducer
		return NewTransducer("identity", func(rf ReducingFn) ReducingFn {
			return rf
		})
	}
	if len(xforms) == 1 {
		return xforms[0]
	}

	result := xforms[0]
	for i := 1; i < len(xforms); i++ {
		result = result.Compose(xforms[i])
	}
	return result
}
