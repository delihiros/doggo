package vm

import "testing"

func TestRandIntBuiltinReturnsIntegerInRange(t *testing.T) {
	runner := MustNewRunner()

	value, err := runner.Run(`(let [x (rand-int 10)] (and (int? x) (<= 0 x) (< x 10)))`)
	if err != nil {
		t.Fatalf("rand-int should execute: %v", err)
	}
	if got := value.String(); got != "true" {
		t.Fatalf("rand-int should return an integer in range: %s", got)
	}
}

func TestRandBuiltinReturnsFloatInScaledRange(t *testing.T) {
	runner := MustNewRunner()

	for _, bound := range []string{"10", "2.5"} {
		value, err := runner.Run(`(let [x (rand ` + bound + `)]
			(and (float? x) (<= 0.0 x) (< x ` + bound + `)))`)
		if err != nil {
			t.Fatalf("rand should execute for bound %s: %v", bound, err)
		}
		if got := value.String(); got != "true" {
			t.Fatalf("rand should return a float below %s: %s", bound, got)
		}
	}
}

func TestRandIntBuiltinRejectsNonPositiveBounds(t *testing.T) {
	runner := MustNewRunner()

	for _, expr := range []string{`(rand-int 0)`, `(rand-int -1)`} {
		if _, err := runner.Run(expr); err == nil {
			t.Fatalf("expected error for %s", expr)
		}
	}
}
