package debugmeta

import (
	"doggo/internal/compiler/sexpr"
	"doggo/internal/types/coll"
)

// Owner centralizes compiler debug/source metadata ownership around Tracker.
type Owner struct {
	tracker              *Tracker
	expansionSourceForms []any
}

func NewOwner() *Owner {
	return &Owner{tracker: NewTracker()}
}

func (o *Owner) SetCurrent(file string, line, column int) {
	if o == nil || o.tracker == nil {
		return
	}
	if file != "" {
		o.tracker.SetSourceFile(file)
	}
	if line > 0 || column > 0 {
		o.tracker.SetCurrentSpan(Span{
			File:   o.EffectiveFile(),
			Line:   line,
			Column: column,
		})
	}
}

func (o *Owner) SetCurrentSpan(span Span) {
	if o == nil || o.tracker == nil {
		return
	}
	if span.File == "" {
		span.File = o.EffectiveFile()
	}
	o.tracker.SetCurrentSpan(span)
}

func (o *Owner) PushExpansionSource(span Span, sourceForm any) {
	if o == nil || o.tracker == nil {
		return
	}
	if span.File == "" {
		span.File = o.EffectiveFile()
	}
	o.expansionSourceForms = append(o.expansionSourceForms, sourceForm)
	o.tracker.PushExpansionSource(span)
}

func (o *Owner) PopExpansionSource() {
	if o == nil || o.tracker == nil {
		return
	}
	if len(o.expansionSourceForms) > 0 {
		o.expansionSourceForms = o.expansionSourceForms[:len(o.expansionSourceForms)-1]
	}
	o.tracker.PopExpansionSource()
}

func (o *Owner) RecordInstructions(count int) {
	if o == nil || o.tracker == nil {
		return
	}
	o.tracker.RecordInstructions(count)
}

func (o *Owner) EnterScope() {
	if o == nil || o.tracker == nil {
		return
	}
	o.tracker.EnterScope()
}

func (o *Owner) LeaveScope() Table {
	if o == nil || o.tracker == nil {
		return Table{}
	}
	return o.tracker.LeaveScope()
}

func (o *Owner) InstructionTable() Table {
	if o == nil || o.tracker == nil {
		return Table{}
	}
	return o.tracker.InstructionTable()
}

func (o *Owner) EffectiveSourceLocation() (string, int, int) {
	if o == nil || o.tracker == nil {
		return "", 0, 0
	}
	return o.tracker.EffectiveSourceLocation()
}

func (o *Owner) SourceSpan() (Span, bool) {
	if o == nil || o.tracker == nil {
		return Span{}, false
	}
	return o.tracker.SourceSpan()
}

func (o *Owner) EffectiveFile() string {
	if o == nil || o.tracker == nil {
		return ""
	}
	file, _, _ := o.tracker.EffectiveSourceLocation()
	if file != "" {
		return file
	}
	return o.tracker.sourceFile
}

func (o *Owner) SourceFormText(sourceForm any, sourceContentProvider func(string) (string, bool)) string {
	if o == nil || sourceContentProvider == nil {
		return ""
	}
	file, _, _ := o.EffectiveSourceLocation()
	if file == "" {
		return ""
	}
	content, ok := sourceContentProvider(file)
	if !ok {
		return ""
	}
	start, end, ok := o.effectiveSourceFormOffsets(sourceForm, len(content))
	if !ok {
		return ""
	}
	return content[start:end]
}

func (o *Owner) effectiveSourceFormOffsets(sourceForm any, contentLen int) (int, int, bool) {
	if span, ok := o.SourceSpan(); ok {
		if span.StartOffset >= 0 && span.EndOffset > span.StartOffset && span.EndOffset <= contentLen {
			return span.StartOffset, span.EndOffset, true
		}
	}
	for i := 0; i < len(o.expansionSourceForms); i++ {
		if start, end, ok := sourceFormOffsets(o.expansionSourceForms[i], contentLen); ok {
			return start, end, true
		}
	}
	return sourceFormOffsets(sourceForm, contentLen)
}

func sourceFormOffsets(value any, contentLen int) (int, int, bool) {
	switch list := value.(type) {
	case *coll.List:
		if list == nil {
			return 0, 0, false
		}
		if list.StartOffset < 0 || list.EndOffset <= list.StartOffset || list.EndOffset > contentLen {
			return 0, 0, false
		}
		return list.StartOffset, list.EndOffset, true
	case *sexpr.List:
		if list == nil {
			return 0, 0, false
		}
		sp := list.Span()
		if sp.StartOffset < 0 || sp.EndOffset <= sp.StartOffset || sp.EndOffset > contentLen {
			return 0, 0, false
		}
		return sp.StartOffset, sp.EndOffset, true
	default:
		return 0, 0, false
	}
}
