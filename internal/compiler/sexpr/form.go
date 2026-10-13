package sexpr

import (
	"strconv"
	"strings"

	"doggo/internal/syntax/span"
)

// Form is the compiler-facing s-expression form.
type Form interface {
	Meta() *Map
	Span() span.Span
	Truthy() bool
	String() string
}

type baseForm struct {
	meta *Map
	sp   span.Span
}

func (b baseForm) Meta() *Map {
	if b.meta == nil {
		return emptyMap
	}
	return b.meta
}

func (b baseForm) Span() span.Span {
	return b.sp
}

type File struct {
	Path      string
	Namespace string
	Forms     []Form
}

type Symbol struct {
	baseForm
	Name string
}

func NewSymbol(name string, sp span.Span, meta *Map) Symbol {
	return Symbol{baseForm: baseForm{meta: meta, sp: sp}, Name: name}
}

func (s Symbol) Truthy() bool { return true }
func (s Symbol) String() string {
	return s.Name
}

type SymbolResolutionKind string

const (
	UnresolvedSymbolResolution SymbolResolutionKind = ""
	GlobalSymbolResolution     SymbolResolutionKind = "global"
	BuiltinSymbolResolution    SymbolResolutionKind = "builtin"
)

type SymbolResolution struct {
	Kind      SymbolResolutionKind
	Index     int
	Namespace string
}

type ResolvedSymbol struct {
	baseForm
	Name       string
	Resolution SymbolResolution
}

func NewResolvedSymbol(name string, resolution SymbolResolution, sp span.Span, meta *Map) ResolvedSymbol {
	return ResolvedSymbol{
		baseForm:   baseForm{meta: meta, sp: sp},
		Name:       name,
		Resolution: resolution,
	}
}

func (s ResolvedSymbol) Truthy() bool { return true }
func (s ResolvedSymbol) String() string {
	return s.Name
}

type Number struct {
	baseForm
	Literal string
}

func NewNumber(literal string, sp span.Span, meta *Map) Number {
	return Number{baseForm: baseForm{meta: meta, sp: sp}, Literal: literal}
}

func (n Number) Truthy() bool { return true }
func (n Number) String() string {
	return n.Literal
}

type String struct {
	baseForm
	Value string
}

func NewString(value string, sp span.Span, meta *Map) String {
	return String{baseForm: baseForm{meta: meta, sp: sp}, Value: value}
}

func (s String) Truthy() bool { return true }
func (s String) String() string {
	return strconv.Quote(s.Value)
}

type Keyword struct {
	baseForm
	Name string
}

func NewKeyword(name string, sp span.Span, meta *Map) Keyword {
	return Keyword{baseForm: baseForm{meta: meta, sp: sp}, Name: name}
}

func (k Keyword) Truthy() bool { return true }
func (k Keyword) String() string {
	return ":" + k.Name
}

type Regex struct {
	baseForm
	Pattern string
}

func NewRegex(pattern string, sp span.Span, meta *Map) Regex {
	return Regex{baseForm: baseForm{meta: meta, sp: sp}, Pattern: pattern}
}

func (r Regex) Truthy() bool { return true }
func (r Regex) String() string {
	return "#" + strconv.Quote(r.Pattern)
}

type Character struct {
	baseForm
	Literal string
}

func NewCharacter(literal string, sp span.Span, meta *Map) Character {
	return Character{baseForm: baseForm{meta: meta, sp: sp}, Literal: literal}
}

func (c Character) Truthy() bool { return true }
func (c Character) String() string {
	return `\` + c.Literal
}

type Bool struct {
	baseForm
	Value bool
}

func NewBool(value bool, sp span.Span, meta *Map) Bool {
	return Bool{baseForm: baseForm{meta: meta, sp: sp}, Value: value}
}

func (b Bool) Truthy() bool { return b.Value }
func (b Bool) String() string {
	if b.Value {
		return "true"
	}
	return "false"
}

type Nil struct {
	baseForm
}

func NewNil(sp span.Span, meta *Map) Nil {
	return Nil{baseForm: baseForm{meta: meta, sp: sp}}
}

func (n Nil) Truthy() bool   { return false }
func (n Nil) String() string { return "nil" }

type List struct {
	baseForm
	Items []Form
}

func NewList(items []Form, sp span.Span, meta *Map) *List {
	return &List{baseForm: baseForm{meta: meta, sp: sp}, Items: items}
}

func (l *List) Truthy() bool { return true }
func (l *List) String() string {
	return "(" + joinForms(l.Items) + ")"
}

type Vector struct {
	baseForm
	Items []Form
}

func NewVector(items []Form, sp span.Span, meta *Map) *Vector {
	return &Vector{baseForm: baseForm{meta: meta, sp: sp}, Items: items}
}

func (v *Vector) Truthy() bool { return true }
func (v *Vector) String() string {
	return "[" + joinForms(v.Items) + "]"
}

type Set struct {
	baseForm
	Items []Form
}

func NewSet(items []Form, sp span.Span, meta *Map) *Set {
	return &Set{baseForm: baseForm{meta: meta, sp: sp}, Items: items}
}

func (s *Set) Truthy() bool { return true }
func (s *Set) String() string {
	return "#{" + joinForms(s.Items) + "}"
}

type MapEntry struct {
	Key   Form
	Value Form
}

type Map struct {
	baseForm
	Entries []MapEntry
}

func NewMap(entries []MapEntry, sp span.Span, meta *Map) *Map {
	return &Map{baseForm: baseForm{meta: meta, sp: sp}, Entries: entries}
}

func (m *Map) Truthy() bool { return true }
func (m *Map) String() string {
	if m == nil {
		return "{}"
	}
	parts := make([]string, 0, len(m.Entries)*2)
	for _, entry := range m.Entries {
		parts = append(parts, entry.Key.String(), entry.Value.String())
	}
	return "{" + strings.Join(parts, " ") + "}"
}

func (m *Map) Get(name string) Form {
	if m == nil {
		return Nil{}
	}
	for _, entry := range m.Entries {
		switch key := entry.Key.(type) {
		case Keyword:
			if key.Name == name {
				return entry.Value
			}
		case Symbol:
			if key.Name == name {
				return entry.Value
			}
		case String:
			if key.Value == name {
				return entry.Value
			}
		}
	}
	return Nil{}
}

var emptyMap = &Map{}

func joinForms(forms []Form) string {
	if len(forms) == 0 {
		return ""
	}
	parts := make([]string, 0, len(forms))
	for _, form := range forms {
		parts = append(parts, form.String())
	}
	return strings.Join(parts, " ")
}
