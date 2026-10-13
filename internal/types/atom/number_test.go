package atom

import (
	"math"
	"testing"
)

func TestNumber(t *testing.T) {
	tests := []struct {
		name     string
		number   *Number
		expected string
	}{
		{"int zero", NewNumberInt(0), "0"},
		{"int positive", NewNumberInt(42), "42"},
		{"int negative", NewNumberInt(-42), "-42"},
		{"float zero", NewNumberFloat(0.0), "0"},
		{"float positive", NewNumberFloat(3.14), "3.14"},
		{"float negative", NewNumberFloat(-3.14), "-3.14"},
		{"float scientific", NewNumberFloat(1.5e10), "1.5e+10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.number.String() != tt.expected {
				t.Errorf("Number.String() = %v, want %v", tt.number.String(), tt.expected)
			}
			if tt.number.Type() != "number" {
				t.Errorf("Number.Type() = %v, want number", tt.number.Type())
			}
		})
	}
}

func TestNumberToFloat64(t *testing.T) {
	tests := []struct {
		name     string
		number   *Number
		expected float64
	}{
		{"int zero", NewNumberInt(0), 0.0},
		{"int positive", NewNumberInt(42), 42.0},
		{"int negative", NewNumberInt(-42), -42.0},
		{"float zero", NewNumberFloat(0.0), 0.0},
		{"float positive", NewNumberFloat(3.14), 3.14},
		{"float negative", NewNumberFloat(-3.14), -3.14},
		{"nil number", nil, 0.0},
		{"number with both nil", &Number{IntVal: nil, FloatVal: nil}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.number.ToFloat64()
			if result != tt.expected {
				t.Errorf("Number.ToFloat64() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNumberIsZero(t *testing.T) {
	tests := []struct {
		name     string
		number   *Number
		expected bool
	}{
		{"int zero", NewNumberInt(0), true},
		{"int positive", NewNumberInt(42), false},
		{"int negative", NewNumberInt(-42), false},
		{"float zero", NewNumberFloat(0.0), true},
		{"float positive", NewNumberFloat(3.14), false},
		{"float negative", NewNumberFloat(-3.14), false},
		{"nil number", nil, true},
		{"number with both nil", &Number{IntVal: nil, FloatVal: nil}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.number.IsZero()
			if result != tt.expected {
				t.Errorf("Number.IsZero() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNumberEqualityAndHashPreserveInt64Precision(t *testing.T) {
	left := NewNumberInt(9007199254740992)
	right := NewNumberInt(9007199254740993)
	if left.Equals(right) {
		t.Fatal("distinct int64 values must not compare equal")
	}
	if left.Hash() == right.Hash() {
		t.Fatal("adjacent large int64 values should not collapse to the same hash")
	}

	equalFloat := NewNumberFloat(9007199254740992)
	if !left.Equals(equalFloat) {
		t.Fatal("an exactly representable integer and float should compare equal")
	}
	if left.Hash() != equalFloat.Hash() {
		t.Fatal("equal integer and float values must have equal hashes")
	}
}

func TestNumberNaNIsNotEqualToItself(t *testing.T) {
	nan := NewNumberFloat(math.NaN())
	if nan.Equals(nan) {
		t.Fatal("NaN must remain unordered")
	}
}
