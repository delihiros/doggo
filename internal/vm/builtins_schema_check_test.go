package vm

import (
	"strings"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestSchemaCheckBuiltinReturnsStableReportMaps(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(let [pass (check (fn [x] true) [:enum :ok] {:seed 7 :iterations 2})
		      fail (check (fn [x] false) [:enum :bad] {:iterations 1})
		      threw (check (fn [x] (throw (ex-info "boom" {:input x}))) [:enum :boom] {:iterations 1})]
		  [pass fail threw])`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	pass := mustMap(t, vec.Items[0])
	fail := mustMap(t, vec.Items[1])
	threw := mustMap(t, vec.Items[2])

	assertSchemaCheckTopLevelKeys(t, pass)
	assertSchemaCheckTopLevelKeys(t, fail)
	assertSchemaCheckTopLevelKeys(t, threw)

	if got := mustBool(t, pass.Get(atom.NewKeyword("pass?"))); !got {
		t.Fatal("expected pass report")
	}
	if got := mustInt(t, pass.Get(atom.NewKeyword("iterations"))); got != 2 {
		t.Fatalf("pass iterations = %d, want 2", got)
	}
	if got := mustInt(t, pass.Get(atom.NewKeyword("seed"))); got != 7 {
		t.Fatalf("pass seed = %d, want 7", got)
	}
	if got := pass.Get(atom.NewKeyword("failing-input")); got != atom.NilValue {
		t.Fatalf("expected nil pass failing-input, got %T %v", got, got)
	}
	if got := pass.Get(atom.NewKeyword("shrunk-input")); got != atom.NilValue {
		t.Fatalf("expected nil pass shrunk-input, got %T %v", got, got)
	}
	if got := mustVector(t, pass.Get(atom.NewKeyword("shrink-steps"))); len(got.Items) != 0 {
		t.Fatalf("pass shrink-steps len = %d, want 0", len(got.Items))
	}
	if got := pass.Get(atom.NewKeyword("failure")); got != atom.NilValue {
		t.Fatalf("expected nil pass failure, got %T %v", got, got)
	}
	if got := pass.Get(atom.NewKeyword("explain")); got != atom.NilValue {
		t.Fatalf("expected nil pass explain, got %T %v", got, got)
	}

	if got := mustBool(t, fail.Get(atom.NewKeyword("pass?"))); got {
		t.Fatal("expected failure report")
	}
	if got := mustKeywordString(t, fail.Get(atom.NewKeyword("failing-input"))); got != "bad" {
		t.Fatalf("failing-input = %s, want :bad", got)
	}
	if got := mustKeywordString(t, fail.Get(atom.NewKeyword("shrunk-input"))); got != "bad" {
		t.Fatalf("shrunk-input = %s, want :bad", got)
	}
	if got := mustVector(t, fail.Get(atom.NewKeyword("shrink-steps"))); len(got.Items) != 0 {
		t.Fatalf("fail shrink-steps len = %d, want 0", len(got.Items))
	}
	failFailure := mustMap(t, fail.Get(atom.NewKeyword("failure")))
	assertSchemaCheckFailureKeys(t, failFailure)
	if got := mustKeywordString(t, failFailure.Get(atom.NewKeyword("kind"))); got != "return-false" {
		t.Fatalf("failure kind = %s, want :return-false", got)
	}
	if got := mustBool(t, failFailure.Get(atom.NewKeyword("value"))); got {
		t.Fatalf("failure value = %v, want false", got)
	}
	if got := failFailure.Get(atom.NewKeyword("exception")); got != atom.NilValue {
		t.Fatalf("expected nil failure exception, got %T %v", got, got)
	}

	threwFailure := mustMap(t, threw.Get(atom.NewKeyword("failure")))
	assertSchemaCheckFailureKeys(t, threwFailure)
	if got := mustKeywordString(t, threwFailure.Get(atom.NewKeyword("kind"))); got != "threw" {
		t.Fatalf("threw kind = %s, want :threw", got)
	}
	if got := threwFailure.Get(atom.NewKeyword("value")); got != atom.NilValue {
		t.Fatalf("expected nil threw value, got %T %v", got, got)
	}
	exception := mustMap(t, threwFailure.Get(atom.NewKeyword("exception")))
	if got := mustString(t, exception.Get(atom.NewKeyword("message"))); !strings.Contains(got, "boom") {
		t.Fatalf("unexpected thrown message: %s", got)
	}
}

func TestSchemaCheckBuiltinSupportsTupleAndZeroArgContracts(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(let [tuple-report (check (fn [x y] false) [:tuple [:enum 3] [:enum 4]] {:iterations 1})
		      zero-report (check (fn [] false) [:tuple] {:iterations 1})]
		  [tuple-report zero-report])`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	tupleReport := mustMap(t, vec.Items[0])
	zeroReport := mustMap(t, vec.Items[1])

	if got := mustVector(t, tupleReport.Get(atom.NewKeyword("failing-input"))).String(); got != "[3 4]" {
		t.Fatalf("tuple failing-input = %s, want [3 4]", got)
	}
	if got := mustVector(t, tupleReport.Get(atom.NewKeyword("shrunk-input"))).String(); got != "[3 4]" {
		t.Fatalf("tuple shrunk-input = %s, want [3 4]", got)
	}
	if got := mustVector(t, zeroReport.Get(atom.NewKeyword("failing-input"))).String(); got != "[]" {
		t.Fatalf("zero-arg failing-input = %s, want []", got)
	}
	if got := mustVector(t, zeroReport.Get(atom.NewKeyword("shrunk-input"))).String(); got != "[]" {
		t.Fatalf("zero-arg shrunk-input = %s, want []", got)
	}
	if got := len(mustVector(t, zeroReport.Get(atom.NewKeyword("shrink-steps"))).Items); got != 0 {
		t.Fatalf("zero-arg shrink-steps len = %d, want 0", got)
	}
}

func TestSchemaCheckBuiltinUsesDeterministicSeedsAndOverrides(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(let [first (check (fn [x] false) :int {:seed 9 :iterations 1})
		      second (check (fn [x] false) :int {:seed 9 :iterations 1})
		      override (check (fn [x] false) :int {:iterations 1 :generator-overrides {:int 42}})]
		  [(:failing-input first)
		   (:failing-input second)
		   (:failing-input override)
		   (:seed first)
		   (:seed second)])`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if !vec.Items[0].Equals(vec.Items[1]) {
		t.Fatalf("deterministic seed produced different failing inputs: %v vs %v", vec.Items[0], vec.Items[1])
	}
	if got := mustInt(t, vec.Items[3]); got != 9 {
		t.Fatalf("first seed = %d, want 9", got)
	}
	if got := mustInt(t, vec.Items[4]); got != 9 {
		t.Fatalf("second seed = %d, want 9", got)
	}
	if got := mustInt(t, vec.Items[2]); got != 42 {
		t.Fatalf("override failing-input = %d, want 42", got)
	}
}

func TestSchemaCheckBuiltinReturnsCanonicalExDataForMalformedOptions(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(try
		  (check (fn [x] true) :int {:iterations "x"})
		  (catch [e] (ex-data e)))`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	data := mustMap(t, value)
	if got := mustString(t, data.Get(atom.NewKeyword("reason"))); got != "malformed iteration count" {
		t.Fatalf("unexpected reason: %s", got)
	}
	if got := mustVector(t, data.Get(atom.NewKeyword("option_path"))).String(); got != "[:iterations]" {
		t.Fatalf("unexpected option path: %s", got)
	}
	if got := mustString(t, data.Get(atom.NewKeyword("option_value"))); got != "x" {
		t.Fatalf("unexpected option value: %s", got)
	}
}

func TestSchemaCheckBuiltinPropagatesLowerLayerSchemaErrors(t *testing.T) {
	runner := mustNewSchemaRunner(t)

	value, err := runner.Run(`
		(try
		  (check (fn [x] true) [:predicate identity] {:iterations 1})
		  (catch [e] (ex-data e)))`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	data := mustMap(t, value)
	if got := mustString(t, data.Get(atom.NewKeyword("schema_kind"))); got != "predicate" {
		t.Fatalf("unexpected schema kind: %s", got)
	}
}

func assertSchemaCheckTopLevelKeys(t *testing.T, report *coll.Map) {
	t.Helper()

	want := []types.Value{
		atom.NewKeyword("pass?"),
		atom.NewKeyword("iterations"),
		atom.NewKeyword("seed"),
		atom.NewKeyword("failing-input"),
		atom.NewKeyword("shrunk-input"),
		atom.NewKeyword("shrink-steps"),
		atom.NewKeyword("failure"),
		atom.NewKeyword("explain"),
	}
	if got := report.Count(); got != len(want) {
		t.Fatalf("report key count = %d, want %d", got, len(want))
	}
	for _, key := range want {
		if report.Get(key) == nil {
			t.Fatalf("report missing key %s", key)
		}
	}
}

func assertSchemaCheckFailureKeys(t *testing.T, failure *coll.Map) {
	t.Helper()

	want := []types.Value{
		atom.NewKeyword("kind"),
		atom.NewKeyword("message"),
		atom.NewKeyword("value"),
		atom.NewKeyword("exception"),
	}
	if got := failure.Count(); got != len(want) {
		t.Fatalf("failure key count = %d, want %d", got, len(want))
	}
	for _, key := range want {
		if failure.Get(key) == nil {
			t.Fatalf("failure missing key %s", key)
		}
	}
}
