package span

// Position は 1-based の行・列を表します。
type Position struct {
	Line   int
	Column int
}

// Span はソース範囲を表します。
type Span struct {
	File        string
	Start       Position
	End         Position
	StartOffset int
	EndOffset   int
}

// New は正規化済み Span を返します。
func New(file string, startLine, startCol, endLine, endCol int) Span {
	if startLine <= 0 {
		startLine = 1
	}
	if startCol <= 0 {
		startCol = 1
	}
	if endLine <= 0 {
		endLine = startLine
	}
	if endCol <= 0 {
		endCol = startCol
	}
	return Span{
		File:  file,
		Start: Position{Line: startLine, Column: startCol},
		End:   Position{Line: endLine, Column: endCol},
	}
}

// Unknown は未確定位置の Span を返します。
func Unknown(file string) Span {
	return New(file, 1, 1, 1, 1)
}

// IsZero は実質未設定かを返します。
func (s Span) IsZero() bool {
	return s.Start.Line <= 0 || s.Start.Column <= 0 || s.End.Line <= 0 || s.End.Column <= 0
}
