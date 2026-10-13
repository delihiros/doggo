package vm

import (
	stderrors "errors"
	"runtime/debug"
	"testing"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestExInfoCarriesDataAndOptionalCause(t *testing.T) {
	runner := MustNewRunner()

	value, err := runner.Run(`
		(let [cause (ex-info "tail" {:kind :inner})
		      info (ex-info "boom" {"code" 1 :attempt 2} cause)
		      payload (ex-data info)
		      view (ex->map info)]
		  [(ex-message info)
		   (get payload "code")
		   (get payload :code)
		   (:attempt payload)
		   (ex-message (ex-cause info))
		   (:message view)
		   (get (:data view) "code")
		   (= #{:message :data :cause :trace} (set (keys view)))])
	`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if got := mustString(t, vec.Items[0]); got != "boom" {
		t.Fatalf("unexpected ex-info message: %s", got)
	}
	if got := mustInt(t, vec.Items[1]); got != 1 {
		t.Fatalf("unexpected string-key payload value: %d", got)
	}
	if vec.Items[2] != atom.NilValue {
		t.Fatalf("expected string and keyword keys to remain distinct, got %v", vec.Items[2])
	}
	if got := mustInt(t, vec.Items[3]); got != 2 {
		t.Fatalf("unexpected ex-data attempt: %d", got)
	}
	if got := mustString(t, vec.Items[4]); got != "tail" {
		t.Fatalf("unexpected cause message: %s", got)
	}
	if got := mustString(t, vec.Items[5]); got != "boom" {
		t.Fatalf("unexpected ex->map message: %s", got)
	}
	if got := mustInt(t, vec.Items[6]); got != 1 {
		t.Fatalf("unexpected ex->map payload value: %d", got)
	}
	if !mustBool(t, vec.Items[7]) {
		t.Fatal("unexpected ex->map keys")
	}

	value, err = runner.Run(`
		(let [info (ex-info "solo" {:kind :solo})]
		  [(ex-message info) (ex-cause info) (ex-data info)])
	`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	vec = mustVector(t, value)
	if got := mustString(t, vec.Items[0]); got != "solo" {
		t.Fatalf("unexpected message: %s", got)
	}
	if vec.Items[1] != atom.NilValue {
		t.Fatalf("expected nil cause, got %T %v", vec.Items[1], vec.Items[1])
	}
	payload := mustMap(t, vec.Items[2])
	if got := mustKeywordString(t, payload.Get(atom.NewKeyword("kind"))); got != "solo" {
		t.Fatalf("unexpected ex-data kind: %s", got)
	}
}

func TestCanonicalExceptionInspectorsNormalizeGoWrappedHostErrors(t *testing.T) {
	hostErr := atom.NewGoObject(stderrors.New("host boom"))

	exData := mustExceptionBuiltin(t, "ex-data").Fn(hostErr)
	data := mustMap(t, exData)
	if data.Count() != 0 {
		t.Fatalf("expected host error without attached data to return an empty map, got %s", data.String())
	}
	if got := mustString(t, mustExceptionBuiltin(t, "ex-message").Fn(hostErr)); got != "host boom" {
		t.Fatalf("unexpected host exception message: %s", got)
	}
	view := mustMap(t, mustExceptionBuiltin(t, "ex->map").Fn(hostErr))
	if got := mustString(t, view.Get(atom.NewKeyword("message"))); got != "host boom" {
		t.Fatalf("unexpected mapped message: %s", got)
	}
	if got := mustExceptionBuiltin(t, "ex-data").Fn(atom.NewNumberInt(42)); got != atom.NilValue {
		t.Fatalf("expected ex-data to return nil for non-error value, got %T %v", got, got)
	}
}

func TestExCausePreservesExInfo(t *testing.T) {
	runner := MustNewRunner()

	value, err := runner.Run(`
		(let [inner (ex-info "inner" {:code :bad})
		      outer (ex-info "boom" {:kind :outer} inner)
		      cause (ex-cause outer)]
		  [(some? (ex->map cause))
		   (ex-message cause)
		   (ex-data cause)])
	`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	vec := mustVector(t, value)
	if got := mustBool(t, vec.Items[0]); !got {
		t.Fatal("expected ex-cause to preserve ex-info as an error")
	}
	if got := mustString(t, vec.Items[1]); got != "inner" {
		t.Fatalf("unexpected preserved cause message: %s", got)
	}
	payload := mustMap(t, vec.Items[2])
	if got := mustKeywordString(t, payload.Get(atom.NewKeyword("code"))); got != "bad" {
		t.Fatalf("unexpected preserved cause payload code: %s", got)
	}
}

func TestExInfoAcceptsSchemaExplainPayload(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Run(`(require '[doggo.schema :as schema])`); err != nil {
		t.Fatalf("require schema namespace: %v", err)
	}

	value, err := runner.Run(`
		(ex-data
		  (ex-info "schema invalid"
		           (schema/explain [:map [:name :string]] {:name 42})))`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	view := mustMap(t, value)
	if got := mustBool(t, view.Get(atom.NewKeyword("valid?"))); got {
		t.Fatal("expected explain payload to remain invalid")
	}
	problems := mustVector(t, view.Get(atom.NewKeyword("problems")))
	if len(problems.Items) != 1 {
		t.Fatalf("unexpected explain payload problems length: %d", len(problems.Items))
	}
	problem := mustMap(t, problems.Items[0])
	if got := mustVector(t, problem.Get(atom.NewKeyword("path"))).String(); got != "[:name]" {
		t.Fatalf("unexpected explain payload path: %s", got)
	}
}

func TestArgumentVMErrorReplacesNonPlainActualValues(t *testing.T) {
	goObject := atom.NewGoObject(struct{ Name string }{Name: "host"})
	nested := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("value"),
		coll.NewVector(coll.NewList(coll.NewSet(goObject))),
	})

	for _, actual := range []types.Value{goObject, nested} {
		err := argumentVMError("doggo.url/parse", "url", "plain data", actual, "invalid argument")
		vmErr, ok := err.(*VMError)
		if !ok {
			t.Fatalf("expected VMError, got %T", err)
		}
		data := mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
		safeActual := data.Get(atom.NewKeyword("actual"))
		if _, ok := safeActual.(atom.String); !ok {
			t.Fatalf("unsafe actual was retained: %T %v", safeActual, safeActual)
		}
		if containsGoObject(data) {
			t.Fatalf("exception data contains GoObject: %v", data)
		}
		if !utf8.ValidString(safeActual.String()) || !utf8.ValidString(data.String()) {
			t.Fatalf("safe actual is not valid UTF-8: %v", safeActual)
		}
	}
}

func TestArgumentVMErrorPreservesPlainActualValues(t *testing.T) {
	var deep types.Value = atom.NewString("leaf")
	for range 32 {
		deep = coll.NewVector(deep)
	}
	actual := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("values"),
		coll.NewVector(
			atom.NewString("ok"),
			coll.NewList(atom.NewNumberInt(1)),
			coll.NewSet(atom.NewKeyword("ready")),
			deep,
		),
	})
	err := argumentVMError("doggo.url/parse", "url", "plain data", actual, "invalid argument")
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T", err)
	}
	data := mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
	if got := data.Get(atom.NewKeyword("actual")); got != actual {
		t.Fatalf("plain actual changed: got %T %v", got, got)
	}
}

func TestArgumentVMErrorReplacesCyclicActualValue(t *testing.T) {
	previousMaxStack := debug.SetMaxStack(256 << 10)
	defer debug.SetMaxStack(previousMaxStack)

	actual := coll.NewVector()
	actual.Items = []types.Value{actual}
	err := argumentVMError("doggo.url/parse", "url", "plain data", actual, "invalid argument")
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T", err)
	}
	data := mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
	if _, ok := data.Get(atom.NewKeyword("actual")).(atom.String); !ok {
		t.Fatalf("cyclic actual was retained: %v", data)
	}
}

func containsGoObject(value types.Value) bool {
	switch value := value.(type) {
	case *atom.GoObject:
		return true
	case *coll.Map:
		for _, bucket := range value.Buckets {
			for _, entry := range bucket {
				if containsGoObject(entry.Key) || containsGoObject(entry.Value) {
					return true
				}
			}
		}
	case *coll.Vector:
		for _, item := range value.Items {
			if containsGoObject(item) {
				return true
			}
		}
	case *coll.List:
		for _, item := range value.Items {
			if containsGoObject(item) {
				return true
			}
		}
	case *coll.Set:
		for _, bucket := range value.Buckets {
			for _, item := range bucket {
				if containsGoObject(item) {
					return true
				}
			}
		}
	}
	return false
}

func mustExceptionBuiltin(t *testing.T, name string) *Builtin {
	t.Helper()
	for _, def := range builtinsException {
		if def.Name == name {
			return def.Builtin
		}
	}
	t.Fatalf("missing builtin %q", name)
	return nil
}

func mustVector(t *testing.T, v types.Value) *coll.Vector {
	t.Helper()
	vec, ok := v.(*coll.Vector)
	if !ok {
		t.Fatalf("expected vector, got %T %v", v, v)
	}
	return vec
}

func mustBool(t *testing.T, v types.Value) bool {
	t.Helper()
	value, ok := v.(atom.Boolean)
	if !ok {
		t.Fatalf("expected boolean, got %T %v", v, v)
	}
	return bool(value)
}
