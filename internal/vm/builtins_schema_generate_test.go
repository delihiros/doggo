package vm

import (
	"fmt"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestSchemaGenerateBuiltinUsesSeededGeneration(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(let [value (generate [:vector :int] {:seed 42 :size 3})]
		  [(valid? [:vector :int] value)
		   value
		   (generate [:vector :int] {:seed 42 :size 3})])`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if got := mustBool(t, vec.Items[0]); !got {
		t.Fatal("expected generated vector to validate")
	}
	first := mustVector(t, vec.Items[1])
	second := mustVector(t, vec.Items[2])
	if !first.Equals(second) {
		t.Fatalf("expected deterministic generation, got %s and %s", first, second)
	}
	if got, want := len(first.Items), 3; got != want {
		t.Fatalf("unexpected generated vector length: %d", got)
	}
}

func TestSchemaSampleBuiltinReturnsGeneratedValues(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`(sample [:enum :a :b] 3 {:seed 7})`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if got, want := len(vec.Items), 3; got != want {
		t.Fatalf("unexpected sample length: %d", got)
	}
	for i, item := range vec.Items {
		got := mustKeywordString(t, item)
		if got != "a" && got != "b" {
			t.Fatalf("sample item %d = %s, want :a or :b", i, got)
		}
	}
}

func TestSchemaGenerateBuiltinSupportsLiteralAndCallableOverrides(t *testing.T) {
	t.Run("literal", func(t *testing.T) {
		runner := mustNewSchemaRunner(t)

		value, err := runner.Run(`
			(generate [:predicate identity]
			          {:generator-overrides {:predicate :from-literal}})`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		if got := mustKeywordString(t, value); got != "from-literal" {
			t.Fatalf("unexpected literal override result: %s", got)
		}
	})

	t.Run("callable", func(t *testing.T) {
		runner := mustNewSchemaRunner(t)

		value, err := runner.Run(`
			(generate [:predicate identity]
			          {:generator-overrides {:predicate (fn [] :from-fn)}})`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		if got := mustKeywordString(t, value); got != "from-fn" {
			t.Fatalf("unexpected callable override result: %s", got)
		}
	})
}

func TestSchemaGenerateBuiltinReturnsCanonicalExDataForNonGeneratableKinds(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		wantKind string
	}{
		{name: "predicate", expr: `(generate [:predicate identity] {:seed 1})`, wantKind: "predicate"},
		{name: "and", expr: `(generate [:and :int :number] {:seed 1})`, wantKind: "and"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := runSchemaGenerateExceptionData(t, tt.expr)
			assertSchemaGenerateExceptionData(t, data, tt.wantKind)
			if got := mustString(t, data.Get(atom.NewKeyword("reason"))); got != "non-generatable schema kind" {
				t.Fatalf("unexpected generate reason: %s", got)
			}
		})
	}
}

func TestSchemaGenerateBuiltinReturnsCanonicalExDataForMalformedOptions(t *testing.T) {
	tests := []struct {
		name            string
		expr            string
		wantSchemaKind  string
		wantOptionPath  string
		wantOptionValue func(*testing.T, types.Value)
	}{
		{
			name:           "seed",
			expr:           `(generate [:vector :int] {:seed "x"})`,
			wantSchemaKind: "vector",
			wantOptionPath: "[:seed]",
			wantOptionValue: func(t *testing.T, v types.Value) {
				if got := mustString(t, v); got != "x" {
					t.Fatalf("unexpected seed option value: %s", got)
				}
			},
		},
		{
			name:           "size",
			expr:           `(sample [:enum :a :b] 3 {:size "x"})`,
			wantSchemaKind: "enum",
			wantOptionPath: "[:size]",
			wantOptionValue: func(t *testing.T, v types.Value) {
				if got := mustString(t, v); got != "x" {
					t.Fatalf("unexpected size option value: %s", got)
				}
			},
		},
		{
			name:           "generator_overrides",
			expr:           `(generate [:vector :int] {:generator-overrides 1})`,
			wantSchemaKind: "vector",
			wantOptionPath: "[:generator-overrides]",
			wantOptionValue: func(t *testing.T, v types.Value) {
				if got := mustInt(t, v); got != 1 {
					t.Fatalf("unexpected generator-overrides value: %d", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := runSchemaGenerateExceptionData(t, tt.expr)
			assertSchemaGenerateExceptionData(t, data, tt.wantSchemaKind)
			if got := mustVector(t, data.Get(atom.NewKeyword("option_path"))).String(); got != tt.wantOptionPath {
				t.Fatalf("unexpected option path: %s", got)
			}
			tt.wantOptionValue(t, data.Get(atom.NewKeyword("option_value")))
		})
	}
}

func runSchemaGenerateExceptionData(t *testing.T, expr string) *coll.Map {
	t.Helper()

	runner := mustNewSchemaRunner(t)
	value, err := runner.Run(fmt.Sprintf(`(try %s (catch [e] (ex-data e)))`, expr))
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	return mustMap(t, value)
}

func assertSchemaGenerateExceptionData(t *testing.T, data *coll.Map, wantSchemaKind string) {
	t.Helper()

	if got := mustString(t, data.Get(atom.NewKeyword("schema_kind"))); got != wantSchemaKind {
		t.Fatalf("unexpected schema kind: %s", got)
	}
	if data.Get(atom.NewKeyword("schema_form")) == nil {
		t.Fatal("expected schema form in canonical exception data")
	}
}
