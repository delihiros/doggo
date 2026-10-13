package coll

import (
	"doggo/internal/types"
)

// List はリスト（S式の基本構造）を表します。
type List struct {
	Items       []types.Value
	Line        int // ソースコードの行番号（0は不明を意味する）
	Column      int // ソースコードの列番号（0は不明を意味する）
	StartOffset int
	EndOffset   int
	Metadata    *Map // メタデータ（nilはメタデータなし）
}

func (l *List) String() string {
	if l == nil {
		return "()"
	}

	// リストが空の場合は "()" を返す
	if len(l.Items) == 0 {
		return "()"
	}

	// Items配列を直接反復処理する
	var result = "("
	for i, val := range l.Items {
		if i > 0 {
			result += " "
		}
		if val != nil {
			result += val.String()
		} else {
			result += "nil"
		}
	}

	result += ")"
	return result
}

func (l *List) Type() string {
	return "list"
}

func (l *List) Hash() uint32 {
	if l == nil {
		return 0
	}
	return SeqHash(l)
}

func (l *List) Equals(other types.Value) bool {
	if l == nil {
		return other == nil
	}
	return SequentialEquals(l, other)
}

// NewList は新しいリストを作成します。
func NewList(items ...types.Value) *List {
	return &List{Items: items}
}

// NewListWithPos は行番号/列番号情報付きの新しいリストを作成します。
func NewListWithPos(line, column int, items ...types.Value) *List {
	return &List{Items: items, Line: line, Column: column}
}

// NewListWithSpanAndPos は行番号/列番号とバイト範囲付きの新しいリストを作成します。
func NewListWithSpanAndPos(line, column, startOffset, endOffset int, items ...types.Value) *List {
	return &List{
		Items:       items,
		Line:        line,
		Column:      column,
		StartOffset: startOffset,
		EndOffset:   endOffset,
	}
}

// NewListWithLineAndMetadata は行番号とメタデータ付きの新しいリストを作成します。
func NewListWithLineAndMetadata(line int, items []types.Value, metadata *Map) *List {
	return &List{Items: items, Line: line, Metadata: metadata}
}

// SetLine はリストの行番号を設定します。
func (l *List) SetLine(line int) *List {
	if l != nil {
		l.Line = line
	}
	return l
}

// SetPos はリストの行番号/列番号を設定します。
func (l *List) SetPos(line, column int) *List {
	if l != nil {
		l.Line = line
		l.Column = column
	}
	return l
}

// SetSpan はリストのソースバイト範囲を設定します。
func (l *List) SetSpan(startOffset, endOffset int) *List {
	if l != nil {
		l.StartOffset = startOffset
		l.EndOffset = endOffset
	}
	return l
}

// GetLine はリストの行番号を返します。
func (l *List) GetLine() int {
	if l == nil {
		return 0
	}
	return l.Line
}

// GetColumn はリストの列番号を返します。
func (l *List) GetColumn() int {
	if l == nil {
		return 0
	}
	return l.Column
}

// List実装

// Count はリストの要素数を返します。
func (l *List) Count() int {
	if l == nil {
		return 0
	}
	return len(l.Items)
}

// Conj はリストに要素を追加します（先頭に追加）。
func (l *List) Conj(item types.Value) types.Value {
	if l == nil {
		return NewList(item)
	}
	return NewList(append([]types.Value{item}, l.Items...)...)
}

// Seq はリストをシーケンスに変換します。
func (l *List) Seq() types.Value {
	if l == nil || len(l.Items) == 0 {
		return nil
	}
	return l
}

// Peek はリストの先頭要素を返します。
func (l *List) Peek() types.Value {
	if l == nil || len(l.Items) == 0 {
		return nil
	}
	return l.Items[0]
}

// Pop は先頭要素を除いたリストを返します。
func (l *List) Pop() types.List {
	if l == nil || len(l.Items) == 0 {
		return NewList()
	}
	return NewList(l.Items[1:]...)
}

// First はリストの最初の要素を返します（Seq実装）。
func (l *List) First() types.Value {
	if l == nil || len(l.Items) == 0 {
		return nil
	}
	return l.Items[0]
}

// nextImpl はNextとMoreの共通ロジックです。
// 次のシーケンスを返します。存在しない場合はnilを返します。
func (l *List) nextImpl() types.Seq {
	if l == nil || len(l.Items) <= 1 {
		return nil
	}
	return NewList(l.Items[1:]...)
}

// Next はリストの残りの要素を返します（Seq実装）。
func (l *List) Next() types.Seq {
	return l.nextImpl()
}

// More はNext()と同じですが、空の場合は空のシーケンスを返します。
func (l *List) More() types.Seq {
	if ret := l.nextImpl(); ret != nil {
		return ret
	}
	return NewList()
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (l *List) IsSelfEvaluating() bool {
	return false
}
