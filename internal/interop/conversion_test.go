package interop

import (
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type namedString string
type namedBool bool
type pointerValue struct{}
type structInputTarget struct {
	Visible string
	hidden  string
}
type embeddedStructInput struct {
	Visible string
}
type nilEmbeddedStructTarget struct {
	*embeddedStructInput
}

func TestConvertReflectValueToDoggoPreservesPointers(t *testing.T) {
	source := &pointerValue{}
	converted, err := ConvertReflectValueToDoggo(reflect.ValueOf(source))
	if err != nil {
		t.Fatalf("ConvertReflectValueToDoggo() error = %v", err)
	}
	goObject, ok := converted.(*atom.GoObject)
	if !ok {
		t.Fatalf("converted value = %T, want *atom.GoObject", converted)
	}
	if goObject.Unwrap() != source {
		t.Fatal("converted value did not preserve pointer identity")
	}
}

func TestConvertReflectValueToDoggoMapsNilPointersToNil(t *testing.T) {
	var source *pointerValue
	converted, err := ConvertReflectValueToDoggo(reflect.ValueOf(source))
	if err != nil {
		t.Fatalf("ConvertReflectValueToDoggo() error = %v", err)
	}
	if converted != atom.NilValue {
		t.Fatalf("converted value = %v, want nil", converted)
	}
}

func TestConvertReflectValueToDoggoMapsEveryNilableKindToNil(t *testing.T) {
	var pointer *pointerValue
	var slice []byte
	var mapping map[string]int
	var channel chan int
	var function func()
	var unsafePointer unsafe.Pointer

	tests := []struct {
		name  string
		value any
	}{
		{name: "pointer", value: pointer},
		{name: "slice", value: slice},
		{name: "map", value: mapping},
		{name: "channel", value: channel},
		{name: "function", value: function},
		{name: "unsafe pointer", value: unsafePointer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converted, err := ConvertReflectValueToDoggo(reflect.ValueOf(tt.value))
			if err != nil {
				t.Fatalf("ConvertReflectValueToDoggo() error = %v", err)
			}
			if converted != atom.NilValue {
				t.Fatalf("converted value = %v, want nil", converted)
			}
		})
	}
}

func TestConvertDoggoToReflectValuePreservesNamedScalarTypes(t *testing.T) {
	tests := []struct {
		value  types.Value
		target reflect.Type
	}{
		{value: atom.String("doggo"), target: reflect.TypeOf(namedString(""))},
		{value: atom.Boolean(true), target: reflect.TypeOf(namedBool(false))},
	}

	for _, tt := range tests {
		converted, err := ConvertDoggoToReflectValue(tt.value, tt.target)
		if err != nil {
			t.Fatalf("ConvertDoggoToReflectValue() error = %v", err)
		}
		if converted.Type() != tt.target {
			t.Fatalf("converted type = %s, want %s", converted.Type(), tt.target)
		}
	}
}

func TestConvertDoggoToReflectValueConvertsNilToAny(t *testing.T) {
	target := reflect.TypeOf((*any)(nil)).Elem()
	converted, err := ConvertDoggoToReflectValue(atom.NilValue, target)
	if err != nil {
		t.Fatalf("ConvertDoggoToReflectValue() error = %v", err)
	}
	if !converted.IsValid() || converted.Type() != target || !converted.IsNil() {
		t.Fatalf("converted value = %#v, want nil %s", converted, target)
	}
}

func TestConvertDoggoToReflectValueConvertsNilToEveryNilableKind(t *testing.T) {
	tests := []struct {
		name   string
		target reflect.Type
	}{
		{name: "pointer", target: reflect.TypeOf((*pointerValue)(nil))},
		{name: "slice", target: reflect.TypeOf([]byte(nil))},
		{name: "map", target: reflect.TypeOf(map[string]int(nil))},
		{name: "channel", target: reflect.TypeOf((chan int)(nil))},
		{name: "function", target: reflect.TypeOf((func())(nil))},
		{name: "unsafe pointer", target: reflect.TypeOf(unsafe.Pointer(nil))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converted, err := ConvertDoggoToReflectValue(atom.NilValue, tt.target)
			if err != nil {
				t.Fatalf("ConvertDoggoToReflectValue() error = %v", err)
			}
			if !converted.IsValid() || converted.Type() != tt.target || !converted.IsZero() {
				t.Fatalf("converted value = %#v, want nil %s", converted, tt.target)
			}
		})
	}
}

func TestConvertDoggoToReflectValueConvertsNilGoObjectToNilInterface(t *testing.T) {
	target := reflect.TypeOf((*io.Reader)(nil)).Elem()
	converted, err := ConvertDoggoToReflectValue(atom.NewGoObject(nil), target)
	if err != nil {
		t.Fatalf("ConvertDoggoToReflectValue() error = %v", err)
	}
	if !converted.IsValid() || converted.Type() != target || !converted.IsNil() {
		t.Fatalf("converted value = %#v, want nil %s", converted, target)
	}
}

func TestConvertDoggoToReflectValueConvertsTypedNilGoObjectToAny(t *testing.T) {
	var goObject *atom.GoObject
	target := reflect.TypeOf((*any)(nil)).Elem()
	converted, err := ConvertDoggoToReflectValue(goObject, target)
	if err != nil {
		t.Fatalf("ConvertDoggoToReflectValue() error = %v", err)
	}
	if !converted.IsValid() || converted.Type() != target || !converted.IsNil() {
		t.Fatalf("converted value = %#v, want nil %s", converted, target)
	}
}

func TestConvertReflectValueToDoggoRejectsUint64Overflow(t *testing.T) {
	_, err := ConvertReflectValueToDoggo(reflect.ValueOf(uint64(math.MaxUint64)))
	if err == nil || !strings.Contains(err.Error(), "uint64") {
		t.Fatalf("ConvertReflectValueToDoggo() error = %v, want uint64 range error", err)
	}
}

func TestConvertDoggoToReflectValueRejectsIntegerNarrowing(t *testing.T) {
	tests := []struct {
		name   string
		value  int64
		target reflect.Type
	}{
		{name: "int8", value: 128, target: reflect.TypeOf(int8(0))},
		{name: "uint8", value: 256, target: reflect.TypeOf(uint8(0))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ConvertDoggoToReflectValue(atom.NewNumberInt(tt.value), tt.target)
			if err == nil {
				t.Fatalf("ConvertDoggoToReflectValue(%d, %s) succeeded, want range error", tt.value, tt.target)
			}
		})
	}
}

func TestConvertDoggoToReflectValueRejectsFractionalInteger(t *testing.T) {
	_, err := ConvertDoggoToReflectValue(atom.NewNumberFloat(1.5), reflect.TypeOf(int64(0)))
	if err == nil {
		t.Fatal("ConvertDoggoToReflectValue(1.5, int64) succeeded, want loss error")
	}
}

func TestConvertDoggoToReflectValueRejectsNonFiniteInteger(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := ConvertDoggoToReflectValue(atom.NewNumberFloat(value), reflect.TypeOf(int64(0)))
		if err == nil {
			t.Fatalf("ConvertDoggoToReflectValue(%v, int64) succeeded, want range error", value)
		}
	}
}

func TestPopulateStructFromMapRejectsUnknownStructField(t *testing.T) {
	tests := []struct {
		name string
		key  types.Value
	}{
		{name: "unknown", key: atom.NewKeyword("Visble")},
		{name: "unexported", key: atom.NewKeyword("hidden")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := coll.NewMap(map[types.Value]types.Value{
				tt.key: atom.String("discarded"),
			})
			target := reflect.New(reflect.TypeOf(structInputTarget{})).Elem()
			if err := PopulateStructFromMap(target, input); err == nil {
				t.Fatalf("PopulateStructFromMap() accepted %s field", tt.name)
			}
		})
	}
}

func TestPopulateStructFromMapRejectsNilEmbeddedField(t *testing.T) {
	input := coll.NewMap(map[types.Value]types.Value{
		atom.NewKeyword("Visible"): atom.String("must-not-panic"),
	})
	target := reflect.New(reflect.TypeOf(nilEmbeddedStructTarget{})).Elem()
	if err := PopulateStructFromMap(target, input); err == nil {
		t.Fatal("PopulateStructFromMap() traversed a nil embedded pointer")
	}
}

func TestPopulateStructFromKeywordsRejectsNilEmbeddedField(t *testing.T) {
	target := reflect.New(reflect.TypeOf(nilEmbeddedStructTarget{})).Elem()
	err := PopulateStructFromKeywords(target, []types.Value{
		atom.NewKeyword("Visible"),
		atom.String("must-not-panic"),
	})
	if err == nil {
		t.Fatal("PopulateStructFromKeywords() traversed a nil embedded pointer")
	}
}
