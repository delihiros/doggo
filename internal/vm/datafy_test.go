package vm

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func TestDatafyCrossNamespaceDefinitionsRemainDistinct(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.datafy :as datafy])
		(ns lib.one)
		(defrecord User [name])
		(ns lib.two)
		(defrecord User [name])
		(ns app)
		[(datafy/datafy lib.one/User) (datafy/datafy lib.two/User)]`)
	if err != nil {
		t.Fatal(err)
	}

	items := mustVector(t, value)
	if len(items.Items) != 2 {
		t.Fatalf("datafy views = %s", value)
	}
	if got := mustString(t, mustMap(t, items.Items[0]).Get(atom.NewKeyword("qualified-name"))); got != "lib.one/User" {
		t.Fatalf("left qualified name = %s", got)
	}
	if got := mustString(t, mustMap(t, items.Items[1]).Get(atom.NewKeyword("qualified-name"))); got != "lib.two/User" {
		t.Fatalf("right qualified name = %s", got)
	}
}

func TestDatafyValueNormalizesGoObjectsSafely(t *testing.T) {
	view := mustMap(t, datafyValue(atom.NewGoObject(struct{ Name string }{Name: "Ada"})))
	if got := mustKeywordString(t, view.Get(atom.NewKeyword("kind"))); got != "go-object" {
		t.Fatalf("go-object kind = %s", got)
	}
	if got := mustString(t, view.Get(atom.NewKeyword("type"))); got == "" {
		t.Fatal("empty go-object type")
	}
}

func mustKeywordString(t *testing.T, value types.Value) string {
	t.Helper()
	keyword, ok := value.(atom.Keyword)
	if !ok {
		t.Fatalf("expected keyword, got %T %v", value, value)
	}
	return string(keyword)
}

func mustString(t *testing.T, value types.Value) string {
	t.Helper()
	str, ok := value.(atom.String)
	if !ok {
		t.Fatalf("expected string, got %T %v", value, value)
	}
	return string(str)
}

func mustInt(t *testing.T, value types.Value) int {
	t.Helper()
	number, ok := value.(types.Number)
	if !ok {
		t.Fatalf("expected number, got %T %v", value, value)
	}
	return int(number.ToFloat64())
}
