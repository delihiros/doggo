package types

import (
	"context"

	"doggo/internal/types/util"
)

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

// ResolvedSymbol is a symbol value with an attached compiler/runtime binding.
// Equality and hashing are based only on Name so it remains interoperable with plain symbols.
type ResolvedSymbol struct {
	Name       string
	Meta       Map
	Resolution SymbolResolution
}

func NewResolvedSymbol(name string, resolution SymbolResolution) *ResolvedSymbol {
	return &ResolvedSymbol{Name: name, Resolution: resolution}
}

func NewResolvedSymbolWithMeta(name string, resolution SymbolResolution, meta Map) *ResolvedSymbol {
	return &ResolvedSymbol{Name: name, Meta: meta, Resolution: resolution}
}

func (s *ResolvedSymbol) String() string {
	return s.Name
}

func (s *ResolvedSymbol) Type() string {
	return "symbol"
}

func (s *ResolvedSymbol) Hash() uint32 {
	return util.HashString(s.Name)
}

func (s *ResolvedSymbol) Equals(other Value) bool {
	return symbolEqualsName(s.Name, other)
}

func (s *ResolvedSymbol) Invoke(ctx context.Context, args ...Value) (Value, error) {
	return invokeSymbolMapLookup(s.Name, args)
}

func (s *ResolvedSymbol) IsSelfEvaluating() bool {
	return false
}
