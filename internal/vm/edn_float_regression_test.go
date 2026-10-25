package vm

import (
	"context"
	"fmt"
	"math"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestEDNFloatKindRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   float64
		encoded string
	}{
		{"zero", 0, "0.0"},
		{"integral", 1, "1.0"},
		{"negative integral", -2, "-2.0"},
		{"fractional", 3.5, "3.5"},
		{"positive exponent", 1e20, "1e+20"},
		{"negative exponent", 1e-9, "1e-09"},
		{"negative zero", math.Copysign(0, -1), "-0.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := ednWriteString(atom.NewNumberFloat(test.value))
			if err != nil {
				t.Fatal(err)
			}
			if encoded != test.encoded {
				t.Errorf("encoded float = %q, want %q", encoded, test.encoded)
			}
			restored, err := readEDNString(context.Background(), encoded, ednReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			number, ok := restored.(*atom.Number)
			if !ok || number.IntVal != nil || number.FloatVal == nil {
				t.Fatalf("round trip changed float kind: %#v", restored)
			}
			if math.Float64bits(*number.FloatVal) != math.Float64bits(test.value) {
				t.Fatalf("round trip changed float value or sign: %v -> %v", test.value, *number.FloatVal)
			}
		})
	}
}

func TestEDNFloatNestedSchemaRoundTrip(t *testing.T) {
	runner := MustNewRunner()
	for _, shape := range []struct{ name, form, extract string }{
		{"scalar", `%s`, `%s`},
		{"vector", `[%s]`, `(first %s)`},
		{"list", `(list %s)`, `(first %s)`},
		{"map", `{:value %s}`, `(get %s :value)`},
		{"set", `#{%s}`, `(first %s)`},
		{"map key", `{%s :value}`, `(first (keys %s))`},
		{"nested", `{:value [(list #{%s})]}`, `(first (first (first (get %s :value))))`},
	} {
		t.Run(shape.name, func(t *testing.T) {
			value := fmt.Sprintf(shape.form, "(/ 6 2)")
			extracted := fmt.Sprintf(shape.extract, "restored")
			source := fmt.Sprintf(`
				(require '[doggo.edn :as edn] '[doggo.schema :as schema])
				(let [value %s restored (edn/read-string (edn/write-string value))]
				  [(float? %s) (int? %s) (= value restored)
				   (schema/valid? :float %s)])`, value, extracted, extracted, extracted)
			result, err := runner.Run(source)
			if err != nil {
				t.Fatal(err)
			}
			if result.String() != "[true false true true]" {
				t.Fatalf("round trip changed nested float kind or schema validity: %s", result.String())
			}
		})
	}
	result, err := runner.Run(`
		(let [s [:map [:value :float]] value {:value 1.0}]
		  [(schema/valid? s value)
		   (schema/valid? s (edn/read-string (edn/write-string value)))])`)
	if err != nil {
		t.Fatal(err)
	}
	if result.String() != "[true true]" {
		t.Fatalf("map schema validity changed after EDN round trip: %s", result.String())
	}
}

func TestEDNIntegerKindRoundTrip(t *testing.T) {
	for _, value := range []types.Number{
		atom.NewNumberInt(0),
		atom.NewNumberInt(-2),
		atom.NewNumberInt(math.MaxInt64),
		atom.NewNumberInt(math.MinInt64),
		coll.RangeInt(3),
		coll.RangeInt(math.MaxInt64),
	} {
		t.Run(fmt.Sprintf("%T/%s", value, value.String()), func(t *testing.T) {
			encoded, err := ednWriteString(value)
			if err != nil {
				t.Fatal(err)
			}
			if encoded != value.String() {
				t.Fatalf("integer representation changed: %q", encoded)
			}
			restored, err := readEDNString(context.Background(), encoded, ednReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			number, ok := restored.(types.Number)
			if !ok {
				t.Fatalf("round trip changed number kind: %#v", restored)
			}
			got, isInteger := types.Int64Value(number)
			want, _ := types.Int64Value(value)
			if !isInteger || got != want {
				t.Fatalf("round trip changed integer kind or value: %#v", restored)
			}
		})
	}
}

func TestEDNNonfiniteFloatRoundTrip(t *testing.T) {
	for _, test := range []struct {
		value   float64
		encoded string
	}{
		{math.NaN(), "##NaN"},
		{math.Inf(1), "##Inf"},
		{math.Inf(-1), "##-Inf"},
	} {
		t.Run(test.encoded, func(t *testing.T) {
			encoded, err := ednWriteString(atom.NewNumberFloat(test.value))
			if err != nil {
				t.Fatal(err)
			}
			if encoded != test.encoded {
				t.Fatalf("nonfinite representation changed: %q", encoded)
			}
			restored, err := readEDNString(context.Background(), encoded, ednReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			number, ok := restored.(*atom.Number)
			if !ok || number.IntVal != nil || number.FloatVal == nil {
				t.Fatalf("round trip changed nonfinite float kind: %#v", restored)
			}
			if math.IsNaN(test.value) {
				if !math.IsNaN(*number.FloatVal) {
					t.Fatalf("round trip changed NaN: %v", *number.FloatVal)
				}
			} else if *number.FloatVal != test.value {
				t.Fatalf("round trip changed infinity: %v -> %v", test.value, *number.FloatVal)
			}
		})
	}
}
