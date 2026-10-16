package vm

import (
	"fmt"
	"math"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestReadableDelimiterCharactersRoundTrip(t *testing.T) {
	runner := MustNewRunner()
	for _, ch := range "()[]{}\";,\\a日🙂 \n\t\r" {
		for _, shape := range []struct{ name, form string }{
			{"scalar", `%s`},
			{"vector", `[%s]`},
			{"list", `(list %s)`},
			{"map", `{:value %s}`},
			{"set", `#{%s}`},
			{"nested key", `[{%[1]s (list #{%[1]s})}]`},
		} {
			t.Run(fmt.Sprintf("%U/%s", ch, shape.name), func(t *testing.T) {
				character := fmt.Sprintf(`(first %q)`, string(ch))
				value := fmt.Sprintf(shape.form, character)
				source := fmt.Sprintf(`(let [value %s] (= value (read-string (pr-str value))))`, value)
				result, err := runner.Run(source)
				if err != nil {
					t.Fatalf("round trip of %U in %s: %v", ch, shape.name, err)
				}
				if result != atom.True {
					t.Fatalf("round trip changed %U in %s: %v", ch, shape.name, result)
				}
			})
		}
	}
}

func TestReadableFloatKindRoundTrip(t *testing.T) {
	runner := MustNewRunner()
	for _, expression := range []string{"0.0", "1.0", "-2.0", "-0.0", "(/ 6 2)", "1e20", "3.5"} {
		for _, shape := range []struct{ name, form, extract string }{
			{"scalar", `%s`, `%s`},
			{"vector", `[%s]`, `(first %s)`},
			{"list", `(list %s)`, `(first %s)`},
			{"map", `{:value %s}`, `(get %s :value)`},
			{"set", `#{%s}`, `(first %s)`},
			{"map key", `{%s :value}`, `(first (keys %s))`},
			{"nested", `{:value [(list %s)]}`, `(first (first (get %s :value)))`},
		} {
			t.Run(expression+"/"+shape.name, func(t *testing.T) {
				value := fmt.Sprintf(shape.form, expression)
				extracted := fmt.Sprintf(shape.extract, "restored")
				source := fmt.Sprintf(`(let [value %s restored (read-string (pr-str value))] [(float? %s) (int? %s) (= value restored)])`, value, extracted, extracted)
				result, err := runner.Run(source)
				if err != nil {
					t.Fatalf("round trip of %s in %s: %v", expression, shape.name, err)
				}
				if result.String() != "[true false true]" {
					t.Fatalf("round trip changed float kind: %s", result.String())
				}
			})
		}
	}
}

func TestReadableFloatPreservesSchemaAndNegativeZero(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(require '[doggo.schema :as schema])
		(let [s [:map [:value :float]] value {:value (/ 6 2)}]
		  [(schema/valid? s value) (schema/valid? s (read-string (pr-str value)))])`)
	if err != nil {
		t.Fatal(err)
	}
	if result.String() != "[true true]" {
		t.Fatalf("schema validity changed after persistence: %s", result.String())
	}
	result, err = runner.Run(`(read-string (pr-str -0.0))`)
	if err != nil {
		t.Fatal(err)
	}
	number, ok := result.(*atom.Number)
	if !ok || number.FloatVal == nil || !math.Signbit(*number.FloatVal) {
		t.Fatalf("negative float zero lost its kind or sign: %#v", result)
	}
}

func TestReadableIntegerKindsAndHumanFloatDisplay(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`[(int? (read-string (pr-str 3))) (str 3.0) (str (/ 6 2))]`)
	if err != nil {
		t.Fatal(err)
	}
	if result.String() != `[true "3" "3"]` {
		t.Fatalf("ordinary integer reading or human numeric display changed: %s", result.String())
	}
}

func TestReadablePrinterPreservesKeywordAndBoundedSequenceDisplay(t *testing.T) {
	runner := MustNewRunner()
	for _, value := range []string{`:value`, `{:value [:tag "text" nil]}`, `(with-meta [1.0] {:tag :data})`} {
		result, err := runner.Run(fmt.Sprintf(`(let [value %s] (= value (read-string (pr-str value))))`, value))
		if err != nil || result != atom.True {
			t.Fatalf("round trip of %s = %v (%v)", value, result, err)
		}
	}
	result, err := runner.Run(`(= (pr-str (range)) (str (range)))`)
	if err != nil || result != atom.True {
		t.Fatalf("bounded infinite sequence display changed: %v (%v)", result, err)
	}
	assertRunErrorContains(t, runner, `(read-string "\\")`, "invalid character literal")
}

func TestReadablePrinterPreservesNonfiniteAndEmptyValues(t *testing.T) {
	for _, value := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		number := atom.NewNumberFloat(value)
		if got := prStr(number); got != number.String() {
			t.Errorf("nonfinite representation changed from %s to %s", number.String(), got)
		}
	}
	for _, value := range []types.Value{(*coll.Map)(nil), (*coll.Vector)(nil), (*coll.List)(nil), (*coll.Set)(nil)} {
		if got := prStr(value); got != value.String() {
			t.Errorf("nil %T representation changed from %s to %s", value, value.String(), got)
		}
	}
}
