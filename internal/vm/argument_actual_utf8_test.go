package vm

import (
	"testing"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func argumentErrorBoundaries() map[string]func(types.Value) error {
	return map[string]func(types.Value) error{
		"argument": func(actual types.Value) error {
			return argumentVMError("doggo.url/parse", "url", "plain data", actual, "invalid argument")
		},
		"http": func(actual types.Value) error {
			return httpArgumentError("url", "plain data", actual, "invalid argument")
		},
	}
}

func argumentErrorActual(t *testing.T, err error) types.Value {
	t.Helper()
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T", err)
	}
	data := mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
	return data.Get(atom.NewKeyword("actual"))
}

func TestArgumentErrorActualRawUTF8(t *testing.T) {
	invalid := atom.NewString("\xff")
	metadata := coll.NewMapFromPairs([]types.Value{atom.NewKeyword("note"), invalid})
	vectorMeta := coll.NewVector()
	vectorMeta.Metadata = metadata
	listMeta := coll.NewList()
	listMeta.Metadata = metadata
	setMeta := coll.NewSet()
	setMeta.Metadata = metadata
	mapMeta := coll.NewMap(nil)
	mapMeta.Metadata = metadata
	tests := map[string]types.Value{
		"string":      invalid,
		"keyword":     atom.NewKeyword("\xff"),
		"symbol":      types.Symbol("\xff"),
		"vector":      coll.NewVector(invalid),
		"list":        coll.NewList(invalid),
		"map key":     coll.NewMapFromPairs([]types.Value{invalid, atom.NilValue}),
		"map value":   coll.NewMapFromPairs([]types.Value{atom.NewKeyword("key"), invalid}),
		"set":         coll.NewSet(invalid),
		"deep":        coll.NewVector(coll.NewList(coll.NewMapFromPairs([]types.Value{atom.NewKeyword("key"), coll.NewSet(invalid)}))),
		"vector meta": vectorMeta,
		"list meta":   listMeta,
		"set meta":    setMeta,
		"map meta":    mapMeta,
		"map order":   &coll.Map{Order: []types.Value{invalid}},
		"set order":   &coll.Set{Order: []types.Value{invalid}},
		"map bucket":  &coll.Map{Buckets: map[uint32][]*types.MapEntry{0: {types.NewMapEntry(atom.NewKeyword("key"), invalid)}}},
		"set bucket":  &coll.Set{Buckets: map[uint32][]types.Value{0: {invalid}}},
	}
	for boundary, makeError := range argumentErrorBoundaries() {
		for name, actual := range tests {
			t.Run(boundary+"/"+name, func(t *testing.T) {
				got := mustString(t, argumentErrorActual(t, makeError(actual)))
				if got != "<invalid UTF-8>" || !utf8.ValidString(got) {
					t.Fatalf("raw safe actual = %q, want <invalid UTF-8>", got)
				}
			})
		}
	}
}

func TestArgumentErrorActualPreservesValidRawValues(t *testing.T) {
	items := make([]types.Value, 254)
	for i := range items {
		items[i] = atom.NewString("\uFFFD\\\"\n")
	}
	tests := map[string]types.Value{
		"replacement rune": atom.NewString("\uFFFD"),
		"budget boundary":  coll.NewVector(items...),
		"nested":           coll.NewMapFromPairs([]types.Value{atom.NewString("key\\\""), coll.NewVector(coll.NewList(coll.NewSet(atom.NewString("\uFFFD"))))}),
	}
	for boundary, makeError := range argumentErrorBoundaries() {
		for name, actual := range tests {
			t.Run(boundary+"/"+name, func(t *testing.T) {
				if got := argumentErrorActual(t, makeError(actual)); got != actual {
					t.Fatalf("valid raw actual changed: got %T, want original %T", got, actual)
				}
			})
		}
	}
}

type argumentActualProbe struct{ calls int }

func (p *argumentActualProbe) String() string          { p.calls++; return "host value" }
func (p *argumentActualProbe) Type() string            { p.calls++; return "host" }
func (p *argumentActualProbe) Hash() uint32            { p.calls++; return 0 }
func (p *argumentActualProbe) Equals(types.Value) bool { p.calls++; return false }
func (p *argumentActualProbe) IsSelfEvaluating() bool  { p.calls++; return true }

func TestArgumentErrorActualDoesNotEvaluateUnsafeValues(t *testing.T) {
	for boundary, makeError := range argumentErrorBoundaries() {
		for _, name := range []string{"host", "nested host", "lazy", "nested lazy", "over budget", "invalid before host", "invalid after host"} {
			t.Run(boundary+"/"+name, func(t *testing.T) {
				probe := &argumentActualProbe{}
				realizations := 0
				lazy := coll.NewLazySeq(func(types.Environment) (types.Value, error) {
					realizations++
					return coll.NewList(), nil
				}, nil)
				var actual types.Value
				switch name {
				case "host":
					actual = probe
				case "nested host":
					actual = coll.NewVector(coll.NewList(probe))
				case "lazy":
					actual = lazy
				case "nested lazy":
					actual = coll.NewVector(coll.NewList(lazy))
				case "over budget":
					items := make([]types.Value, 255)
					for i := range items {
						items[i] = atom.NilValue
					}
					actual = coll.NewVector(items...)
				case "invalid before host":
					actual = coll.NewVector(atom.NewString("\xff"), probe)
				case "invalid after host":
					actual = coll.NewVector(probe, atom.NewString("\xff"))
				}
				got := argumentErrorActual(t, makeError(actual))
				if probe.calls != 0 || realizations != 0 {
					t.Fatalf("unsafe actual evaluated: host calls %d, lazy realizations %d", probe.calls, realizations)
				}
				if safe, ok := got.(atom.String); !ok || string(safe) != "<unsafe value>" {
					t.Fatalf("unsafe actual retained: %T", got)
				}
			})
		}
	}
}

func TestArgumentErrorActualBoundsCycles(t *testing.T) {
	vector := coll.NewVector(atom.NewString("\xff"))
	vector.Items = append(vector.Items, vector)
	metadata := coll.NewMap(nil)
	metadata.Metadata = metadata
	for boundary, makeError := range argumentErrorBoundaries() {
		for name, actual := range map[string]types.Value{"items": vector, "metadata": metadata} {
			t.Run(boundary+"/"+name, func(t *testing.T) {
				if got := mustString(t, argumentErrorActual(t, makeError(actual))); got != "<unsafe value>" {
					t.Fatalf("cyclic actual = %q, want <unsafe value>", got)
				}
			})
		}
	}
}
