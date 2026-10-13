package atom

import (
	"strings"
	"testing"
)

func TestGoObjectUsesStableIdentitySemantics(t *testing.T) {
	builder := &strings.Builder{}
	object := NewGoObject(builder)
	hash := object.Hash()

	_, _ = builder.WriteString("changed")
	if object.Hash() != hash {
		t.Fatal("GoObject hash changed when the wrapped object mutated")
	}
	if !object.Equals(object) {
		t.Fatal("GoObject must equal itself")
	}
	if object.Equals(NewGoObject(builder)) {
		t.Fatal("distinct GoObject wrappers must not be equal")
	}
}

func TestGoObject(t *testing.T) {
	tests := []struct {
		name     string
		obj      *GoObject
		expected string
	}{
		{"nil object", NewGoObject(nil), "#<go-object:nil>"},
		{"int object", NewGoObject(42), "#<go-object:int>"},
		{"string object", NewGoObject("hello"), "#<go-object:string>"},
		{"struct object", NewGoObject(struct{}{}), "#<go-object:struct {}>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.obj.String() != tt.expected {
				t.Errorf("GoObject.String() = %v, want %v", tt.obj.String(), tt.expected)
			}
			if tt.obj.Type() != "go-object" {
				t.Errorf("GoObject.Type() = %v, want go-object", tt.obj.Type())
			}
		})
	}

	// Test Unwrap
	val := 42
	obj := NewGoObject(val)
	if obj.Unwrap() != val {
		t.Errorf("GoObject.Unwrap() = %v, want %v", obj.Unwrap(), val)
	}
}
