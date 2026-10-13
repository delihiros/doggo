package types

import "testing"

type protocolTestValue struct {
	typeName string
}

func (v protocolTestValue) String() string          { return v.typeName }
func (v protocolTestValue) Type() string            { return v.typeName }
func (v protocolTestValue) Hash() uint32            { return 0 }
func (v protocolTestValue) Equals(other Value) bool { return v == other }
func (v protocolTestValue) IsSelfEvaluating() bool  { return true }

func TestProtocolSatisfiesUsesRegisteredImplementation(t *testing.T) {
	protocol := NewProtocol("Runnable", []string{"run"})
	target := protocolTestValue{typeName: "widget"}
	if protocol.Satisfies(target) {
		t.Fatal("protocol unexpectedly satisfied before implementation registration")
	}

	protocol.Extend(target.Type(), map[string]Value{"run": protocolTestValue{typeName: "implementation"}})
	if !protocol.Satisfies(target) {
		t.Fatal("protocol should be satisfied after implementation registration")
	}
}
