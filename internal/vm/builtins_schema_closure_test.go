package vm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"doggo/internal/schema"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func TestSchemaPredicatesAcceptVMClosures(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "anonymous closure validates valid and invalid data",
			source: `(let [s [:predicate (fn [x] (and (int? x) (> x 0)))]] [(valid? s 2) (valid? s -1) (valid? s "x")])`,
			want:   `[true false false]`,
		},
		{
			name:   "captured variables remain available",
			source: `(let [limit 3 s [:predicate (fn [x] (> x limit))]] [(valid? s 4) (valid? s 3)])`,
			want:   `[true false]`,
		},
		{
			name: "escaped predicate closure keeps transitive captures",
			source: `(let [s (let [limit 3]
			                  ((fn [] [:predicate (fn [x] (> x limit))])))]
			  [(valid? s 4) (valid? s 3)])`,
			want: `[true false]`,
		},
		{
			name: "nested schema preserves original schema and problem path",
			source: `(let [limit 3 predicate (fn [x] (> x limit))
			              s [:map [:values [:vector [:predicate predicate]]]]
			              good (explain s {:values [4 5]})
			              bad (explain s {:values [4 2]})]
			  [(:valid? good) (:valid? bad) (= s (:schema bad))
			   (= predicate (:predicate (first (:problems bad))))
			   (:path (first (:problems bad)))
			   (:message (first (:problems bad)))])`,
			want: `[true false true true [:values 1] "predicate returned false"]`,
		},
		{
			name: "predicate receives current dynamic bindings",
			source: `(def ^:dynamic *limit* 10)
			(let [s [:predicate (fn [x] (> x *limit*))]]
			  [(valid? s 5)
			   (binding [*limit* 3] (valid? s 5))
			   (valid? s 5)])`,
			want: `[false true false]`,
		},
		{
			name:   "list schema accepts a closure predicate",
			source: `(let [s (list :predicate (fn [x] (int? x)))] [(valid? s 1) (valid? s "x")])`,
			want:   `[true false]`,
		},
		{
			name: "mutually recursive letfn predicate",
			source: `(letfn [(even [n] (if (zero? n) true (odd (dec n))))
			                (odd [n] (if (zero? n) false (even (dec n))))]
			  [(valid? [:predicate even] 4) (valid? [:predicate even] 3)])`,
			want: `[true false]`,
		},
		{
			name: "instrument contracts accept closure predicates",
			source: `(let [limit 3 s [:predicate (fn [x] (> x limit))]
			              f (instrument (fn [x] x) {:args [:tuple s] :ret s})]
			  [(f 4) (try (f 2) (catch [e] (:reason (ex-data e))))])`,
			want: `[4 "instrumented args do not satisfy contract"]`,
		},
		{
			name: "check generation uses closure predicate",
			source: `(let [limit 3 s [:predicate (fn [x] (> x limit))]
			              report (check (fn [x] (> x limit)) s
			                            {:iterations 2 :seed 7
			                             :generator-overrides {:predicate (fn [] 4)}})]
			  [(:pass? report) (:iterations report)])`,
			want: `[true 2]`,
		},
		{
			name:   "builtin and partial predicate controls",
			source: `[(valid? [:predicate int?] 1) (valid? [:predicate (partial > 3)] 2)]`,
			want:   `[true true]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, mustNewSchemaRunner(t), tt.source, tt.want)
		})
	}
}

func TestSchemaPredicateCallableSurvivesBorrowedVM(t *testing.T) {
	runner := mustNewSchemaRunner(t)
	assertRunResult(t, runner, `
		(let [f (apply (fn []
		          (let [limit 3]
		            (instrument inc {:args [:tuple [:predicate (fn [x] (> x limit))]]
		                             :ret :int}))) [])]
		  (f 4))`, `5`)
}

func TestSchemaPredicateCallableSurvivesRunnerRuns(t *testing.T) {
	runner := mustNewSchemaRunner(t)
	if _, err := runner.Run(`
		(def ^:dynamic *predicate-limit* 10)
		(def saved-instrumented
		  (apply (fn []
		           (let [offset 1]
		             (instrument (fn [x] (+ x offset))
		                         {:args [:tuple [:predicate (fn [x] (> x *predicate-limit*))]]
		                          :ret :int}))) []))`); err != nil {
		t.Fatalf("define escaped instrumentation: %v", err)
	}
	assertRunResult(t, runner, `
		[(binding [*predicate-limit* 3] (saved-instrumented 4))
		 (try (saved-instrumented 4)
		      (catch [e] (:reason (ex-data e))))]`,
		`[5 "instrumented args do not satisfy contract"]`)
}

func TestSchemaPredicateCallableHonorsCancellationAfterBorrowedVM(t *testing.T) {
	runner := mustNewSchemaRunner(t)
	wrapped, err := runner.Run(`
		(require '[doggo.time :as time])
		(apply (fn []
		         (instrument inc {:args [:tuple [:predicate (fn [x] (time/sleep 0) (> x 3))]]
		                          :ret :int})) [])`)
	if err != nil {
		t.Fatalf("create escaped instrumentation: %v", err)
	}
	callable, ok := wrapped.(types.Fn)
	if !ok {
		t.Fatalf("instrument returned %T, want types.Fn", wrapped)
	}
	if value, err := callable.Invoke(context.Background(), atom.NewNumberInt(4)); err != nil || value.String() != "5" {
		t.Fatalf("invoke escaped instrumentation = %v, %v; want 5", value, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if value, err := callable.Invoke(ctx, atom.NewNumberInt(4)); err == nil {
		t.Fatalf("invoke with cancelled context = %v, want error", value)
	} else {
		var instrumentErr *schema.InstrumentError
		if !errors.As(err, &instrumentErr) || len(instrumentErr.Problems) != 1 ||
			!strings.Contains(instrumentErr.Problems[0].Message, context.Canceled.Error()) {
			t.Fatalf("cancellation did not reach predicate: %v", err)
		}
	}
}
