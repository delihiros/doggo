package debugmeta

import "testing"

func TestTrackerUsesExpansionFallbackForCurrentInstruction(t *testing.T) {
	tracker := NewTracker()
	tracker.SetSourceFile("demo.dg")
	tracker.SetCurrentSpan(Span{Line: 10, Column: 4, StartOffset: 30, EndOffset: 35})
	tracker.PushExpansionSource(Span{Line: 2, Column: 1, StartOffset: 0, EndOffset: 12})

	line, column := tracker.EffectiveLocation()
	if line != 2 || column != 1 {
		t.Fatalf("EffectiveLocation() = (%d,%d), want (2,1)", line, column)
	}
}

func TestTrackerPreservesOutermostExpansionSource(t *testing.T) {
	tracker := NewTracker()
	tracker.SetSourceFile("demo.dg")
	tracker.SetCurrentSpan(Span{Line: 20, Column: 8, StartOffset: 80, EndOffset: 96})
	tracker.PushExpansionSource(Span{Line: 2, Column: 1, StartOffset: 0, EndOffset: 18})
	tracker.PushExpansionSource(Span{Line: 7, Column: 3, StartOffset: 31, EndOffset: 52})

	file, line, column := tracker.EffectiveSourceLocation()
	if file != "demo.dg" || line != 2 || column != 1 {
		t.Fatalf("EffectiveSourceLocation() = (%q,%d,%d), want (%q,%d,%d)", file, line, column, "demo.dg", 2, 1)
	}

	span, ok := tracker.SourceSpan()
	if !ok {
		t.Fatal("expected source span")
	}
	if span.StartOffset != 0 || span.EndOffset != 18 {
		t.Fatalf("SourceSpan() = %+v, want outermost offsets 0..18", span)
	}
}
