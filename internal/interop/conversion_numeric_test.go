package interop

import (
	"math"
	"reflect"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

type numericCount int8
type numericUnsigned uint8
type numericFloat float32
type numericComplex complex64

func TestConvertGoObjectRejectsNumericLoss(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		target any
	}{
		{"fractional float64", float64(1.5), int(0)},
		{"fractional float32", float32(1.5), int(0)},
		{"named signed narrowing", int64(256), numericCount(0)},
		{"signed overflow", int64(128), int8(0)},
		{"signed underflow", int64(-129), int8(0)},
		{"unsigned narrowing", uint64(256), uint8(0)},
		{"unsigned to signed", uint64(math.MaxUint64), int64(0)},
		{"unsigned signed boundary", uint64(1) << 63, int64(0)},
		{"negative signed to unsigned", int64(-1), uint64(0)},
		{"negative signed to named unsigned", int64(-1), numericUnsigned(0)},
		{"negative float to unsigned", float64(-1), uint64(0)},
		{"fractional float to unsigned", float64(1.5), uint64(0)},
		{"float signed boundary", math.Exp2(63), int64(0)},
		{"float unsigned boundary", math.Exp2(64), uint64(0)},
		{"float narrowing", float64(128), int8(0)},
		{"float unsigned narrowing", float64(256), uint8(0)},
		{"nonfinite to signed", math.Inf(1), int64(0)},
		{"NaN to unsigned", math.NaN(), uint64(0)},
		{"uintptr narrowing", ^uintptr(0), int8(0)},
		{"negative to uintptr", int64(-1), uintptr(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := reflect.TypeOf(tc.target)
			got, err := ConvertDoggoToReflectValue(atom.NewGoObject(tc.value), target)
			if err == nil || got.IsValid() {
				t.Fatalf("Convert(%T(%v), %s) = %v, %v; want invalid result and numeric conversion error", tc.value, tc.value, target, got, err)
			}
		})
	}
}

func TestConvertFloat32RejectsFiniteOverflow(t *testing.T) {
	for _, value := range []float64{math.MaxFloat64, -math.MaxFloat64} {
		for _, input := range []types.Value{atom.NewNumberFloat(value), atom.NewGoObject(value)} {
			for _, target := range []reflect.Type{reflect.TypeOf(float32(0)), reflect.TypeOf(numericFloat(0))} {
				got, err := ConvertDoggoToReflectValue(input, target)
				if err == nil || got.IsValid() {
					t.Errorf("Convert(%T(%v), %s) = %v, %v; want overflow error", input, value, target, got, err)
				}
			}
		}
	}
}

func TestConvertNumbersRejectFractionalUnsigned(t *testing.T) {
	for _, value := range []float64{1.5, -0.5, 255.5} {
		for _, input := range []types.Value{atom.NewNumberFloat(value), atom.NewGoObject(value)} {
			got, err := ConvertDoggoToReflectValue(input, reflect.TypeOf(uint64(0)))
			if err == nil || got.IsValid() {
				t.Errorf("Convert(%T(%v), uint64) = %v, %v; want fractional loss error", input, value, got, err)
			}
		}
	}
}

func TestConvertComplex64RejectsFiniteOverflow(t *testing.T) {
	for _, value := range []complex128{complex(math.MaxFloat64, 1), complex(1, -math.MaxFloat64)} {
		for _, target := range []reflect.Type{reflect.TypeOf(complex64(0)), reflect.TypeOf(numericComplex(0))} {
			got, err := ConvertDoggoToReflectValue(atom.NewGoObject(value), target)
			if err == nil || got.IsValid() {
				t.Errorf("Convert(%v, %s) = %v, %v; want overflow error", value, target, got, err)
			}
		}
	}
}

func TestConvertNumericPreservesValidValues(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  any
	}{
		{int64(127), numericCount(127)}, {uint64(255), numericUnsigned(255)},
		{uint64(math.MaxInt64), int64(math.MaxInt64)}, {int64(math.MaxInt64), uint64(math.MaxInt64)},
		{uint64(math.MaxUint64), uint64(math.MaxUint64)},
		{float64(-128), int8(-128)}, {float32(127), int8(127)},
		{float64(255), uint8(255)}, {int64(7), uintptr(7)},
		{int64(12), numericFloat(12)}, {float64(1.1), numericFloat(1.1)},
		{numericCount(12), int64(12)}, {float64(math.MaxFloat32), float32(math.MaxFloat32)},
		// Ordinary finite float rounding and native nonnumeric conversions remain allowed.
		{float64(1.1), float32(1.1)}, {int64('a'), "a"},
		{[]byte("doggo"), "doggo"}, {"doggo", []byte("doggo")},
		{complex128(1.1 + 2.2i), numericComplex(1.1 + 2.2i)}, {complex64(1 + 2i), complex128(1 + 2i)},
	} {
		t.Run(reflect.TypeOf(tc.value).String()+" to "+reflect.TypeOf(tc.want).String(), func(t *testing.T) {
			got, err := ConvertDoggoToReflectValue(atom.NewGoObject(tc.value), reflect.TypeOf(tc.want))
			if err != nil || !got.IsValid() || !reflect.DeepEqual(got.Interface(), tc.want) {
				t.Fatalf("Convert(%T(%v)) = %v, %v; want %#v", tc.value, tc.value, got, err, tc.want)
			}
		})
	}
	for _, value := range []float64{1.1, math.MaxFloat32, math.Inf(1), math.Inf(-1), math.NaN()} {
		for _, input := range []types.Value{atom.NewNumberFloat(value), atom.NewGoObject(value)} {
			got, err := ConvertDoggoToReflectValue(input, reflect.TypeOf(numericFloat(0)))
			if err != nil || (!math.IsNaN(value) && got.Float() != float64(float32(value))) || (math.IsNaN(value) && !math.IsNaN(got.Float())) {
				t.Errorf("Convert(%T(%v), numericFloat) = %v, %v", input, value, got, err)
			}
		}
	}
}

func TestConvertNumericPreservesIntegerFloatRounding(t *testing.T) {
	// One unit above a float32 midpoint exercises the existing reflection rounding.
	value := int64((1 << 62) + (1 << 38) + 1)
	for _, tc := range []struct {
		input types.Value
		want  float32
	}{
		{atom.NewGoObject(value), float32(float64(value))},
		// Ordinary Doggo numbers retain their existing float64-based float policy.
		{atom.NewNumberInt(value), float32(float64(value))},
	} {
		got, err := ConvertDoggoToReflectValue(tc.input, reflect.TypeOf(float32(0)))
		if err != nil || got.Float() != float64(tc.want) {
			t.Errorf("Convert(%T(%v), float32) = %v, %v; want %v", tc.input, value, got, err, tc.want)
		}
	}
}

func TestConvertGoObjectValueDependentConversions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   []int
		target  any
		wantErr bool
	}{
		{"short array", []int{1}, [2]int{}, true},
		{"short array pointer", []int{1}, (*[2]int)(nil), true},
		{"valid array", []int{1, 2}, [2]int{1, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("conversion panicked: %v", p)
				}
			}()
			got, err := ConvertDoggoToReflectValue(atom.NewGoObject(tc.input), reflect.TypeOf(tc.target))
			if tc.wantErr {
				if err == nil || got.IsValid() {
					t.Fatalf("Convert(%v) = %v, %v; want conversion error", tc.input, got, err)
				}
			} else if err != nil || !reflect.DeepEqual(got.Interface(), tc.target) {
				t.Fatalf("Convert(%v) = %v, %v; want %v", tc.input, got, err, tc.target)
			}
		})
	}
}
