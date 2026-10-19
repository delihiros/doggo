package vm

import "testing"

func TestSchemaGenerationKeepsPredicateContext(t *testing.T) {
	tests := []struct {
		name, source, want string
	}{
		{"partial fixed override", `(let [s [:predicate (partial > 10)]] [(valid? s 5) (generate s {:generator-overrides {:predicate 5}})])`, `[true 5]`},
		{"partial sample", `(sample [:predicate (partial > 10)] 2 {:generator-overrides {:predicate 5}})`, `[5 5]`},
		{"nested tuple", `(generate [:tuple [:predicate (partial > 10)]] {:generator-overrides {:predicate 5}})`, `[5]`},
		{"nested map vector", `(generate [:map [:items [:vector [:predicate (partial > 10)]]]] {:size 2 :generator-overrides {:predicate 5}})`, `{:items [5]}`},
		{"callable override", `(generate [:predicate (partial > 10)] {:generator-overrides {:predicate (fn [] 5)}})`, `5`},
		{"partial under and override", `(generate [:and :int [:predicate (partial > 10)]] {:generator-overrides {:and 5}})`, `5`},
		{"check shares generation context", `(:pass? (check pos? [:predicate (partial > 10)] {:iterations 2 :generator-overrides {:predicate 5}}))`, `true`},
		{"ordinary builtin control", `(generate [:predicate pos?] {:generator-overrides {:predicate 5}})`, `5`},
		{"closure control", `(generate [:predicate (fn [x] (> 10 x))] {:generator-overrides {:predicate 5}})`, `5`},
		{"wrapped partial control", `(let [p (partial > 10)] (generate [:predicate (fn [x] (p x))] {:generator-overrides {:predicate 5}}))`, `5`},
		{"comp control", `(generate [:predicate (comp pos? identity)] {:generator-overrides {:predicate 5}})`, `5`},
		{"invalid override remains rejected", `(try (generate [:predicate (partial > 10)] {:generator-overrides {:predicate 12}}) (catch [e] (:reason (ex-data e))))`, `"invalid generator override result"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, mustNewSchemaRunner(t), tt.source, tt.want)
		})
	}
}
