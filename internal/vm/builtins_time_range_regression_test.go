package vm

import (
	"fmt"
	"strings"
	"testing"
	stdtime "time"
)

func TestTimeExpandedYearEpochRoundTrips(t *testing.T) {
	tests := []struct {
		millis int64
		text   string
	}{
		{-62167219200001, "-0001-12-31T23:59:59.999Z"},
		{-62167219200000, "0000-01-01T00:00:00Z"},
		{253402300799999, "9999-12-31T23:59:59.999Z"},
		{253402300800000, "10000-01-01T00:00:00Z"},
		{9007199254740993, "287396-10-12T08:59:00.993Z"},
		{9223372036854775807, "292278994-08-17T07:12:55.807Z"},
		{-9223372036854775808, "-292275055-05-16T16:47:04.192Z"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.millis), func(t *testing.T) {
			runner := MustNewRunner()
			value, err := runner.Run(fmt.Sprintf(`
				(require '[doggo.time :as time])
				(let [value (time/from-epoch-millis %d)]
				  [(time/epoch-millis value)
				   (time/format value)
				   (time/format value {:offset :preserve})])`, tt.millis))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := value.String(), fmt.Sprintf("[%d %q %q]", tt.millis, tt.text, tt.text); got != want {
				t.Fatalf("round trip = %s, want %s", got, want)
			}
		})
	}
}

func TestTimeExpandedYearOperations(t *testing.T) {
	for _, millis := range []int64{-9223372036854775808, -62167219200001, 253402300799999, 9223372036854775806} {
		t.Run(fmt.Sprint(millis), func(t *testing.T) {
			runner := MustNewRunner()
			value, err := runner.Run(fmt.Sprintf(`
				(require '[doggo.time :as time])
				(let [base (time/from-epoch-millis %d)
				      next (time/plus base (time/millis 1))]
				  [(time/epoch-millis next)
				   (time/epoch-millis (time/minus next (time/millis 1)))
				   (time/in-millis (time/minus next base))
				   (time/compare base next)
				   (time/before? base next)
				   (time/after? next base)])`, millis))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := value.String(), fmt.Sprintf("[%d %d 1 -1 true true]", millis+1, millis); got != want {
				t.Fatalf("operations = %s, want %s", got, want)
			}
		})
	}
}

func TestTimeNanosecondsSurviveValidationAndFormatting(t *testing.T) {
	tests := []struct {
		input  string
		utc    string
		millis int64
	}{
		{"2024-01-02T03:04:05.123456789Z", "2024-01-02T03:04:05.123456789Z", 1704164645123},
		{"1969-12-31T23:59:59.999999999Z", "1969-12-31T23:59:59.999999999Z", -1},
		{"9999-12-31T23:30:00.123456789-01:00", "10000-01-01T00:30:00.123456789Z", 253402302600123},
		{"0000-01-01T00:30:00.123456789+01:00", "-0001-12-31T23:30:00.123456789Z", -62167220999877},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			runner := MustNewRunner()
			value, err := runner.Run(fmt.Sprintf(`
				(require '[doggo.instant :as instant])
				(require '[doggo.time :as time])
				(let [value (instant/parse-timestamp %q)]
				  [(time/epoch-millis value)
				   (time/format value)
				   (time/format value {:offset :preserve})
				   (time/format value {:offset-seconds 0})])`, tt.input))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := value.String(), fmt.Sprintf("[%d %q %q %q]", tt.millis, tt.utc, tt.input, tt.utc); got != want {
				t.Fatalf("nanosecond rendering = %s, want %s", got, want)
			}
		})
	}
}

func TestTimeNowNanosecondsSurviveValidationAndFormatting(t *testing.T) {
	oldNow := timeBuiltinNow
	t.Cleanup(func() { timeBuiltinNow = oldNow })
	timeBuiltinNow = func() stdtime.Time {
		return stdtime.Date(2024, 1, 2, 3, 4, 5, 123456789, stdtime.UTC)
	}
	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.time :as time])
		(let [value (time/now)]
		  [(time/epoch-millis value)
		   (time/format value)
		   (time/format value {:offset-seconds 3600})])`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), `[1704164645123 "2024-01-02T03:04:05.123456789Z" "2024-01-02T04:04:05.123456789+01:00"]`; got != want {
		t.Fatalf("now rendering = %s, want %s", got, want)
	}
}

func TestTimeExpandedYearEndpointNanoseconds(t *testing.T) {
	tests := []struct {
		text   string
		millis int64
	}{
		{"292278994-08-17T07:12:55.807999999Z", 9223372036854775807},
		{"-292275055-05-16T16:47:04.192000001Z", -9223372036854775808},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			runner := MustNewRunner()
			value, err := runner.Run(fmt.Sprintf(`
				(require '[doggo.time :as time])
				(let [value {:instant %q :epoch-millis %d :offset-seconds 0}]
				  [(time/epoch-millis value) (time/format value {:offset :preserve})])`, tt.text, tt.millis))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := value.String(), fmt.Sprintf("[%d %q]", tt.millis, tt.text); got != want {
				t.Fatalf("endpoint = %s, want %s", got, want)
			}
		})
	}
}

func TestTimeExpandedYearRejectsInvalidInstantMaps(t *testing.T) {
	tests := []struct {
		name  string
		value string
		field string
	}{
		{"malformed date", `{:instant "10000-02-30T00:00:00Z"}`, ":instant"},
		{"noncanonical year", `{:instant "010000-01-01T00:00:00Z"}`, ":instant"},
		{"overflowing year", `{:instant "9999999999999999999999-01-01T00:00:00Z"}`, ":instant"},
		{"inconsistent epoch before missing offset", `{:instant "10000-01-01T00:00:00Z" :epoch-millis 0}`, ":epoch-millis"},
		{"missing offset", `{:instant "10000-01-01T00:00:00Z" :epoch-millis 253402300800000}`, ":offset-seconds"},
		{"wrapped epoch alias", `{:instant "1000000000-01-01T00:00:00Z" :epoch-millis -5336598314638303232 :offset-seconds 0}`, ":epoch-millis"},
		{"past maximum millisecond", `{:instant "292278994-08-17T07:12:55.808Z" :epoch-millis -9223372036854775808 :offset-seconds 0}`, ":epoch-millis"},
		{"before minimum millisecond", `{:instant "-292275055-05-16T16:47:04.191999999Z" :epoch-millis 9223372036854775807 :offset-seconds 0}`, ":epoch-millis"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			value, err := runner.Run(`
				(require '[doggo.time :as time])
				(try (time/epoch-millis ` + tt.value + `)
				  (catch [e] [(:kind (ex-data e)) (:function (ex-data e)) (ex-message e)]))`)
			if err != nil {
				t.Fatal(err)
			}
			wantPrefix := `[:invalid-argument doggo.time/epoch-millis "doggo.time instant must have ` + tt.field + `, got `
			if got := value.String(); !strings.HasPrefix(got, wantPrefix) {
				t.Fatalf("validation = %s, want prefix %s", got, wantPrefix)
			}
		})
	}
}

func TestTimePublicTimestampParserStillRejectsExpandedYears(t *testing.T) {
	for _, input := range []string{"10000-01-01T00:00:00Z", "-0001-12-31T23:59:59Z"} {
		t.Run(input, func(t *testing.T) {
			runner := MustNewRunner()
			value, err := runner.Run(fmt.Sprintf(`
				(require '[doggo.instant :as instant])
				(try (instant/parse-timestamp %q)
				  (catch [e] [(:kind (ex-data e)) (:function (ex-data e))]))`, input))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := value.String(), `[:invalid-argument doggo.instant/parse-timestamp]`; got != want {
				t.Fatalf("public parser = %s, want %s", got, want)
			}
		})
	}
}
