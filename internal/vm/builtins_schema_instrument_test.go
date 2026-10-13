package vm

import (
	"testing"

	"doggo/internal/types/atom"
)

func TestSchemaInstrumentBuiltinWrapsDirectCallable(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(let [f (instrument (fn [x] x) {:args [:tuple :int] :ret :int})
		      failure (try
		                (f "x")
		                (catch [e]
		                  (let [d (ex-data e)]
		                    (:reason d))))]
		  [(f 7) failure])`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if got := mustInt(t, vec.Items[0]); got != 7 {
		t.Fatalf("unexpected wrapped direct callable result: %d", got)
	}
	if got := mustString(t, vec.Items[1]); got != "instrumented args do not satisfy contract" {
		t.Fatalf("unexpected direct instrumentation failure reason: %s", got)
	}
}

func TestSchemaInstrumentBuiltinCurrentNamespaceNamedTargetConvenience(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(do
		  (defn add1 [x] (+ x 1))
		  (instrument 'add1 {:args [:tuple :int] :ret :int})
		  (let [failure (try
		                  (add1 "x")
		                  (catch [e]
		                    (let [d (ex-data e)]
		                      (:reason d))))]
		    [(add1 1)
		     failure
		     (do (unstrument 'add1) (add1 1))]))`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if got := mustInt(t, vec.Items[0]); got != 2 {
		t.Fatalf("unexpected instrumented named target result: %d", got)
	}
	if got := mustString(t, vec.Items[1]); got != "instrumented args do not satisfy contract" {
		t.Fatalf("unexpected named instrumentation failure reason: %s", got)
	}
	if got := mustInt(t, vec.Items[2]); got != 2 {
		t.Fatalf("unexpected unstrumented named target result: %d", got)
	}
}

func TestSchemaInstrumentBuiltinRejectsOutOfScopeNamedTargets(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	t.Run("qualified", func(t *testing.T) {
		value, err := runner.Run(`
			(try
			  (instrument 'doggo.schema/valid? {:args [:tuple :any :any] :ret :bool})
			  (catch [e] (ex-data e)))`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		data := mustMap(t, value)
		if got := mustString(t, data.Get(atom.NewKeyword("reason"))); got != "named target must be an unqualified current-namespace symbol" {
			t.Fatalf("unexpected qualified target error reason: %s", got)
		}
	})

	t.Run("builtin", func(t *testing.T) {
		value, err := runner.Run(`
			(try
			  (instrument 'count {:args [:tuple [:vector :int]] :ret :int})
			  (catch [e] (ex-data e)))`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		data := mustMap(t, value)
		if got := mustString(t, data.Get(atom.NewKeyword("reason"))); got != "named target must resolve to a current-namespace global binding" {
			t.Fatalf("unexpected builtin target error reason: %s", got)
		}
	})
}
