package types

import "reflect"

// FormatCollection はコレクションの文字列表現をフォーマットします。
func FormatCollection(items []Value, openBracket, closeBracket string, empty string) string {
	if len(items) == 0 {
		return empty
	}
	result := openBracket
	for i, item := range items {
		if i > 0 {
			result += " "
		}
		result += item.String()
	}
	result += closeBracket
	return result
}

// Equal は2つの値が等しいかどうかを判定します。
// nil値に対しても安全です。
func Equal(a, b Value) bool {
	if a == nil {
		return b == nil
	}
	if b == nil {
		return false
	}
	return a.Equals(b)
}

// Identical は深い等価性を評価せず、同じ値表現かを判定します。
func Identical(a, b Value) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aType := reflect.TypeOf(a)
	return aType == reflect.TypeOf(b) && aType.Comparable() && a == b
}

// IsTruthy は値が真かどうかを判定します。
// Clojureの仕様に従い、nilとfalseのみが偽値で、それ以外はすべて真値です。
func IsTruthy(val Value) bool {
	if val == nil {
		return false
	}

	// 型名で判定（循環参照を避けるため）
	if val.Type() == "nil" {
		return false
	}

	if val.Type() == "boolean" {
		return val.String() == "true"
	}

	// nilとfalse以外はすべて真値
	return true
}

// ToSlice はシーケンスを[]Valueスライスに変換します。
// ListやLazySeqの場合はSeqインターフェースを使用して走査し、遅延評価を適切に処理します。
// nilまたはNil型の場合は空のスライスを返します。
func ToSlice(arg Value) ([]Value, bool) {
	if arg == nil {
		return []Value{}, true
	}
	if arg.Type() == "nil" {
		return []Value{}, true
	}

	// Vectorの場合は直接フィールドにアクセスできないため、Sequableとして処理
	// LazySeqと空のListを処理
	sequable, ok := arg.(Sequable)
	if !ok {
		return nil, false
	}

	seq := sequable.Seq()
	if seq == nil {
		return []Value{}, true
	}

	iseq, ok := seq.(Seq)
	if !ok {
		return nil, false
	}

	var items []Value
	for iseq != nil {
		items = append(items, iseq.First())
		iseq = iseq.Next()
	}
	return items, true
}
