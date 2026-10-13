package hir

import "doggo/internal/compiler/sexpr"

type ScopeKind string

const (
	ScopeLocal ScopeKind = "local"
	ScopeParam ScopeKind = "param"
)

type Binding struct {
	ID      int
	ScopeID int
	Name    string
	Depth   int
	Slot    int
	Kind    ScopeKind
}

type Node interface {
	Form() sexpr.Form
	isNode()
}

type FormNode struct {
	Value sexpr.Form
}

func (n FormNode) Form() sexpr.Form {
	return n.Value
}

func (FormNode) isNode() {}

type Program struct {
	Namespace string
	Aliases   map[string]string
	Forms     []Node
	Bindings  []Binding
}
