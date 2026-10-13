package debugmeta

type Span struct {
	File        string
	Line        int
	Column      int
	StartOffset int
	EndOffset   int
}

type Tracker struct {
	sourceFile string
	current    Span
	expansion  []Span
	scopes     []Table
}

func NewTracker() *Tracker {
	return &Tracker{
		scopes: []Table{NewTable("")},
	}
}

func (t *Tracker) SetSourceFile(file string) {
	if file == "" {
		return
	}
	t.sourceFile = file
	if t.current.File == "" {
		t.current.File = file
	}
}

func (t *Tracker) SetCurrentSpan(span Span) {
	if span.File == "" {
		span.File = t.sourceFile
	}
	if span.File != "" {
		t.sourceFile = span.File
		t.current.File = span.File
	}
	if span.Line > 0 {
		t.current.Line = span.Line
	}
	if span.Column > 0 {
		t.current.Column = span.Column
	}
	if span.StartOffset >= 0 {
		t.current.StartOffset = span.StartOffset
	}
	if span.EndOffset > 0 {
		t.current.EndOffset = span.EndOffset
	}
}

func (t *Tracker) PushExpansionSource(span Span) {
	if span.File == "" {
		span.File = t.sourceFile
	}
	t.expansion = append(t.expansion, span)
}

func (t *Tracker) PopExpansionSource() {
	if len(t.expansion) == 0 {
		return
	}
	t.expansion = t.expansion[:len(t.expansion)-1]
}

func (t *Tracker) EffectiveLocation() (int, int) {
	_, line, column := t.EffectiveSourceLocation()
	return line, column
}

func (t *Tracker) EffectiveSourceLocation() (string, int, int) {
	if span, ok := t.SourceSpan(); ok && span.Line > 0 {
		return coalesceFile(span.File, t.sourceFile), span.Line, coalesceColumn(span.Column)
	}
	return coalesceFile(t.current.File, t.sourceFile), t.current.Line, coalesceColumn(t.current.Column)
}

func (t *Tracker) SourceSpan() (Span, bool) {
	if len(t.expansion) > 0 {
		span := t.expansion[0]
		if span.File == "" {
			span.File = t.sourceFile
		}
		if hasSpanData(span) {
			return span, true
		}
	}
	span := t.current
	if span.File == "" {
		span.File = t.sourceFile
	}
	if !hasSpanData(span) {
		return Span{}, false
	}
	return span, true
}

func (t *Tracker) RecordInstructions(count int) {
	scope := &t.scopes[len(t.scopes)-1]
	if scope.SourceFile == "" {
		scope.SourceFile = t.sourceFile
	}
	line, column := t.EffectiveLocation()
	scope.Record(line, column, count)
}

func (t *Tracker) EnterScope() {
	t.scopes = append(t.scopes, NewTable(t.sourceFile))
}

func (t *Tracker) LeaveScope() Table {
	if len(t.scopes) == 0 {
		return NewTable(t.sourceFile)
	}
	scope := t.scopes[len(t.scopes)-1].Clone()
	if scope.SourceFile == "" {
		scope.SourceFile = t.sourceFile
	}
	if len(t.scopes) > 1 {
		t.scopes = t.scopes[:len(t.scopes)-1]
	}
	return scope
}

func (t *Tracker) InstructionTable() Table {
	if len(t.scopes) == 0 {
		return NewTable(t.sourceFile)
	}
	scope := t.scopes[len(t.scopes)-1].Clone()
	if scope.SourceFile == "" {
		scope.SourceFile = t.sourceFile
	}
	return scope
}

func coalesceFile(file, fallback string) string {
	if file != "" {
		return file
	}
	return fallback
}

func coalesceColumn(column int) int {
	if column > 0 {
		return column
	}
	return 1
}

func hasSpanData(span Span) bool {
	return span.File != "" || span.Line > 0 || span.Column > 0 || span.StartOffset >= 0 || span.EndOffset > 0
}
