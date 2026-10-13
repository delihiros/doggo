package parser

import (
	"testing"

	"doggo/internal/lexer"
	"doggo/internal/syntax/form"
)

func TestParseBuildsSyntaxNodeTreeWithSpans(t *testing.T) {
	source := "(def x 1)\n"

	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	p := New(tokens, source)
	node, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if node == nil {
		t.Fatal("Parse returned nil")
	}
	if node.Kind != form.KindList {
		t.Fatalf("node.Kind = %q, want %q", node.Kind, form.KindList)
	}
	if got := len(node.Children()); got != 3 {
		t.Fatalf("len(node.Children()) = %d, want 3", got)
	}
	if node.Children()[0].Kind != form.KindSymbol {
		t.Fatalf("head.Kind = %q, want %q", node.Children()[0].Kind, form.KindSymbol)
	}
	if node.Children()[2].Kind != form.KindNumber {
		t.Fatalf("tail.Kind = %q, want %q", node.Children()[2].Kind, form.KindNumber)
	}
	if node.Span.Start.Line != 1 || node.Span.Start.Column != 1 {
		t.Fatalf("unexpected start span: %+v", node.Span.Start)
	}
	if node.Span.End.Line != 1 || node.Span.End.Column != 10 {
		t.Fatalf("unexpected end span: %+v", node.Span.End)
	}
}

func TestParseAttachesMetadataToSyntaxNodes(t *testing.T) {
	source := "^{:private true} foo"

	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	p := New(tokens, source)
	node, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if node == nil {
		t.Fatal("Parse returned nil")
	}
	if node.Kind != form.KindSymbol {
		t.Fatalf("node.Kind = %q, want %q", node.Kind, form.KindSymbol)
	}
	if node.Metadata == nil {
		t.Fatal("expected syntax metadata")
	}
	if node.Metadata.Kind != form.KindMap {
		t.Fatalf("metadata.Kind = %q, want %q", node.Metadata.Kind, form.KindMap)
	}
	if got := len(node.Metadata.MapEntries()); got != 1 {
		t.Fatalf("len(metadata entries) = %d, want 1", got)
	}
	entry := node.Metadata.MapEntries()[0]
	if entry.Key.Kind != form.KindKeyword {
		t.Fatalf("metadata key kind = %q, want %q", entry.Key.Kind, form.KindKeyword)
	}
	if entry.Value.Kind != form.KindBoolean {
		t.Fatalf("metadata value kind = %q, want %q", entry.Value.Kind, form.KindBoolean)
	}
}
