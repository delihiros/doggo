package vm

import (
	"errors"
	"strings"
	"testing"

	"doggo/internal/compiler"
)

func TestCatchFinallyControlFlow(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "throwing catch runs finally once before outer catch",
			source: `(let [order (atom [])]
			  (try
			    (try
			      (throw (ex-info "body" {}))
			      (catch [e]
			        (swap! order conj :catch)
			        (throw (ex-info "catch" {})))
			      (finally (swap! order conj :finally)))
			    (catch [e] (swap! order conj :outer)))
			  @order)`,
			want: "[:catch :finally :outer]",
		},
		{
			name: "catch result survives finally",
			source: `(let [count (atom 0)
			               result (try
			                        (throw (ex-info "body" {}))
			                        (catch [e] :first [:catch (ex-message e)])
			                        (finally (swap! count inc) :ignored))]
			  [result @count])`,
			want: `[[:catch "body"] 1]`,
		},
		{
			name: "nested catch inside catch leaves finally guard active",
			source: `(let [order (atom [])]
			  (try
			    (try
			      (throw (ex-info "body" {}))
			      (catch [e]
			        (try
			          (throw (ex-info "nested" {}))
			          (catch [nested] (swap! order conj :nested)))
			        (throw (ex-info "replacement" {})))
			      (finally (swap! order conj :finally)))
			    (catch [e] (swap! order conj (ex-message e))))
			  @order)`,
			want: `[:nested :finally "replacement"]`,
		},
		{
			name: "finally throw replaces catch throw exactly once",
			source: `(let [count (atom 0)
			               result (try
			                        (try
			                          (throw (ex-info "body" {}))
			                          (catch [e] (throw (ex-info "catch" {})))
			                          (finally
			                            (swap! count inc)
			                            (throw (ex-info "finally" {}))))
			                        (catch [e] (ex-message e)))]
			  [result @count])`,
			want: `["finally" 1]`,
		},
		{
			name: "finally throw replaces successful catch exactly once",
			source: `(let [count (atom 0)
			               result (try
			                        (try
			                          (throw (ex-info "body" {}))
			                          (catch [e] :handled)
			                          (finally
			                            (swap! count inc)
			                            (throw (ex-info "finally" {}))))
			                        (catch [e] (ex-message e)))]
			  [result @count])`,
			want: `["finally" 1]`,
		},
		{
			name: "throwing catch in function unwinds inner and outer finally",
			source: `(let [order (atom [])
			               fail (fn []
			                      (try
			                        (throw (ex-info "body" {}))
			                        (catch [e] (throw (ex-info "catch" {})))
			                        (finally (swap! order conj :inner))))]
			  (try
			    (try (fail) (finally (swap! order conj :middle)))
			    (catch [e] (swap! order conj :outer)))
			  @order)`,
			want: "[:inner :middle :outer]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, MustNewRunner(), tt.source, tt.want)
		})
	}
}

func TestCatchFinallyPreservesThrownExceptionIdentity(t *testing.T) {
	runner := MustNewRunner()
	want, err := runner.Run(`(def replacement (ex-info "replacement" {:kind :replacement})) replacement`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := runner.Run(`(def finally-count (atom 0))
	  (try
	    (try
	      (throw (ex-info "body" {}))
	      (catch [e] (throw replacement))
	      (finally (swap! finally-count inc)))
	    (catch [e] e))`)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("caught exception = %p, want original %p", got, want)
	}
	assertRunResult(t, runner, `@finally-count`, "1")
}

func TestCatchFinallyPropagatesRuntimeError(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `(def finally-count (atom 0)) @finally-count`, "0")
	_, err := runner.Run(`(try
	  (throw (ex-info "body" {}))
	  (catch [e] ((fn [] (/ 1 0))))
	  (finally (swap! finally-count inc)))`)
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("error = %v, want division by zero", err)
	}
	if _, ok := err.(*RuntimeError); !ok {
		t.Fatalf("error type = %T, want *RuntimeError", err)
	}
	assertRunResult(t, runner, `@finally-count`, "1")
}

func TestCatchFinallyRejectsEscapingRecur(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "escaping catch cannot leave a stale guard for a later exception",
			source: `(let [order (atom [])]
			  (try
			    (do
			      (loop [n 0]
			        (try
			          (if (= n 0) (throw (ex-info "body" {})) :done)
			          (catch [e] (recur 1))
			          (finally (swap! order conj :finally))))
			      (swap! order conj :after)
			      (throw (ex-info "after" {})))
			    (catch [e] (swap! order conj :outer)))
			  @order)`,
		},
		{
			name: "nested try in catch cannot recur to the enclosing loop",
			source: `(loop [n 0]
			  (try
			    (throw (ex-info "body" {}))
			    (catch [e] (try (recur 1) (catch [nested] :handled)))
			    (finally :cleanup)))`,
		},
		{
			name: "nested catch cannot recur across its own finally guard",
			source: `(try
			  (throw (ex-info "outer" {}))
			  (catch [outer]
			    (loop [n 0]
			      (try
			        (throw (ex-info "inner" {}))
			        (catch [inner] (recur 1))
			        (finally :inner-cleanup))))
			  (finally :outer-cleanup))`,
		},
		{
			name: "nested catch without finally still respects outer catch guard",
			source: `(loop [n 0]
			  (try
			    (throw (ex-info "outer" {}))
			    (catch [outer]
			      (try
			        (throw (ex-info "inner" {}))
			        (catch [inner] (recur 1))))
			    (finally :outer-cleanup)))`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MustNewRunner().Compile(tt.source)
			var compileErr *compiler.CompileError
			if !errors.As(err, &compileErr) {
				t.Fatalf("compile error = %v (%T), want *compiler.CompileError", err, err)
			}
			if compileErr.Message != "recur cannot exit a catch with finally" {
				t.Fatalf("compile message = %q, want recur boundary error", compileErr.Message)
			}
		})
	}
}

func TestCatchFinallyAllowsContainedRecur(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "loop inside catch completes before finally and later exception",
			source: `(let [order (atom [])]
			  (try
			    (do
			      (try
			        (throw (ex-info "body" {}))
			        (catch [e] (loop [n 0] (if (< n 2) (recur (inc n)) n)))
			        (finally (swap! order conj :finally)))
			      (swap! order conj :after)
			      (throw (ex-info "after" {})))
			    (catch [e] (swap! order conj :outer)))
			  @order)`,
			want: "[:finally :after :outer]",
		},
		{
			name: "nested function has its own loop scope",
			source: `(try
			  (throw (ex-info "body" {}))
			  (catch [e] ((fn [] (loop [n 0] (if (< n 2) (recur (inc n)) n)))))
			  (finally :cleanup))`,
			want: "2",
		},
		{
			name: "catch without finally can recur to enclosing loop",
			source: `(loop [n 0]
			  (try
			    (if (= n 0) (throw (ex-info "body" {})) n)
			    (catch [e] (recur 1))))`,
			want: "1",
		},
		{
			name: "completed catch restores enclosing loop recur",
			source: `(let [count (atom 0)]
			  (loop [n 0]
			    (try
			      (throw (ex-info "body" {}))
			      (catch [e] ((fn [] (loop [inner 0] (if (< inner 1) (recur 1) inner)))))
			      (finally (swap! count inc)))
			    (if (< n 2) (recur (inc n)) @count)))`,
			want: "3",
		},
		{
			name: "loop inside nested catch stays within both finally guards",
			source: `(try
			  (throw (ex-info "outer" {}))
			  (catch [outer]
			    (try
			      (throw (ex-info "inner" {}))
			      (catch [inner] (loop [n 0] (if (< n 2) (recur (inc n)) n)))
			      (finally :inner-cleanup)))
			  (finally :outer-cleanup))`,
			want: "2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, MustNewRunner(), tt.source, tt.want)
		})
	}
}
