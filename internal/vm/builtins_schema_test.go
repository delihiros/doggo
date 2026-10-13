package vm

import (
	"strings"
	"testing"

	"doggo/internal/types/atom"
)

func mustNewSchemaRunner(t *testing.T) *Runner {
	t.Helper()
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.schema :refer :all])`); err != nil {
		t.Fatalf("require doggo.schema: %v", err)
	}
	return runner
}

func TestSchemaBuiltinsValidateDataFirstSchemas(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		[(valid? [:vector :int] [1 2 3])
		 (valid? [:vector :int] [1 "x" 3])]`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if got := mustBool(t, vec.Items[0]); !got {
		t.Fatal("expected valid? to return true")
	}
	if got := mustBool(t, vec.Items[1]); got {
		t.Fatal("expected valid? to return false")
	}
}

func TestSchemaExplainReturnsStructuredProblems(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(explain [:map [:name :string]] {:name 42})`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	view := mustMap(t, value)
	if got := mustBool(t, view.Get(atom.NewKeyword("valid?"))); got {
		t.Fatal("expected explain valid? to be false")
	}
	if view.Get(atom.NewKeyword("schema")) == nil {
		t.Fatal("expected explain schema to be present")
	}
	if view.Get(atom.NewKeyword("value")) == nil {
		t.Fatal("expected explain value to be present")
	}
	problems := mustVector(t, view.Get(atom.NewKeyword("problems")))
	if len(problems.Items) != 1 {
		t.Fatalf("unexpected explain problems length: %d", len(problems.Items))
	}
	problem := mustMap(t, problems.Items[0])
	if got := mustVector(t, problem.Get(atom.NewKeyword("path"))).String(); got != "[:name]" {
		t.Fatalf("unexpected explain path: %s", got)
	}
	if got := mustString(t, problem.Get(atom.NewKeyword("message"))); got == "" {
		t.Fatal("expected non-empty explain message")
	}
}

func TestSchemaBuiltinsRejectMalformedSchemaDefinitions(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	_, err := runner.Run(`(valid? [:or] 42)`)
	if err == nil {
		t.Fatal("expected malformed schema error")
	}
	if !strings.Contains(err.Error(), "or schema expects at least one child schema") {
		t.Fatalf("unexpected malformed schema error: %v", err)
	}
}

func TestSchemaNormalizationErrorsProduceCanonicalExData(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(require '[doggo.schema :as schema])
		(try
		  (schema/valid? [:predicate 42] 1)
		  (catch [e]
		    [(ex-message e)
		     (ex-data e)
		     (ex->map e)]))`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if got := mustString(t, vec.Items[0]); !strings.Contains(got, "expected callable") {
		t.Fatalf("unexpected schema normalization message: %s", got)
	}

	data := mustMap(t, vec.Items[1])
	if got := mustVector(t, data.Get(atom.NewKeyword("path"))).String(); got != "[:predicate]" {
		t.Fatalf("unexpected normalization path: %s", got)
	}
	if got := mustString(t, data.Get(atom.NewKeyword("expected"))); got != "callable" {
		t.Fatalf("unexpected normalization expected value: %s", got)
	}
	if got := mustInt(t, data.Get(atom.NewKeyword("actual"))); got != 42 {
		t.Fatalf("unexpected normalization actual value: %d", got)
	}
	if got := mustString(t, data.Get(atom.NewKeyword("actual_type"))); got != "number" {
		t.Fatalf("unexpected normalization actual type: %s", got)
	}

	exception := mustMap(t, vec.Items[2])
	exceptionData := mustMap(t, exception.Get(atom.NewKeyword("data")))
	if got := mustString(t, exceptionData.Get(atom.NewKeyword("expected"))); got != "callable" {
		t.Fatalf("unexpected exception expected value: %s", got)
	}
	if cause := exception.Get(atom.NewKeyword("cause")); cause == nil || cause == atom.NilValue {
		t.Fatal("expected normalized schema cause")
	}
	trace := mustVector(t, exception.Get(atom.NewKeyword("trace")))
	if len(trace.Items) == 0 {
		t.Fatal("expected normalized schema trace")
	}
}

func mustSchemaBuiltin(t *testing.T, name string) *Builtin {
	t.Helper()
	for _, def := range builtinsSchema {
		if def.Name == name {
			return def.Builtin
		}
	}
	t.Fatalf("missing schema builtin %q", name)
	return nil
}
