package schema

import (
	"context"
	"errors"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type instrumentTestFn struct {
	name    string
	handler func(context.Context, ...types.Value) (types.Value, error)
}

func (f *instrumentTestFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	return f.handler(ctx, args...)
}

func (f *instrumentTestFn) String() string {
	if f.name != "" {
		return "#<instrument-test:" + f.name + ">"
	}
	return "#<instrument-test>"
}
func (f *instrumentTestFn) Type() string                  { return "instrument-test" }
func (f *instrumentTestFn) Hash() uint32                  { return 17 }
func (f *instrumentTestFn) Equals(other types.Value) bool { return f == other }
func (f *instrumentTestFn) IsSelfEvaluating() bool        { return true }

func mustSchemaValue(t *testing.T, value types.Value) *Schema {
	t.Helper()
	sch, err := Normalize(value)
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func TestInstrumentCallableChecksArgsAndReturn(t *testing.T) {
	fn := &instrumentTestFn{
		name: "echo-int",
		handler: func(_ context.Context, args ...types.Value) (types.Value, error) {
			return args[0], nil
		},
	}
	contract := InstrumentContract{
		Args: mustSchemaValue(t, coll.NewVector(atom.NewKeyword("tuple"), atom.NewKeyword("int"))),
		Ret:  mustSchemaValue(t, atom.NewKeyword("int")),
	}
	wrapped, err := InstrumentCallable(fn, contract, InstrumentOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := wrapped.Invoke(context.Background(), atom.NewString("x")); err == nil {
		t.Fatal("expected arg contract failure")
	}

	result, err := wrapped.Invoke(context.Background(), atom.NewNumberInt(7))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(types.Int64Valuer); func() int64 { v, _ := got.Int64Value(); return v }() != 7 {
		t.Fatalf("unexpected result: %s", result)
	}

	badReturnFn := &instrumentTestFn{
		name: "bad-return",
		handler: func(_ context.Context, args ...types.Value) (types.Value, error) {
			return atom.NewString("bad"), nil
		},
	}
	wrappedBadReturn, err := InstrumentCallable(badReturnFn, contract, InstrumentOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrappedBadReturn.Invoke(context.Background(), atom.NewNumberInt(1)); err == nil {
		t.Fatal("expected return contract failure")
	}
}

func TestInstrumentCallableMalformedContractReturnsCanonicalError(t *testing.T) {
	fn := &instrumentTestFn{name: "noop", handler: func(_ context.Context, _ ...types.Value) (types.Value, error) { return coll.NewVector(), nil }}
	_, err := InstrumentCallable(fn, InstrumentContract{Args: mustSchemaValue(t, atom.NewKeyword("int"))}, InstrumentOptions{Target: types.Symbol("demo")})
	if err == nil {
		t.Fatal("expected malformed contract failure")
	}

	var instErr *InstrumentError
	if !errors.As(err, &instErr) {
		t.Fatalf("InstrumentCallable returned %T, want *InstrumentError", err)
	}
	data := instErr.ExceptionData()
	if data == nil {
		t.Fatal("expected canonical exception data")
	}
	if got, want := data.Type, "SchemaInstrumentError"; got != want {
		t.Fatalf("exception type = %q, want %q", got, want)
	}
	if got, want := data.Kind, "schema instrumentation error"; got != want {
		t.Fatalf("exception kind = %q, want %q", got, want)
	}
	if got, want := data.Phase, "runtime"; got != want {
		t.Fatalf("exception phase = %q, want %q", got, want)
	}
	if got := data.Data["contract"]; got == nil {
		t.Fatal("expected contract in exception data")
	}
	if got := data.Data["target"]; got == nil {
		t.Fatal("expected target in exception data")
	}
}

func TestUnstrumentCallableRestoresOriginalValue(t *testing.T) {
	fn := &instrumentTestFn{name: "noop", handler: func(_ context.Context, _ ...types.Value) (types.Value, error) { return coll.NewVector(), nil }}
	contract := InstrumentContract{Ret: mustSchemaValue(t, atom.NewKeyword("nil"))}
	wrapped, err := InstrumentCallable(fn, contract, InstrumentOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := UnstrumentCallable(wrapped); got != fn {
		t.Fatalf("UnstrumentCallable(wrapped) = %#v, want original fn %#v", got, fn)
	}
	if got := UnstrumentCallable(atom.NewString("x")); !got.Equals(atom.NewString("x")) {
		t.Fatalf("UnstrumentCallable(non-wrapper) changed value: %s", got)
	}
}
