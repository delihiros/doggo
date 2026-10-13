package vm

import (
	"testing"

	"iter"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestSchemaShrinkBuiltinReturnsLazyCandidates(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		[(type-name (shrink :int 10))
		 (doall (map (fn [v] (valid? :int v))
		             (take 3 (shrink :int 10))))
		 (doall (take 3 (shrink :int 10)))]`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if got := mustString(t, vec.Items[0]); got != "lazy-seq" {
		t.Fatalf("unexpected shrink result type: %s", got)
	}
	if got, want := vec.Items[1].String(), "(true true true)"; got != want {
		t.Fatalf("unexpected shrink validity checks: %s, want %s", got, want)
	}
	if got, want := vec.Items[2].String(), "(0 5 9)"; got != want {
		t.Fatalf("unexpected shrink candidates: %s, want %s", got, want)
	}
}

func TestSchemaShrinkStreamBridgeIsIncremental(t *testing.T) {
	yielded := 0
	stream := iter.Seq[types.Value](func(yield func(types.Value) bool) {
		for i := range 5 {
			yielded++
			if !yield(atom.NewNumberInt(int64(i))) {
				return
			}
		}
	})

	seq := newLazySeqFromValueStream(stream)
	if yielded != 0 {
		t.Fatalf("stream should stay cold before realization, got %d yielded values", yielded)
	}

	if got := seq.First(); !got.Equals(atom.NewNumberInt(0)) {
		t.Fatalf("unexpected first shrink candidate: %v", got)
	}
	if yielded != 1 {
		t.Fatalf("expected exactly one yielded value after First, got %d", yielded)
	}

	next := seq.Next()
	if next == nil {
		t.Fatal("expected tail sequence")
	}
	if got := next.First(); !got.Equals(atom.NewNumberInt(1)) {
		t.Fatalf("unexpected second shrink candidate: %v", got)
	}
	if yielded != 2 {
		t.Fatalf("expected exactly two yielded values after second First, got %d", yielded)
	}
}

func TestSchemaShrinkSeqBuiltinReturnsFirstCandidatesEagerly(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`(shrink-seq :int 10 3)`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	if got, want := mustVector(t, value).String(), "[0 5 9]"; got != want {
		t.Fatalf("unexpected shrink-seq candidates: %s, want %s", got, want)
	}
}

func TestSchemaShrinkBuiltinReturnsValidVectorCandidates(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(doall (map (fn [v] (valid? [:vector :int] v))
		            (take 5 (shrink [:vector :int] [3 2 1]))))`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	if got, want := value.String(), "(true true true true true)"; got != want {
		t.Fatalf("unexpected shrink validation results: %s, want %s", got, want)
	}
}

func TestSchemaShrinkBuiltinReturnsCanonicalExDataForUnsupportedKinds(t *testing.T) {
	tests := []struct {
		name           string
		expr           string
		wantSchemaKind string
	}{
		{
			name:           "predicate",
			expr:           `(try (shrink [:predicate identity] 1) (catch [e] (ex-data e)))`,
			wantSchemaKind: "predicate",
		},
		{
			name:           "and",
			expr:           `(try (shrink [:and :int :number] 1) (catch [e] (ex-data e)))`,
			wantSchemaKind: "and",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := runSchemaShrinkExceptionData(t, tt.expr)
			assertSchemaShrinkExceptionData(t, data, tt.wantSchemaKind)
			if got := mustString(t, data.Get(atom.NewKeyword("reason"))); got != "unsupported shrink schema kind" {
				t.Fatalf("unexpected shrink reason: %s", got)
			}
		})
	}
}

func TestSchemaShrinkBuiltinReturnsCanonicalExDataForInvalidValues(t *testing.T) {
	data := runSchemaShrinkExceptionData(t, `(try (shrink :int "x") (catch [e] (ex-data e)))`)
	assertSchemaShrinkExceptionData(t, data, "int")

	if got := mustString(t, data.Get(atom.NewKeyword("reason"))); got != "value does not satisfy schema for shrinking" {
		t.Fatalf("unexpected shrink reason: %s", got)
	}
	if got := mustString(t, data.Get(atom.NewKeyword("value"))); got != "x" {
		t.Fatalf("unexpected invalid value in canonical error data: %s", got)
	}
}

func runSchemaShrinkExceptionData(t *testing.T, expr string) *coll.Map {
	t.Helper()

	runner := mustNewSchemaRunner(t)
	value, err := runner.Run(expr)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	return mustMap(t, value)
}

func assertSchemaShrinkExceptionData(t *testing.T, data *coll.Map, wantSchemaKind string) {
	t.Helper()

	if got := mustString(t, data.Get(atom.NewKeyword("schema_kind"))); got != wantSchemaKind {
		t.Fatalf("unexpected schema kind: %s", got)
	}
	if data.Get(atom.NewKeyword("schema")) == nil {
		t.Fatal("expected schema in canonical shrink error data")
	}
}
