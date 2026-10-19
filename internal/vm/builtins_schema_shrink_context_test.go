package vm

import "testing"

func TestSchemaShrinkKeepsPredicateContext(t *testing.T) {
	for _, tt := range []struct {
		name, source, want string
	}{
		{"failing partial property", `(let [r (check (fn [x] false) [:predicate (partial > 10)] {:seed 1 :iterations 1 :generator-overrides {:predicate 5}})] [(:pass? r) (:failing-input r) (:shrunk-input r) (:shrink-steps r) (:kind (:failure r))])`, `[false 5 5 [] :return-false]`},
		{"nested partial tuple", `(let [r (check (fn [x] false) [:tuple [:predicate (partial > 10)]] {:seed 1 :iterations 1 :generator-overrides {:predicate 5}})] [(:pass? r) (:failing-input r) (:shrunk-input r) (:shrink-steps r)])`, `[false [5] [5] []]`},
		{"nested partial map vector", `(let [r (check (fn [x] false) [:map [:items [:vector [:predicate (partial > 10)]]]] {:seed 1 :size 2 :iterations 1 :generator-overrides {:predicate 5}})] [(:pass? r) (:failing-input r) (:shrink-steps r)])`, `[false {:items [5]} []]`},
		{"direct partial shrink", `(try (shrink [:predicate (partial > 10)] 5) (catch [e] (:reason (ex-data e))))`, `"unsupported shrink schema kind"`},
		{"direct partial shrink-seq", `(try (shrink-seq [:predicate (partial > 10)] 5 3) (catch [e] (:reason (ex-data e))))`, `"unsupported shrink schema kind"`},
		{"direct nested partial", `(try (shrink [:tuple [:predicate (partial > 10)]] [5]) (catch [e] [(:reason (ex-data e)) (:path (ex-data e))]))`, `["unsupported shrink schema kind" [0]]`},
		{"or branch respects partial", `(try (shrink-seq [:or [:predicate (partial > 10)] :int] 5 3) (catch [e] (:reason (ex-data e))))`, `"unsupported shrink schema kind"`},
		{"valid and generation control", `(let [s [:predicate (partial > 10)]] [(valid? s 5) (valid? s 12) (generate s {:generator-overrides {:predicate 5}})])`, `[true false 5]`},
		{"passing property control", `(:pass? (check (fn [x] true) [:predicate (partial > 10)] {:seed 1 :iterations 1 :generator-overrides {:predicate 5}}))`, `true`},
		{"zero shrink limit control", `(let [r (check (fn [x] false) [:predicate (partial > 10)] {:seed 1 :iterations 1 :shrink-limit 0 :generator-overrides {:predicate 5}})] [(:pass? r) (:shrink-steps r)])`, `[false []]`},
		{"ordinary fn control", `(let [r (check (fn [x] false) [:predicate (fn [x] (> 10 x))] {:seed 1 :iterations 1 :generator-overrides {:predicate 5}})] [(:pass? r) (:shrunk-input r) (:shrink-steps r)])`, `[false 5 []]`},
		{"invalid partial value remains rejected", `(try (shrink [:predicate (partial > 10)] 12) (catch [e] (:reason (ex-data e))))`, `"value does not satisfy schema for shrinking"`},
		{"invalid partial shrink-seq remains rejected", `(try (shrink-seq [:predicate (partial > 10)] 12 3) (catch [e] (:reason (ex-data e))))`, `"value does not satisfy schema for shrinking"`},
		{"zero count skips validation", `(shrink-seq [:predicate (partial > 10)] 12 0)`, `[]`},
		{"negative count rejected first", `(try (shrink-seq [:predicate (partial > 10)] 12 -1) (catch [e] (:reason (ex-data e))))`, `"malformed shrink count"`},
		{"supported lazy shrink control", `[(type-name (shrink :int 10)) (doall (take 3 (shrink :int 10))) (shrink-seq :int 10 3)]`, `["lazy-seq" (0 5 9) [0 5 9]]`},
		{"supported property shrinks control", `(let [r (check (fn [x] false) :int {:seed 1 :iterations 1 :generator-overrides {:int 5}})] [(:pass? r) (:shrunk-input r) (:shrink-steps r)])`, `[false 0 [0]]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, mustNewSchemaRunner(t), tt.source, tt.want)
		})
	}
}
