package types

import (
	"context"
)

// Value はすべてのLisp値の基底型です。
type Value interface {
	String() string          // 文字列表現を返す
	Type() string            // 型名を返す
	Hash() uint32            // ハッシュ値を返す
	Equals(other Value) bool // 等価性を判定する
	IsSelfEvaluating() bool  // 自己評価型かどうかを返す
}

// Displayable は人間が読みやすい形式での出力をサポートするインターフェースです。
type Displayable interface {
	Display() string // 表示用の文字列表現を返す
}

// Number は数値型を表すインターフェースです。
type Number interface {
	Value
	ToFloat64() float64
	IsZero() bool
}

// Int64Valuer は値を正確な int64 として取り出せる型を表します。
// 戻り値の bool は、その値が正確な int64 として表現可能かを示します。
type Int64Valuer interface {
	Int64Value() (int64, bool)
}

// Environment は変数と関数のスコープ管理を行うインターフェースです。
type Environment interface {
	Get(key Value) (Value, bool)      // シンボルの値を取得
	Set(key Value, value Value) error // シンボルに値を設定
	Child() Environment               // 子スコープを作成
	Root() Environment                // ルート環境を取得
}

// Countable は要素数を取得できるコレクションを表します。
type Countable interface {
	Count() int
}

// Conjable は要素を追加できるコレクションを表します。
type Conjable interface {
	Conj(item Value) Value
}

// Sequable はシーケンスに変換できるコレクションを表します。
type Sequable interface {
	Seq() Value // シーケンスを返す（空の場合はnil）
}

// List は永続的なリストを表します。
type List interface {
	Value
	Countable
	Conjable
	Sequable
	Peek() Value // 先頭要素を取得（スタックとして使用）
	Pop() List   // 先頭要素を除いたリストを返す
}

// Vector は永続的なベクターを表します。
type Vector interface {
	Value
	Countable
	Conjable
	Sequable
	Nth(index int) Value               // インデックスで要素を取得
	Assoc(index int, val Value) Vector // インデックスで要素を設定
	Pop() Vector                       // 最後の要素を除いたベクターを返す
	RSeq() Sequable                    // 逆順シーケンスを返す
}

// Map は永続的なマップを表します。
type Map interface {
	Value
	Countable
	Conjable
	Sequable
	Assoc(key, val Value) Map // キーと値を追加/更新
	Dissoc(key Value) Map     // キーを削除
	Get(key Value) Value      // キーで値を取得（見つからない場合はnil）
	Find(key Value) *MapEntry // キーでエントリを取得（見つからない場合はnil）
	Keys() Sequable           // キーのシーケンスを返す
	Vals() Sequable           // 値のシーケンスを返す
	RSeq() Sequable           // 逆順シーケンスを返す（ソート済みマップの場合）
}

// Seq はシーケンスを表します。
type Seq interface {
	Value
	First() Value // 最初の要素を返す（空の場合はnil）
	Next() Seq    // 残りの要素を返す（空の場合はnil）
	More() Seq    // Next()と同じだが、空の場合は空のシーケンスを返す
}

// ----------------------------------------------------------------------------
// 遅延シーケンス関連インターフェース
// ----------------------------------------------------------------------------

// ReduceFn は reduce 用の関数型です。
type ReduceFn func(acc, val Value) Value

// Pending is a value whose deferred computation may be complete or outstanding.
type Pending interface {
	Realized() bool
}

// LazySeq は遅延評価シーケンスを表すマーカーインターフェースです。
type LazySeq interface {
	Seq
	Pending
}

// ReducibleSeq は直接 reduce 可能なシーケンスです。
// reduce はこのインターフェースを検出して最適化を行います。
type ReducibleSeq interface {
	Seq
	// ReduceDirect は最適化された reduce を実行します。
	// 最適化が適用できた場合は (結果, true, nil) を返します。
	// 最適化が適用できない場合は (nil, false, nil) を返します。
	ReduceDirect(fn ReduceFn, init Value) (Value, bool, error)
}

// Fn は関数として呼び出し可能な値を表します。
type Fn interface {
	Value
	Invoke(ctx context.Context, args ...Value) (Value, error) // 関数として呼び出す
}

// ----------------------------------------------------------------------------
// 位置情報
// ----------------------------------------------------------------------------

// Position は式の位置情報を表します。
type Position struct {
	Line   int
	Column int
}
