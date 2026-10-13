package vm

import (
	"strings"
	"testing"
)

// TestDefmultiAndDefmethod_BasicDispatch tests basic multimethod dispatch
func TestDefmultiAndDefmethod_BasicDispatch(t *testing.T) {
	tests := []struct {
		name     string
		setup    string
		call     string
		expected string
	}{
		{
			name: "dispatch on keyword from map",
			setup: `
				(defmulti area (fn [shape] (:type shape)))
				(defmethod area :circle [s] (* 3.14 (:r s) (:r s)))
				(defmethod area :rectangle [s] (* (:w s) (:h s)))
			`,
			call:     `(area {:type :circle :r 2})`,
			expected: "12.56",
		},
		{
			name: "dispatch on custom class function",
			setup: `
				(defmulti process (fn [x] (:class x)))
				(defmethod process :text [x] (str "text: " (:value x)))
				(defmethod process :num [x] (str "num: " (:value x)))
			`,
			call:     `(process {:class :text :value "hello"})`,
			expected: `"text: hello"`,
		},
		{
			name: "dispatch on first element",
			setup: `
				(defmulti handle first)
				(defmethod handle :add [cmd] (+ (nth cmd 1) (nth cmd 2)))
				(defmethod handle :mul [cmd] (* (nth cmd 1) (nth cmd 2)))
			`,
			call:     `(handle [:add 3 5])`,
			expected: "8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()

			// Setup multimethod
			_, err := runner.Run(tt.setup)
			if err != nil {
				t.Fatalf("setup error: %v", err)
			}

			// Call and check result
			result, err := runner.Run(tt.call)
			if err != nil {
				t.Fatalf("call error: %v", err)
			}

			if result.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}

// TestDefmethod_Override tests that later defmethod overrides earlier one
func TestDefmethod_Override(t *testing.T) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(defmulti greet (fn [x] (:lang x)))
		(defmethod greet :en [x] "Hello")
		(defmethod greet :en [x] "Hi")
	`)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	result, err := runner.Run(`(greet {:lang :en})`)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}

	// 後から登録した "Hi" が使われる
	if result.String() != `"Hi"` {
		t.Errorf("expected \"Hi\", got %s", result.String())
	}
}

// TestDefmethod_Default tests :default dispatch value
func TestDefmethod_Default(t *testing.T) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(defmulti describe (fn [x] (:category x)))
		(defmethod describe :animal [x] "It's an animal")
		(defmethod describe :default [x] "Unknown category")
	`)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	// 登録済みの dispatch value
	result1, err := runner.Run(`(describe {:category :animal})`)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	if result1.String() != `"It's an animal"` {
		t.Errorf("expected \"It's an animal\", got %s", result1.String())
	}

	// 未登録の dispatch value -> :default が使われる
	result2, err := runner.Run(`(describe {:category :plant})`)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	if result2.String() != `"Unknown category"` {
		t.Errorf("expected \"Unknown category\", got %s", result2.String())
	}
}

// TestDefmethod_NoMatch_Error tests error when no matching method
func TestDefmethod_NoMatch_Error(t *testing.T) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(defmulti transform (fn [x] (:op x)))
		(defmethod transform :inc [x] (+ (:val x) 1))
	`)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	// 未登録の dispatch value で :default もない場合はエラー
	_, err = runner.Run(`(transform {:op :dec :val 5})`)
	if err == nil {
		t.Fatal("expected error for unmatched dispatch value, got nil")
	}

	// エラーメッセージに dispatch value が含まれることを確認
	if !strings.Contains(err.Error(), ":dec") {
		t.Errorf("error message should contain dispatch value ':dec', got: %v", err)
	}
}

// TestDefmethod_ArityAndArgsForwarding tests that args are correctly passed to dispatch and method
func TestDefmethod_ArityAndArgsForwarding(t *testing.T) {
	tests := []struct {
		name     string
		setup    string
		call     string
		expected string
	}{
		{
			name: "single arg",
			setup: `
				(defmulti single-arg (fn [x] :num))
				(defmethod single-arg :num [x] (* x 2))
			`,
			call:     `(single-arg 21)`,
			expected: "42",
		},
		{
			name: "multiple args - dispatch uses first",
			setup: `
				(defmulti multi-arg (fn [op a b] op))
				(defmethod multi-arg :add [op a b] (+ a b))
				(defmethod multi-arg :sub [op a b] (- a b))
			`,
			call:     `(multi-arg :add 10 3)`,
			expected: "13",
		},
		{
			name: "variadic args",
			setup: `
				(defmulti var-arg (fn [op & nums] op))
				(defmethod var-arg :sum [op & nums] (apply + nums))
			`,
			call:     `(var-arg :sum 1 2 3 4 5)`,
			expected: "15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()

			_, err := runner.Run(tt.setup)
			if err != nil {
				t.Fatalf("setup error: %v", err)
			}

			result, err := runner.Run(tt.call)
			if err != nil {
				t.Fatalf("call error: %v", err)
			}

			if result.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}

// TestMultimethod_IsCallable tests that multimethod can be used as a function value
func TestMultimethod_IsCallable(t *testing.T) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(defmulti double-it (fn [x] :num))
		(defmethod double-it :num [x] (* x 2))
	`)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	// map で multimethod を使う
	result, err := runner.Run(`(map double-it [1 2 3])`)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}

	if result.String() != "(2 4 6)" {
		t.Errorf("expected (2 4 6), got %s", result.String())
	}
}

// TestMultimethod_Type tests that multimethod has correct type
func TestMultimethod_Type(t *testing.T) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(defmulti my-multi (fn [x] :default))
	`)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	// multimethod? predicate で確認
	result, err := runner.Run(`(multimethod? my-multi)`)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}

	if result.String() != `true` {
		t.Errorf("expected true, got %s", result.String())
	}

	// 非 multimethod の確認
	result2, err := runner.Run(`(multimethod? 42)`)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}

	if result2.String() != `false` {
		t.Errorf("expected false, got %s", result2.String())
	}
}
