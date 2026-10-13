package vm

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/iotest"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestUUIDRandomIsCanonicalVersion4(t *testing.T) {
	uuid, err := uuidRandomFrom(bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if got := uuid.Value(); got != "00000000-0000-4000-8000-000000000000" {
		t.Fatalf("random UUID = %q", got)
	}
}

func TestUUIDParseRejectsDirectInvalidUTF8(t *testing.T) {
	builtin, ok := DefaultBuiltinCatalog().lookup("uuid-parse*")
	if !ok {
		t.Fatal("missing uuid-parse*")
	}
	result, err := builtin.CtxFn(context.Background(), atom.NewString("\xff"))
	if err == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
	if result != nil {
		t.Fatalf("parse returned a result on error: %v", result)
	}
	if !utf8.ValidString(err.Error()) {
		t.Fatalf("error is not valid UTF-8: %q", err.Error())
	}
	data := uuidExceptionData(t, err)
	if !utf8.ValidString(data.String()) {
		t.Fatalf("exception data is not valid UTF-8: %q", data.String())
	}
	assertUUIDErrorData(t, data, "invalid-argument", "doggo.uuid/parse")
	if got := mustString(t, data.Get(atom.NewKeyword("actual"))); got != "<invalid UTF-8>" {
		t.Fatalf("safe actual = %q", got)
	}
}

func TestUUIDRandomReadFailureIsCanonicalAndHasNoFallback(t *testing.T) {
	result, err := uuidRandomFrom(iotest.ErrReader(errors.New("\xff")))
	if err == nil {
		t.Fatal("random source failure was accepted")
	}
	if result != nil {
		t.Fatalf("random returned fallback value: %v", result)
	}
	if !utf8.ValidString(err.Error()) {
		t.Fatalf("error is not valid UTF-8: %q", err.Error())
	}
	data := uuidExceptionData(t, err)
	if !utf8.ValidString(data.String()) {
		t.Fatalf("exception data is not valid UTF-8: %q", data.String())
	}
	assertUUIDErrorData(t, data, "runtime-failure", "doggo.uuid/random")
	if containsGoObject(data) {
		t.Fatalf("exception data contains GoObject: %v", data)
	}
}

func assertUUIDErrorData(t *testing.T, data *coll.Map, kind, function string) {
	t.Helper()
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != kind {
		t.Fatalf("kind = %s, want %s", got, kind)
	}
	if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != function {
		t.Fatalf("function = %v, want %s", data.Get(atom.NewKeyword("function")), function)
	}
}

func uuidExceptionData(t *testing.T, err error) *coll.Map {
	t.Helper()
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T: %v", err, err)
	}
	return mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
}
