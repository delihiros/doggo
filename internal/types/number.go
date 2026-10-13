package types

import (
	"math"
	"math/big"
)

// Int64Value returns n without precision loss when it represents an integer.
func Int64Value(n Number) (int64, bool) {
	valuer, ok := n.(Int64Valuer)
	if !ok {
		return 0, false
	}
	return valuer.Int64Value()
}

// TruncateInt64Value converts finite, in-range numbers to int64 toward zero.
func TruncateInt64Value(n Number) (int64, bool) {
	if integer, ok := Int64Value(n); ok {
		return integer, true
	}

	value := n.ToFloat64()
	if math.IsNaN(value) || value < -0x1p63 || value >= 0x1p63 {
		return 0, false
	}
	return int64(value), true
}

// CompareNumbers compares two numbers. ordered is false when either value is NaN.
func CompareNumbers(left, right Number) (cmp int, ordered bool) {
	leftInt, leftIsInt := Int64Value(left)
	rightInt, rightIsInt := Int64Value(right)
	if leftIsInt && rightIsInt {
		switch {
		case leftInt < rightInt:
			return -1, true
		case leftInt > rightInt:
			return 1, true
		default:
			return 0, true
		}
	}

	leftFloat := left.ToFloat64()
	rightFloat := right.ToFloat64()
	if math.IsNaN(leftFloat) || math.IsNaN(rightFloat) {
		return 0, false
	}
	if leftIsInt {
		return compareIntAndFloat(leftInt, rightFloat), true
	}
	if rightIsInt {
		return -compareIntAndFloat(rightInt, leftFloat), true
	}
	switch {
	case leftFloat < rightFloat:
		return -1, true
	case leftFloat > rightFloat:
		return 1, true
	default:
		return 0, true
	}
}

// CompareValues orders numbers numerically and other values by their printed form.
func CompareValues(left, right Value) (cmp int, ordered bool) {
	leftNumber, leftIsNumber := left.(Number)
	rightNumber, rightIsNumber := right.(Number)
	if leftIsNumber && rightIsNumber {
		return CompareNumbers(leftNumber, rightNumber)
	}

	switch leftString, rightString := left.String(), right.String(); {
	case leftString < rightString:
		return -1, true
	case leftString > rightString:
		return 1, true
	default:
		return 0, true
	}
}

func compareIntAndFloat(integer int64, floating float64) int {
	if math.IsInf(floating, 1) {
		return -1
	}
	if math.IsInf(floating, -1) {
		return 1
	}
	return new(big.Float).SetInt64(integer).Cmp(new(big.Float).SetFloat64(floating))
}

// HashNumber returns the same hash for numerically equal integer and float values.
func HashNumber(n Number) uint32 {
	if integer, ok := Int64Value(n); ok {
		return hashNumberBits(uint64(integer))
	}

	floating := n.ToFloat64()
	if floating >= -0x1p63 && floating < 0x1p63 && floating == math.Trunc(floating) {
		return hashNumberBits(uint64(int64(floating)))
	}
	return hashNumberBits(math.Float64bits(floating))
}

// CheckedAddInt64 returns false when the exact sum does not fit in int64.
func CheckedAddInt64(left, right int64) (int64, bool) {
	result := left + right
	if (right > 0 && result < left) || (right < 0 && result > left) {
		return 0, false
	}
	return result, true
}

// CheckedSubInt64 returns false when the exact difference does not fit in int64.
func CheckedSubInt64(left, right int64) (int64, bool) {
	result := left - right
	if (right > 0 && result > left) || (right < 0 && result < left) {
		return 0, false
	}
	return result, true
}

// CheckedMulInt64 returns false when the exact product does not fit in int64.
func CheckedMulInt64(left, right int64) (int64, bool) {
	if left == 0 || right == 0 {
		return 0, true
	}
	if (left == -1<<63 && right == -1) || (right == -1<<63 && left == -1) {
		return 0, false
	}
	result := left * right
	if result/right != left {
		return 0, false
	}
	return result, true
}

// CheckedNegInt64 returns false when negation does not fit in int64.
func CheckedNegInt64(value int64) (int64, bool) {
	if value == -1<<63 {
		return 0, false
	}
	return -value, true
}

func hashNumberBits(value uint64) uint32 {
	value ^= value >> 33
	value *= 0xff51afd7ed558ccd
	value ^= value >> 33
	value *= 0xc4ceb9fe1a85ec53
	value ^= value >> 33
	return uint32(value) ^ uint32(value>>32)
}

// ModInt64 returns a modulo result with the same sign as the divisor.
func ModInt64(left, right int64) (int64, bool) {
	if right == 0 {
		return 0, false
	}
	result := left % right
	if (result > 0 && right < 0) || (result < 0 && right > 0) {
		result += right
	}
	return result, true
}
