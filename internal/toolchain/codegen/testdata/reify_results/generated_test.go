package reify

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"doggo/internal/interop"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/vm"
	"doggo/reifyprobe/native"
)

type resultInvoker struct {
	value types.Value
	err   error
	args  []types.Value
}

func (i *resultInvoker) Invoke(_ context.Context, _ types.Value, args []types.Value, _ types.Environment) (types.Value, error) {
	i.args = args
	return i.value, i.err
}
func method(name string) types.Map {
	return coll.NewEmptyMap().Assoc(atom.NewKeyword(name), atom.Boolean(true))
}
func captureWarning(t *testing.T, call func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = f
	defer func() { os.Stderr = previous; f.Close() }()
	call()
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCheckerConversionFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result types.Value
		want   string
	}{
		{"wrong value", coll.NewVector(atom.String("bad"), atom.NilValue), "result 0"},
		{"wrong error", coll.NewVector(atom.NewNumberInt(7), atom.String("bad")), "result 1"},
		{"not vector", atom.NewNumberInt(7), "vector"},
		{"short vector", coll.NewVector(atom.NewNumberInt(7)), "2"},
		{"long vector", coll.NewVector(atom.NewNumberInt(7), atom.NilValue, atom.NilValue), "2"},
		{"nil integer", coll.NewVector(atom.NilValue, atom.NilValue), "nil"},
		{"raw nil integer", coll.NewVector(nil, atom.NilValue), "nil"},
		{"overflow", coll.NewVector(atom.NewNumberFloat(math.Exp2(64)), atom.NilValue), "loss"},
		{"fractional", coll.NewVector(atom.NewNumberFloat(1.5), atom.NilValue), "loss"},
		{"wrapped fractional", coll.NewVector(atom.NewGoObject(float64(1.5)), atom.NilValue), "loss"},
		{"wrapped unsigned overflow", coll.NewVector(atom.NewGoObject(uint64(math.MaxUint64)), atom.NilValue), "overflows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewNativeCheckerImpl(&resultInvoker{value: tc.result}, nil, method("check"))
			n, err := c.Check()
			if n != 0 || err == nil || !strings.Contains(err.Error(), "Checker.Check") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Check() = %d, %v; want atomic zero and contextual conversion error containing %q", n, err, tc.want)
			}
		})
	}
}

func TestValidAndReturnedErrors(t *testing.T) {
	realErr := errors.New("real Go failure")
	for _, tc := range []struct {
		name    string
		value   types.Value
		wantN   int
		wantErr error
	}{
		{"valid", coll.NewVector(atom.NewNumberInt(7), atom.NilValue), 7, nil},
		{"Go error", coll.NewVector(atom.NewNumberInt(7), atom.NewGoObject(realErr)), 7, realErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := NewNativeCheckerImpl(&resultInvoker{value: tc.value}, nil, method("check")).Check()
			if n != tc.wantN || !errors.Is(err, tc.wantErr) {
				t.Fatalf("Check() = %d, %v", n, err)
			}
		})
	}
	// A bad non-error slot must not hide a real error returned in another slot.
	n, err := NewNativeCheckerImpl(&resultInvoker{value: coll.NewVector(atom.String("bad"), atom.NewGoObject(realErr))}, nil, method("check")).Check()
	if n != 0 || !errors.Is(err, realErr) || !strings.Contains(err.Error(), "result 0") {
		t.Fatalf("lost conversion or Go error: %d, %v", n, err)
	}
	for _, value := range []types.Value{atom.NilValue, atom.NewGoObject(native.Count(12)), atom.NewNumberInt(12), atom.NewNumberInt(128)} {
		inv := &resultInvoker{value: coll.NewVector(value, atom.NilValue, atom.NilValue, atom.NilValue)}
		count, ptr, slice, err := NewNativeValuesImpl(inv, nil, method("values")).Values()
		bad := value == atom.NilValue || value.Equals(atom.NewNumberInt(128))
		if ptr != nil || slice != nil || (bad && (err == nil || count != 0)) || (!bad && (err != nil || count != 12)) {
			t.Fatalf("Values(%s) = %d, %v, %v, %v", value, count, ptr, slice, err)
		}
	}
}

func TestNoErrorMethodsWarnAndReturnZeros(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     types.Value
		invokeErr error
		pair      bool
	}{
		{"single conversion", atom.String("bad"), nil, false},
		{"single nil", atom.NilValue, nil, false},
		{"single raw nil", nil, nil, false},
		{"pair conversion", coll.NewVector(atom.NewNumberInt(7), atom.NewNumberInt(8)), nil, true},
		{"pair shape", atom.NewNumberInt(7), nil, true},
		{"pair extra", coll.NewVector(atom.NewNumberInt(7), atom.String("ok"), atom.NilValue), nil, true},
		{"invoke", atom.NewNumberInt(7), errors.New("callback failure"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := &resultInvoker{value: tc.value, err: tc.invokeErr}
			impl := NewNativeNoErrorImpl(inv, nil, method("single").Assoc(atom.NewKeyword("pair"), atom.Boolean(true)))
			warning := captureWarning(t, func() {
				if tc.pair {
					n, s := impl.Pair()
					if n != 0 || s != "" {
						t.Errorf("Pair() = %d, %q", n, s)
					}
				} else if n := impl.Single(); n != 0 {
					t.Errorf("Single() = %d", n)
				}
			})
			method := "NoError.Single"
			if tc.pair {
				method = "NoError.Pair"
			}
			if !strings.Contains(warning, "warning: error in reified method "+method) {
				t.Fatalf("missing diagnostic: %q", warning)
			}
		})
	}
	impl := NewNativeNoErrorImpl(&resultInvoker{value: coll.NewVector(atom.NewNumberInt(7), atom.String("ok"))}, nil, method("pair"))
	if n, s := impl.Pair(); n != 7 || s != "ok" {
		t.Fatalf("valid Pair() = %d, %q", n, s)
	}
}

func TestAliasErrorPolicies(t *testing.T) {
	boom := errors.New("callback boom")
	inv := &resultInvoker{err: boom}
	for _, tc := range []struct {
		name string
		fail func() error
	}{
		{"plain", NewNativePlainImpl(inv, nil, method("fail")).Fail},
		{"alias", NewNativeAliasImpl(inv, nil, method("fail")).Fail},
		{"chain", NewNativeChainImpl(inv, nil, method("fail")).Fail},
		{"missing alias", NewNativeAliasImpl(inv, nil, coll.NewEmptyMap()).Fail},
		{"missing chain", NewNativeChainImpl(inv, nil, coll.NewEmptyMap()).Fail},
		{"invalid alias error", NewNativeAliasImpl(&resultInvoker{value: atom.String("bad")}, nil, method("fail")).Fail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fail(); err == nil {
				t.Fatal("expected observable error")
			}
		})
	}
	for _, inv := range []*resultInvoker{{err: boom}, {value: coll.NewVector(atom.String("bad"), atom.NilValue)}, {value: coll.NewVector(atom.NewNumberInt(7), atom.String("bad"))}, {value: atom.NilValue}} {
		if n, err := NewNativeAliasPairImpl(inv, nil, method("check")).Check(); n != 0 || err == nil {
			t.Errorf("alias pair = %d, %v", n, err)
		}
	}
	if n, err := NewNativeAliasPairImpl(inv, nil, coll.NewEmptyMap()).Check(); n != 0 || err == nil {
		t.Errorf("missing alias pair = %d, %v", n, err)
	}
	if n, err := NewNativeAliasPairImpl(&resultInvoker{value: coll.NewVector(atom.NewNumberInt(7), atom.NilValue)}, nil, method("check")).Check(); n != 7 || err != nil {
		t.Errorf("valid alias pair = %d, %v", n, err)
	}
	// Defined concrete error types are ordinary result types, not generic error slots.
	if got := NewNativeConcreteImpl(&resultInvoker{value: atom.NewGoObject(native.ConcreteError("native"))}, nil, method("fail")).Fail(); got != "native" {
		t.Fatalf("concrete = %v", got)
	}
	warning := captureWarning(t, func() {
		if got := NewNativeConcreteImpl(inv, nil, method("fail")).Fail(); got != "" {
			t.Errorf("concrete failure = %v", got)
		}
	})
	if !strings.Contains(warning, "Concrete.Fail") {
		t.Fatalf("concrete failure warning = %q", warning)
	}
	// Aliased error slots must also receive argument-conversion errors.
	if err := NewNativeArgumentsImpl(inv, nil, method("fail")).Fail(string([]byte{0xff})); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("argument conversion = %v", err)
	}
	badArg := int(^uint(0) >> 1)
	variadic := &resultInvoker{value: coll.NewVector(atom.NewNumberInt(7), atom.NilValue)}
	if n, err := NewNativeVariadicImpl(variadic, nil, method("join")).Join(badArg, 2, 3); n != 7 || err != nil || len(variadic.args) != 2 {
		t.Fatalf("variadic = %d, %v, %v", n, err, variadic.args)
	}
	first := errors.New("first")
	last := errors.New("last")
	a, n, b := NewNativeMultiErrorImpl(&resultInvoker{value: coll.NewVector(atom.NewGoObject(first), atom.String("bad"), atom.NewGoObject(last))}, nil, method("check")).Check()
	if n != 0 || !errors.Is(a, first) || !errors.Is(b, last) || !strings.Contains(a.Error(), "result 1") || !strings.Contains(b.Error(), "result 1") {
		t.Fatalf("multi errors = %v, %d, %v", a, n, b)
	}
	for _, result := range []types.Value{coll.NewVector(atom.NewNumberInt(7), atom.NewGoObject(boom), atom.NilValue), coll.NewVector(atom.NewGoObject(boom))} {
		if _, err := NewNativeCheckerImpl(&resultInvoker{value: result}, nil, method("check")).Check(); err == nil || (result.(types.Vector).Count() == 3 && !errors.Is(err, boom)) {
			t.Fatalf("malformed result lost error: %v", err)
		}
	}
	// Already-assignable Doggo values must retain the direct conversion path.
	if got := NewNativeDirectImpl(&resultInvoker{value: atom.String("direct")}, nil, method("direct")).Direct(); got != atom.String("direct") {
		t.Fatalf("direct value = %v", got)
	}
}

func TestVMCallbackExceptionsMatchErrorAliases(t *testing.T) {
	interop.RegisterPackage("doggo/reifyprobe/native", map[string]interface{}{
		"Plain": reflect.TypeOf((*native.Plain)(nil)).Elem(), "Alias": reflect.TypeOf((*native.Alias)(nil)).Elem(), "Chain": reflect.TypeOf((*native.Chain)(nil)).Elem(),
	})
	runner := vm.MustNewRunnerWithoutCore()
	for _, name := range []string{"Plain", "Alias", "Chain"} {
		t.Run(name, func(t *testing.T) {
			value, err := runner.Run(`(do (import "doggo/reifyprobe/native") (reify native/` + name + ` {:fail (fn [] (throw (ex-info "callback boom" {})))}))`)
			if err != nil {
				t.Fatal(err)
			}
			failer := value.(*atom.GoObject).Unwrap().(interface{ Fail() error })
			if err := failer.Fail(); err == nil || !strings.Contains(err.Error(), "callback boom") {
				t.Fatalf("Fail() = %v", err)
			}
		})
	}
}

func TestNilInterfaceResultsStayNil(t *testing.T) {
	for _, value := range []types.Value{nil, atom.NilValue, atom.NewGoObject(nil)} {
		inv := &resultInvoker{value: value}
		if got := NewNativeDirectImpl(inv, nil, method("direct")).Direct(); got != nil {
			t.Errorf("nullable interface = %#v, want nil", got)
		}
		if got := NewNativeAnyImpl(inv, nil, method("any")).Any(); got != nil {
			t.Errorf("nullable any = %#v, want nil", got)
		}
	}
}

func TestGeneratedLocalImportCollisions(t *testing.T) {
	inv := &resultInvoker{value: coll.NewVector(atom.NewNumberInt(9), atom.NilValue)}
	if n, err := NewNativeLocalCollisionImpl(inv, nil, method("check")).Check(); n != 9 || err != nil {
		t.Errorf("result0 import: %d, %v", n, err)
	}
	if n, err := NewNativeValuesCollisionImpl(inv, nil, method("check")).Check(); n != 9 || err != nil {
		t.Errorf("resultValues import: %d, %v", n, err)
	}
}

func TestGeneratedWrappedNumericLoss(t *testing.T) {
	for _, value := range []any{int64(256), int64(-129), uint64(256), float64(128), float64(1.5)} {
		inv := &resultInvoker{value: coll.NewVector(atom.NewGoObject(value), atom.NilValue, atom.NilValue, atom.NilValue)}
		n, ptr, slice, err := NewNativeValuesImpl(inv, nil, method("values")).Values()
		if n != 0 || ptr != nil || slice != nil || err == nil || !strings.Contains(err.Error(), "Values.Values") || !strings.Contains(err.Error(), "result 0") {
			t.Errorf("Values(%T(%v)) = %d, %v, %v, %v; want atomic zeros and contextual numeric error", value, value, n, ptr, slice, err)
		}
	}
	for _, value := range []float64{math.MaxFloat64, -math.MaxFloat64} {
		for _, input := range []types.Value{atom.NewNumberFloat(value), atom.NewGoObject(value)} {
			inv := &resultInvoker{value: coll.NewVector(input, atom.NilValue)}
			if n, err := NewNativeFloatImpl(inv, nil, method("float")).Float(); n != 0 || err == nil || !strings.Contains(err.Error(), "Float.Float") || !strings.Contains(err.Error(), "overflows") {
				t.Errorf("Float(%T(%v)) = %v, %v; want atomic zero and contextual overflow error", input, value, n, err)
			}
		}
	}
	for _, value := range []types.Value{atom.NewNumberFloat(1.1), atom.NewGoObject(float64(1.1)), atom.NewGoObject(float32(1.1))} {
		if n, err := NewNativeFloatImpl(&resultInvoker{value: coll.NewVector(value, atom.NilValue)}, nil, method("float")).Float(); n != float32(1.1) || err != nil {
			t.Errorf("valid rounded float = %v, %v", n, err)
		}
	}
	large := int64((1 << 62) + (1 << 38) + 1)
	if n, err := NewNativeFloatImpl(&resultInvoker{value: coll.NewVector(atom.NewGoObject(large), atom.NilValue)}, nil, method("float")).Float(); n != float32(float64(large)) || err != nil {
		t.Errorf("wrapped integer reflection float rounding = %v, %v; want %v", n, err, float32(float64(large)))
	}
}

func TestGeneratedComplexNarrowing(t *testing.T) {
	for _, value := range []complex128{complex(math.MaxFloat64, 1), complex(1, -math.MaxFloat64)} {
		inv := &resultInvoker{value: coll.NewVector(atom.NewGoObject(value), atom.NilValue)}
		if n, err := NewNativeComplexImpl(inv, nil, method("complex")).Complex(); n != 0 || err == nil || !strings.Contains(err.Error(), "Complex.Complex") {
			t.Errorf("Complex(%v) = %v, %v; want contextual overflow error", value, n, err)
		}
	}
	inv := &resultInvoker{value: coll.NewVector(atom.NewGoObject(complex128(1.1+2.2i)), atom.NilValue)}
	if n, err := NewNativeComplexImpl(inv, nil, method("complex")).Complex(); n != complex64(1.1+2.2i) || err != nil {
		t.Errorf("valid complex rounding = %v, %v", n, err)
	}
}

func TestGeneratedUnsignedNumericLoss(t *testing.T) {
	for _, value := range []types.Value{atom.NewNumberFloat(1.5), atom.NewGoObject(float64(1.5)), atom.NewGoObject(int64(-1)), atom.NewGoObject(math.Exp2(64))} {
		inv := &resultInvoker{value: coll.NewVector(value, atom.NilValue)}
		if n, err := NewNativeUnsignedImpl(inv, nil, method("unsigned")).Unsigned(); n != 0 || err == nil || !strings.Contains(err.Error(), "Unsigned.Unsigned") {
			t.Errorf("Unsigned(%v) = %v, %v; want contextual numeric error", value, n, err)
		}
	}
}
