package form

import (
	"strconv"
	"strings"

	"doggo/internal/syntax/span"
)

// Kind is the syntax-node kind emitted by the reader/parser frontend.
type Kind string

const (
	KindUnknown Kind = "unknown"
	KindList    Kind = "list"
	KindVector  Kind = "vector"
	KindMap     Kind = "map"
	KindSet     Kind = "set"
	KindSymbol  Kind = "symbol"
	KindNumber  Kind = "number"
	KindString  Kind = "string"
	KindKeyword Kind = "keyword"
	KindRegex   Kind = "regex"
	KindChar    Kind = "char"
	KindBoolean Kind = "boolean"
	KindNil     Kind = "nil"
)

type NumberLiteral struct {
	Literal string
	IsFloat bool
	Int64   int64
	Float64 float64
}

type CharacterLiteral struct {
	Literal string
	Value   rune
}

type MapEntry struct {
	Key   *Node
	Value *Node
}

// Node is the canonical syntax form emitted by the parser.
// It is intentionally independent from runtime types.Value.
type Node struct {
	Kind        Kind
	Span        span.Span
	StartOffset int
	EndOffset   int
	Metadata    *Node

	Text      string
	Number    NumberLiteral
	Character CharacterLiteral
	Boolean   bool
	Items     []*Node
	Entries   []MapEntry
}

func NewSymbol(name string, sp span.Span, start, end int) *Node {
	return &Node{Kind: KindSymbol, Span: sp, StartOffset: start, EndOffset: end, Text: name}
}

func NewNumber(num NumberLiteral, sp span.Span, start, end int) *Node {
	return &Node{Kind: KindNumber, Span: sp, StartOffset: start, EndOffset: end, Number: num}
}

func NewString(value string, sp span.Span, start, end int) *Node {
	return &Node{Kind: KindString, Span: sp, StartOffset: start, EndOffset: end, Text: value}
}

func NewKeyword(name string, sp span.Span, start, end int) *Node {
	return &Node{Kind: KindKeyword, Span: sp, StartOffset: start, EndOffset: end, Text: name}
}

func NewRegex(pattern string, sp span.Span, start, end int) *Node {
	return &Node{Kind: KindRegex, Span: sp, StartOffset: start, EndOffset: end, Text: pattern}
}

func NewCharacter(char CharacterLiteral, sp span.Span, start, end int) *Node {
	return &Node{Kind: KindChar, Span: sp, StartOffset: start, EndOffset: end, Character: char}
}

func NewBoolean(value bool, sp span.Span, start, end int) *Node {
	return &Node{Kind: KindBoolean, Span: sp, StartOffset: start, EndOffset: end, Boolean: value}
}

func NewNil(sp span.Span, start, end int) *Node {
	return &Node{Kind: KindNil, Span: sp, StartOffset: start, EndOffset: end}
}

func NewList(sp span.Span, start, end int, items ...*Node) *Node {
	return &Node{Kind: KindList, Span: sp, StartOffset: start, EndOffset: end, Items: items}
}

func NewVector(sp span.Span, start, end int, items ...*Node) *Node {
	return &Node{Kind: KindVector, Span: sp, StartOffset: start, EndOffset: end, Items: items}
}

func NewSet(sp span.Span, start, end int, items ...*Node) *Node {
	return &Node{Kind: KindSet, Span: sp, StartOffset: start, EndOffset: end, Items: items}
}

func NewMap(sp span.Span, start, end int, entries ...MapEntry) *Node {
	return &Node{Kind: KindMap, Span: sp, StartOffset: start, EndOffset: end, Entries: entries}
}

func (n *Node) Children() []*Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case KindList, KindVector, KindSet:
		return append([]*Node(nil), n.Items...)
	case KindMap:
		out := make([]*Node, 0, len(n.Entries)*2)
		for _, entry := range n.Entries {
			out = append(out, entry.Key, entry.Value)
		}
		return out
	default:
		return nil
	}
}

func (n *Node) MapEntries() []MapEntry {
	if n == nil || n.Kind != KindMap {
		return nil
	}
	return append([]MapEntry(nil), n.Entries...)
}

func (n *Node) SupportsMetadata() bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case KindList, KindVector, KindMap, KindSet, KindSymbol:
		return true
	default:
		return false
	}
}

func (n *Node) SetMetadata(meta *Node) *Node {
	if n == nil {
		return nil
	}
	n.Metadata = meta
	return n
}

func SetFileRecursive(path string, node *Node) {
	if node == nil {
		return
	}
	setFileRecursive(path, node)
}

func SetFilesRecursive(path string, nodes []*Node) {
	for _, node := range nodes {
		SetFileRecursive(path, node)
	}
}

func setFileRecursive(path string, node *Node) {
	if node == nil {
		return
	}
	node.Span.File = path
	if node.Metadata != nil {
		setFileRecursive(path, node.Metadata)
	}
	for _, item := range node.Items {
		setFileRecursive(path, item)
	}
	for _, entry := range node.Entries {
		setFileRecursive(path, entry.Key)
		setFileRecursive(path, entry.Value)
	}
}

func (n *Node) String() string {
	if n == nil {
		return "nil"
	}

	body := n.render()
	if n.Metadata != nil {
		return "^" + n.Metadata.render() + " " + body
	}
	return body
}

func (n *Node) render() string {
	switch n.Kind {
	case KindList:
		return "(" + joinNodes(n.Items) + ")"
	case KindVector:
		return "[" + joinNodes(n.Items) + "]"
	case KindSet:
		return "#{" + joinNodes(n.Items) + "}"
	case KindMap:
		parts := make([]string, 0, len(n.Entries)*2)
		for _, entry := range n.Entries {
			parts = append(parts, entry.Key.String(), entry.Value.String())
		}
		return "{" + strings.Join(parts, " ") + "}"
	case KindSymbol:
		return n.Text
	case KindNumber:
		return n.Number.Literal
	case KindString:
		return strconv.Quote(n.Text)
	case KindKeyword:
		return ":" + n.Text
	case KindRegex:
		return "#" + strconv.Quote(n.Text)
	case KindChar:
		return `\` + n.Character.Literal
	case KindBoolean:
		if n.Boolean {
			return "true"
		}
		return "false"
	case KindNil:
		return "nil"
	default:
		return ""
	}
}

func joinNodes(nodes []*Node) string {
	if len(nodes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		parts = append(parts, node.String())
	}
	return strings.Join(parts, " ")
}
