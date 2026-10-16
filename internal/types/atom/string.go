package atom

import (
	"doggo/internal/textutil"
	"doggo/internal/types"
	"doggo/internal/types/util"
)

// String は文字列を表します。
type String string

func (s String) String() string {
	return textutil.QuoteDoggoString(string(s))
}

func (s String) Display() string {
	return string(s)
}

func (s String) Type() string {
	return "string"
}

func (s String) Hash() uint32 {
	return util.HashString(string(s))
}

func (s String) Equals(other types.Value) bool {
	if other == nil {
		return false
	}
	if o, ok := other.(String); ok {
		return s == o
	}
	return false
}

// NewString は新しい文字列を作成します。
func NewString(val string) String {
	return String(val)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (s String) IsSelfEvaluating() bool {
	return true
}

// Character は文字を表します。
type Character rune

// specialCharNames は特別な文字名とそのルーンのマッピングです。
var specialCharNames = map[string]rune{
	"newline": '\n',
	"space":   ' ',
	"tab":     '\t',
	"return":  '\r',
}

// specialCharNamesReverse は特別なルーンとその文字名のマッピングです。
var specialCharNamesReverse = map[rune]string{
	'\n': "newline",
	' ':  "space",
	'\t': "tab",
	'\r': "return",
}

func (c Character) String() string {
	r := rune(c)
	if name, ok := specialCharNamesReverse[r]; ok {
		return `\` + name
	}
	return `\` + string(r)
}

func (c Character) Display() string {
	return string(c)
}

func (c Character) Type() string {
	return "character"
}

func (c Character) Hash() uint32 {
	return uint32(c)
}

func (c Character) Equals(other types.Value) bool {
	if other == nil {
		return false
	}
	if o, ok := other.(Character); ok {
		return c == o
	}
	return false
}

// NewCharacter は新しいCharacterを作成します。
func NewCharacter(val rune) Character {
	return Character(val)
}

// NewCharacterFromLiteral はリテラル文字列（a, newline など）からCharacterを作成します。
// 無効なリテラルの場合はok=falseを返します。
func NewCharacterFromLiteral(literal string) (Character, bool) {
	// 特別な文字名をチェック
	if r, ok := specialCharNames[literal]; ok {
		return Character(r), true
	}

	// 単一文字の場合
	runes := []rune(literal)
	if len(runes) == 1 {
		return Character(runes[0]), true
	}

	// 無効なリテラル
	return 0, false
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (c Character) IsSelfEvaluating() bool {
	return true
}

// Rune は基礎のruneを返します。
func (c Character) Rune() rune {
	return rune(c)
}
