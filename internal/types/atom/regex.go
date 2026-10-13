package atom

import (
	"regexp"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// Regex は正規表現を表します。
type Regex struct {
	Pattern string         // 元のパターン文字列
	Re      *regexp.Regexp // コンパイル済み正規表現
}

func (r *Regex) String() string {
	return `#"` + r.Pattern + `"`
}

func (r *Regex) Display() string {
	return r.String()
}

func (r *Regex) Type() string {
	return "regex"
}

func (r *Regex) Hash() uint32 {
	return util.HashString(r.Pattern)
}

func (r *Regex) Equals(other types.Value) bool {
	if other == nil {
		return false
	}
	if o, ok := other.(*Regex); ok {
		return r.Pattern == o.Pattern
	}
	return false
}

// NewRegex は新しい正規表現を作成します。
// パターンが不正な場合はエラーを返します。
func NewRegex(pattern string) (*Regex, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &Regex{Pattern: pattern, Re: re}, nil
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (r *Regex) IsSelfEvaluating() bool {
	return true
}

