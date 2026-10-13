package vm

import (
	"strings"
	"testing"
)

func TestExceptionHandling(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "normal result",
			source: `(try 1 (catch [e] 2))`,
			want:   "1",
		},
		{
			name:   "catch canonical error",
			source: `(try (throw (ex-info "boom" {:kind :boom})) (catch [e] (ex-message e)))`,
			want:   `"boom"`,
		},
		{
			name: "nested catch",
			source: `
				(try
				  (try
				    (throw (ex-info "inner" {:kind :inner}))
				    (catch [e] :caught-inner))
				  (catch [e] :caught-outer))`,
			want: ":caught-inner",
		},
		{
			name: "rethrow",
			source: `
				(try
				  (try
				    (throw (ex-info "inner" {:kind :inner}))
				    (catch [e] (throw e)))
				  (catch [e] (:kind (ex-data e))))`,
			want: ":inner",
		},
		{
			name:   "catch evaluates every body form",
			source: `(try (throw (ex-info "boom" {})) (catch [e] :first :last))`,
			want:   ":last",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			got, err := runner.Run(tt.source)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("result = %s, want %s", got.String(), tt.want)
			}
		})
	}
}

func TestUncaughtException(t *testing.T) {
	runner := MustNewRunner()
	_, err := runner.Run(`(throw (ex-info "oops" {:kind :boom}))`)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*RuntimeError); !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if !strings.Contains(err.Error(), "oops") {
		t.Fatalf("expected error message, got %v", err)
	}
}

func TestStackTraceWithFunctionNames(t *testing.T) {
	t.Run("named functions", func(t *testing.T) {
		runner := MustNewRunner()
		_, err := runner.Run(`
			(defn bar [x] (throw (ex-info "error in bar" {:kind :test})))
			(defn foo [] (bar 1))
			(foo)
		`)
		if err == nil {
			t.Fatal("expected error")
		}
		for _, name := range []string{"bar", "foo"} {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("expected stack trace to contain %q: %v", name, err)
			}
		}
	})

	t.Run("nested functions", func(t *testing.T) {
		runner := MustNewRunner()
		_, err := runner.Run(`
			(defn inner [] (/ 1 0))
			(defn middle [] (inner))
			(defn outer [] (middle))
			(outer)
		`)
		if err == nil {
			t.Fatal("expected error")
		}
		for _, name := range []string{"inner", "middle", "outer"} {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("expected stack trace to contain %q: %v", name, err)
			}
		}
	})
}

func TestCatchBranchesOnCanonicalErrorData(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(try
		  (throw (ex-info "bad input" {:kind :invalid-argument :argument :value}))
		  (catch [e]
		    (if (= :invalid-argument (:kind (ex-data e)))
		      [:handled (ex-message e)]
		      (throw e))))
	`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := result.String(); got != `[:handled "bad input"]` {
		t.Fatalf("unexpected result: %s", got)
	}
}

func TestTryFinally(t *testing.T) {
	t.Run("normal exit", func(t *testing.T) {
		runner := MustNewRunner()
		result, err := runner.Run(`
			(do
			  (def x (atom 0))
			  (try
			    (reset! x 1)
			    (catch [e] nil)
			    (finally (reset! x 2)))
			  @x)`)
		if err != nil {
			t.Fatal(err)
		}
		if result.String() != "2" {
			t.Fatalf("result = %s, want 2", result.String())
		}
	})

	t.Run("caught error", func(t *testing.T) {
		runner := MustNewRunner()
		result, err := runner.Run(`
			(do
			  (def order (atom []))
			  (try
			    (try
			      (swap! order conj :inner-try)
			      (throw (ex-info "inner" {:kind :inner}))
			      (catch [e] (swap! order conj :inner-catch))
			      (finally (swap! order conj :inner-finally)))
			    (catch [e] (swap! order conj :outer-catch))
			    (finally (swap! order conj :outer-finally)))
			  @order)`)
		if err != nil {
			t.Fatal(err)
		}
		want := "[:inner-try :inner-catch :inner-finally :outer-finally]"
		if result.String() != want {
			t.Fatalf("result = %s, want %s", result.String(), want)
		}
	})

	t.Run("propagated error", func(t *testing.T) {
		runner := MustNewRunner()
		if _, err := runner.Run(`(def state (atom 0))`); err != nil {
			t.Fatal(err)
		}
		_, err := runner.Run(`
			(try
			  (throw (ex-info "propagated" {:kind :test}))
			  (finally (reset! state 9)))`)
		if err == nil {
			t.Fatal("expected propagated error")
		}
		state, stateErr := runner.Run(`@state`)
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		if state.String() != "9" {
			t.Fatalf("state = %s, want 9", state.String())
		}
	})

	t.Run("finally value is ignored", func(t *testing.T) {
		runner := MustNewRunner()
		result, err := runner.Run(`(try 42 (finally 999))`)
		if err != nil {
			t.Fatal(err)
		}
		if result.String() != "42" {
			t.Fatalf("result = %s, want 42", result.String())
		}
	})
}
