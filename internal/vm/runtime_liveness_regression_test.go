package vm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	stateagent "doggo/internal/agent"
	stateatom "doggo/internal/atom"
	"doggo/internal/stm"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type reentrantWatchKey struct {
	name     string
	onEquals func()
}

func (k *reentrantWatchKey) String() string         { return k.name }
func (k *reentrantWatchKey) Type() string           { return "reentrant-watch-key" }
func (k *reentrantWatchKey) Hash() uint32           { return 0xd06 }
func (k *reentrantWatchKey) IsSelfEvaluating() bool { return true }
func (k *reentrantWatchKey) Equals(other types.Value) bool {
	if k.onEquals != nil {
		k.onEquals()
	}
	otherKey, ok := other.(*reentrantWatchKey)
	return ok && k.name == otherKey.name
}

type timedRunResult struct {
	value string
	err   error
}

func runWithTimeout(t *testing.T, source string) timedRunResult {
	t.Helper()
	done := make(chan timedRunResult, 1)
	go func() {
		runner := MustNewRunner()
		value, err := runner.Run(source)
		result := timedRunResult{err: err}
		if value != nil {
			result.value = value.String()
		}
		done <- result
	}()

	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatalf("Run(%s) did not terminate", source)
		return timedRunResult{}
	}
}

func TestDerivedInfiniteSequencesDoNotHangValueOperations(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "filter map key",
			source: `(let [k (filter even? (range))] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "cons map key",
			source: `(let [k (cons -1 (range))] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "concat map key",
			source: `(let [k (concat [1] (range))] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "distinct mapped equality",
			source: `(= (map identity (range)) (map identity (range)))`,
			want:   "cannot compare distinct infinite sequences",
		},
		{
			name:   "filtered infinite source versus finite equality",
			source: `(= (filter (fn [_] false) (range)) [])`,
			want:   "cannot compare distinct infinite sequences",
		},
		{
			name:   "repeat map key",
			source: `(let [k (repeat 1)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "iterate map key",
			source: `(let [k (iterate inc 0)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "repeatedly map key",
			source: `(let [k (repeatedly (fn [] 1))] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "cycle map key",
			source: `(let [k (cycle [1 2])] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "distinct repeated equality",
			source: `(= (repeat 1) (repeat 1))`,
			want:   "cannot compare distinct infinite sequences",
		},
		{
			name:   "drop preserves provenance",
			source: `(let [source (map (fn [_] (throw (ex-info "source realized" {:kind :test}))) (repeat nil)) k (drop 1 source)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "keep preserves provenance",
			source: `(let [source (map (fn [_] (throw (ex-info "source realized" {:kind :test}))) (repeat nil)) k (keep identity source)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "remove preserves provenance",
			source: `(let [source (map (fn [_] (throw (ex-info "source realized" {:kind :test}))) (repeat nil)) k (remove identity source)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "map-indexed preserves provenance",
			source: `(let [source (map (fn [_] (throw (ex-info "source realized" {:kind :test}))) (repeat nil)) k (map-indexed (fn [_ x] x) source)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "reductions preserves provenance",
			source: `(let [source (map (fn [_] (throw (ex-info "source realized" {:kind :test}))) (repeat nil)) k (reductions + 0 source)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "keep-indexed preserves provenance",
			source: `(let [source (map (fn [_] (throw (ex-info "source realized" {:kind :test}))) (repeat nil)) k (keep-indexed (fn [_ x] x) source)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "take-nth preserves provenance",
			source: `(let [source (map (fn [_] (throw (ex-info "source realized" {:kind :test}))) (repeat nil)) k (take-nth 2 source)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "take-while bounds source work per node",
			source: `(let [k (take-while (fn [_] true) (range))] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "mapcat preserves provenance",
			source: `(let [source (map (fn [_] (throw (ex-info "source realized" {:kind :test}))) (repeat nil)) k (mapcat list source)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "pmap preserves provenance",
			source: `(let [source (map (fn [_] (throw (ex-info "source realized" {:kind :test}))) (repeat nil)) k (pmap identity source)] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "mapcat detects an infinite mapped collection",
			source: `(let [k (mapcat identity [(range)])] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
		{
			name:   "mapcat detects an unproductive infinite collection from a list",
			source: `(let [k (mapcat identity (list (filter (fn [_] false) (range))))] (get {k :ok} k))`,
			want:   "cannot hash transformed infinite sequence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runWithTimeout(t, tt.source)
			if result.err == nil || !strings.Contains(result.err.Error(), tt.want) {
				t.Fatalf("Run error = %v, want %q", result.err, tt.want)
			}
		})
	}
}

func TestFiniteLazySequenceVariantsRemainHashable(t *testing.T) {
	tests := []string{
		`(let [k (cycle [])] (get {k :ok} k))`,
		`(let [k (repeatedly 3 (fn [] 1))] (get {k :ok} k))`,
		`(let [k (take-while (fn [x] (< x 3)) (range))] (get {k :ok} k))`,
	}
	for _, source := range tests {
		result := runWithTimeout(t, source)
		if result.err != nil {
			t.Fatalf("Run(%s) error = %v", source, result.err)
		}
		if result.value != ":ok" {
			t.Fatalf("Run(%s) value = %s, want :ok", source, result.value)
		}
	}
}

func TestNestedLazySequenceProvenancePreventsValueOperationHangs(t *testing.T) {
	tests := []string{
		`(let [k (lazy-seq (filter (fn [_] false) (range)))] (get {k :ok} k))`,
		`(= (lazy-seq (filter (fn [_] false) (range))) [])`,
	}
	for _, source := range tests {
		result := runWithTimeout(t, source)
		if result.err == nil || !strings.Contains(result.err.Error(), "infinite sequence") {
			t.Fatalf("Run(%s) error = %v, want bounded infinite-sequence error", source, result.err)
		}
	}
}

func TestNestedLazySequenceErrorsReachValueOperations(t *testing.T) {
	tests := []string{
		`(let [k (lazy-seq (map inc ["bad"]))] (get {k :ok} k))`,
		`(= (lazy-seq (map inc ["bad"])) [])`,
		`(first (lazy-seq (map inc ["bad"])))`,
		`(doall (lazy-seq (map inc ["bad"])))`,
		`(= (take 1 (lazy-seq (map inc ["bad"]))) [])`,
	}
	for _, source := range tests {
		result := runWithTimeout(t, source)
		if result.err == nil || !strings.Contains(result.err.Error(), "inc: argument must be a number") {
			t.Fatalf("Run(%s) error = %v, want nested lazy evaluation error", source, result.err)
		}
	}
}

func TestFreshLazyAliasChainsDoNotBypassTraversalLimits(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`(letfn [(nest [] (lazy-seq (nest)))] (first (nest)))`, "infinite"},
		{`(letfn [(nest [] (lazy-seq (nest)))] (let [k (nest)] (get {k :ok} k)))`, "infinite"},
		{`(let [holder (atom nil)
		       value (lazy-seq @holder)]
		   (reset! holder value)
		   (get {value :ok} value))`, "infinite"},
	}
	for _, tt := range tests {
		result := runWithTimeout(t, tt.source)
		if result.err == nil || !strings.Contains(result.err.Error(), tt.want) {
			t.Fatalf("Run(%s) error = %v, want %q", tt.source, result.err, tt.want)
		}
	}
}

func TestMixedLazyAliasChainsShareTraversalLimit(t *testing.T) {
	for _, source := range []string{
		`(letfn [(nest [] (lazy-seq (drop 0 (nest))))] (first (nest)))`,
		`(letfn [(nest [] (lazy-seq (with-meta (nest) {})))] (first (nest)))`,
		`(letfn [(nest [] (lazy-seq (concat (nest))))] (first (nest)))`,
		`(letfn [(nest [] (lazy-seq (drop 0 (nest))))] (empty? (nest)))`,
	} {
		result := runWithTimeout(t, source)
		if result.err == nil || !strings.Contains(result.err.Error(), "sequence operation limit of 1000 lazy aliases exceeded") {
			t.Fatalf("Run(%s) error = %v, want bounded lazy-alias error", source, result.err)
		}
	}
}

func TestCollectionLazyAliasChainsRespectTraversalLimit(t *testing.T) {
	exactBoundary := fmt.Sprintf(
		`(empty? (reduce (fn [seq _] (drop 0 seq)) [1] (range 0 %d)))`,
		coll.LazySeqAliasLimit+1,
	)
	result := runWithTimeout(t, exactBoundary)
	if result.err != nil || result.value != "false" {
		t.Fatalf("Run(%s) = (%s, %v), want false", exactBoundary, result.value, result.err)
	}

	exceededBoundary := fmt.Sprintf(
		`(empty? (reduce (fn [seq _] (drop 0 seq)) [1] (range 0 %d)))`,
		coll.LazySeqAliasLimit+2,
	)
	result = runWithTimeout(t, exceededBoundary)
	want := fmt.Sprintf("sequence operation limit of %d lazy aliases exceeded", coll.LazySeqAliasLimit)
	if result.err == nil || !strings.Contains(result.err.Error(), want) {
		t.Fatalf("Run(%s) error = %v, want %q", exceededBoundary, result.err, want)
	}
}

func TestCollectionToVMLazyAliasChainsRespectTraversalLimit(t *testing.T) {
	source := func(aliasCount int) string {
		return fmt.Sprintf(
			`(empty? (drop 0 (reduce (fn [seq _] (lazy-seq seq)) [1] (range 0 %d))))`,
			aliasCount,
		)
	}

	exactBoundary := source(coll.LazySeqAliasLimit)
	result := runWithTimeout(t, exactBoundary)
	if result.err != nil || result.value != "false" {
		t.Fatalf("Run(%s) = (%s, %v), want false", exactBoundary, result.value, result.err)
	}

	exceededBoundary := source(coll.LazySeqAliasLimit + 1)
	result = runWithTimeout(t, exceededBoundary)
	want := fmt.Sprintf("sequence operation limit of %d lazy aliases exceeded", coll.LazySeqAliasLimit)
	if result.err == nil || !strings.Contains(result.err.Error(), want) {
		t.Fatalf("Run(%s) error = %v, want %q", exceededBoundary, result.err, want)
	}
}

func TestReduceKVPreservesMixedLazyAliasErrors(t *testing.T) {
	result := runWithTimeout(t, `(letfn [(nest [] (lazy-seq (drop 0 (nest))))]
	                                  (reduce-kv (fn [acc _ _] acc) 0 (nest)))`)
	if result.err == nil || !strings.Contains(result.err.Error(), "sequence operation limit of 1000 lazy aliases exceeded") {
		t.Fatalf("Run error = %v, want bounded lazy-alias error", result.err)
	}
}

func TestUnrealizedLazySelfEqualityDoesNotRealize(t *testing.T) {
	result := runWithTimeout(t, `(let [calls (atom 0)
	                                    value (lazy-seq (do (swap! calls inc) nil))]
	                                [(= value value) @calls])`)
	if result.err != nil {
		t.Fatalf("Run error = %v", result.err)
	}
	if result.value != "[true 0]" {
		t.Fatalf("Run value = %s, want [true 0]", result.value)
	}
}

func TestSourceDependentLazySequencesPreserveTraversalRisk(t *testing.T) {
	tests := []string{
		`(let [k (take 1 (filter (fn [_] false) (range)))] (get {k :ok} k))`,
		`(let [k (cycle (filter (fn [_] false) (range)))] (get {k :ok} k))`,
		`(let [k (take-while (fn [_] true) (filter (fn [_] false) (range)))] (get {k :ok} k))`,
	}
	for _, source := range tests {
		result := runWithTimeout(t, source)
		if result.err == nil || !strings.Contains(result.err.Error(), "infinite sequence") {
			t.Fatalf("Run(%s) error = %v, want bounded infinite-sequence error", source, result.err)
		}
	}
}

func TestFiniteTakeBeyondInspectionLimitRemainsAValue(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`(let [k (take 10001 (range))] (get {k :ok} k))`, ":ok"},
		{`(= (take 10001 (range)) (range 0 10001))`, "true"},
	}
	for _, tt := range tests {
		result := runWithTimeout(t, tt.source)
		if result.err != nil {
			t.Fatalf("Run(%s) error = %v", tt.source, result.err)
		}
		if result.value != tt.want {
			t.Fatalf("Run(%s) value = %s, want %s", tt.source, result.value, tt.want)
		}
	}
}

func TestFiniteTakeOfRealizedLazyAliasRemainsAValue(t *testing.T) {
	result := runWithTimeout(t, `(let [source (lazy-seq (range))
	                                    key (take 1 source)
	                                    table {key :ok}]
	                                [(= key [0]) (get table key)])`)
	if result.err != nil {
		t.Fatalf("Run error = %v", result.err)
	}
	if result.value != "[true :ok]" {
		t.Fatalf("Run value = %s, want [true :ok]", result.value)
	}
}

func TestNestedUnproductiveLazySourcesDoNotHangValueOperations(t *testing.T) {
	for _, source := range []string{
		`(let [source (lazy-seq (filter (fn [_] false) (range)))
		       key (take 1 source)]
		   (get {key :ok} key))`,
		`(let [source (lazy-seq (filter (fn [_] false) (range)))
		       key (take-while (fn [_] true) source)]
		   (get {key :ok} key))`,
	} {
		result := runWithTimeout(t, source)
		if result.err == nil || !strings.Contains(result.err.Error(), "cannot hash transformed infinite sequence") {
			t.Fatalf("Run(%s) error = %v, want bounded infinite-sequence error", source, result.err)
		}
	}
}

func TestKnownInfiniteAndFiniteSequencesCompareUnequal(t *testing.T) {
	tests := []string{
		`(= (range) [])`,
		`(= (repeat 1) [1 1])`,
	}
	for _, source := range tests {
		result := runWithTimeout(t, source)
		if result.err != nil {
			t.Fatalf("Run(%s) error = %v", source, result.err)
		}
		if result.value != "false" {
			t.Fatalf("Run(%s) value = %s, want false", source, result.value)
		}
	}
}

func TestTerminatingTakeWhileComparesUnequalToInfiniteSequence(t *testing.T) {
	result := runWithTimeout(t, `(= (take-while (fn [x] (< x 3)) (range)) (range))`)
	if result.err != nil {
		t.Fatalf("Run error = %v", result.err)
	}
	if result.value != "false" {
		t.Fatalf("Run value = %s, want false", result.value)
	}
}

func TestTakeZeroIgnoresAnUnreachableSourceError(t *testing.T) {
	result := runWithTimeout(t, `(let [source (lazy-seq (throw (ex-info "boom" {:kind :test})))]
	                                    (try (first source) (catch [e] nil))
	                                    (let [key (take 0 source)]
	                                      [(count key) (= key []) (get {key :ok} key)]))`)
	if result.err != nil {
		t.Fatalf("Run error = %v", result.err)
	}
	if result.value != "[0 true :ok]" {
		t.Fatalf("Run value = %s, want [0 true :ok]", result.value)
	}
}

func TestLongFiniteConsHashRemainsLinear(t *testing.T) {
	result := runWithTimeout(t, `(let [k (reduce (fn [s x] (cons x s)) (lazy-seq nil) (range 0 5001))]
	                                  (get {k :ok} k))`)
	if result.err != nil {
		t.Fatalf("Run error = %v", result.err)
	}
	if result.value != ":ok" {
		t.Fatalf("Run value = %s, want :ok", result.value)
	}
}

func TestProvenanceInspectionLimitFailsClosed(t *testing.T) {
	result := runWithTimeout(t, `(let [tail (filter (fn [_] false) (range))
	                                  k (reduce (fn [s x] (cons x s)) tail (range 0 10000))]
	                              (get {k :ok} k))`)
	if result.err == nil || !strings.Contains(result.err.Error(), "cannot hash transformed infinite sequence") {
		t.Fatalf("Run error = %v, want bounded provenance error", result.err)
	}
}

func TestTraversalLimitAllowsExactFiniteBoundary(t *testing.T) {
	result := runWithTimeout(t, `(let [k (reduce (fn [s x] (cons x s)) (lazy-seq nil) (range 0 10000))]
	                                  (get {k :ok} k))`)
	if result.err != nil {
		t.Fatalf("Run error = %v", result.err)
	}
	if result.value != ":ok" {
		t.Fatalf("Run value = %s, want :ok", result.value)
	}
}

func TestNewLazySeqRejectsNonBooleanInfiniteMarker(t *testing.T) {
	result := runWithTimeout(t, `(new-lazy-seq (fn [] nil) :yes)`)
	if result.err == nil || !strings.Contains(result.err.Error(), "infinite marker must be boolean") {
		t.Fatalf("Run error = %v, want infinite marker validation", result.err)
	}
}

func TestRealizedInfiniteTransformationPreservesEvaluationError(t *testing.T) {
	tests := []string{
		`(let [k (filter (fn [_] (throw (ex-info "predicate failed" {:kind :test}))) (range))]
		   (try (first k) (catch [e] nil))
		   (get {k :ok} k))`,
		`(let [k (filter (fn [_] (throw (ex-info "predicate failed" {:kind :test}))) (range))]
		   (try (first k) (catch [e] nil))
		   (= k k))`,
		`(let [s (filter (fn [_] (throw (ex-info "predicate failed" {:kind :test}))) (range))
		       k (concat s)]
		   (try (first s) (catch [e] nil))
		   (get {k :ok} k))`,
		`(let [s (filter (fn [_] (throw (ex-info "predicate failed" {:kind :test}))) (range))
		       k (cons -1 s)]
		   (try (first s) (catch [e] nil))
		   (get {k :ok} k))`,
	}
	for _, source := range tests {
		result := runWithTimeout(t, source)
		if result.err == nil || !strings.Contains(result.err.Error(), "predicate failed") {
			t.Fatalf("Run error = %v, want original lazy evaluation error", result.err)
		}
	}
}

func TestCycleDefersSourceRealization(t *testing.T) {
	result := runWithTimeout(t, `(let [s (cycle (lazy-seq (throw (ex-info "cycle source realized" {:kind :test}))))] :ok)`)
	if result.err != nil || result.value != ":ok" {
		t.Fatalf("constructing cycle = (%s, %v), want (:ok, nil)", result.value, result.err)
	}

	result = runWithTimeout(t, `(first (cycle (lazy-seq (throw (ex-info "cycle source realized" {:kind :test})))))`)
	if result.err == nil || !strings.Contains(result.err.Error(), "cycle source realized") {
		t.Fatalf("realizing cycle error = %v, want source error", result.err)
	}
}

func TestConcatSourcesAreSafeDuringRealization(t *testing.T) {
	collections := []types.Value{coll.NewList(), coll.NewList(atom.NewNumberInt(1))}
	for range 200 {
		seq := newConcatLazySeq(context.Background(), nil, collections, 0, nil)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = seq.First()
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = seq.Sources()
		}()
		close(start)
		wg.Wait()
	}
}

func TestWatchKeysCanReenterTheirTargetWithoutDeadlock(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "atom",
			source: `(let [a (atom 0)
			               f (fn [_ _ _ _] nil)
			               k (map (fn [x] (add-watch a :inner f) x) [1])]
			          (add-watch a k f)
			          :ok)`,
		},
		{
			name: "agent",
			source: `(let [a (agent 0)
			               f (fn [_ _ _ _] nil)
			               k (map (fn [x] (agent-error a) x) [1])]
			          (add-watch a k f)
			          :ok)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runWithTimeout(t, tt.source)
			if result.err != nil {
				t.Fatalf("Run error = %v", result.err)
			}
			if result.value != ":ok" {
				t.Fatalf("Run value = %s, want :ok", result.value)
			}
		})
	}
}

func TestWatchKeyEqualityCanReenterItsTargetWithoutDeadlock(t *testing.T) {
	type target struct {
		name  string
		add   func(types.Value) error
		close func()
	}

	a := stateatom.NewAtom(atom.NilValue)
	ag := stateagent.NewAgent(atom.NilValue)
	r := stm.NewRef(atom.NilValue)
	targets := []target{
		{
			name: "atom",
			add: func(key types.Value) error {
				return a.AddWatch(key, func(context.Context, types.Value, *stateatom.Atom, types.Value, types.Value) error { return nil })
			},
		},
		{
			name:  "agent",
			close: ag.Close,
			add: func(key types.Value) error {
				return ag.AddWatch(key, func(context.Context, types.Value, *stateagent.Agent, types.Value, types.Value) error { return nil })
			},
		},
		{
			name: "ref",
			add: func(key types.Value) error {
				return r.AddWatch(key, func(context.Context, types.Value, *stm.Ref, types.Value, types.Value) error { return nil })
			},
		},
	}

	for _, tt := range targets {
		t.Run(tt.name, func(t *testing.T) {
			if tt.close != nil {
				defer tt.close()
			}
			var once sync.Once
			var nestedErr error
			first := &reentrantWatchKey{name: "same"}
			first.onEquals = func() {
				once.Do(func() {
					nestedErr = tt.add(atom.NewKeyword("inner"))
				})
			}
			if err := tt.add(first); err != nil {
				t.Fatalf("initial AddWatch error = %v", err)
			}

			done := make(chan error, 1)
			go func() {
				done <- tt.add(&reentrantWatchKey{name: "same"})
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("reentrant AddWatch error = %v", err)
				}
				if nestedErr != nil {
					t.Fatalf("nested AddWatch error = %v", nestedErr)
				}
			case <-time.After(250 * time.Millisecond):
				t.Fatal("watch key equality deadlocked while reentering target")
			}
		})
	}
}

func TestRestartAgentRejectsMalformedOptions(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"missing option value", `(restart-agent (agent 0) 1 :clear-actions)`, "option key-value pairs"},
		{"non-keyword option", `(restart-agent (agent 0) 1 "clear-actions" true)`, "option name must be a keyword"},
		{"unknown option", `(restart-agent (agent 0) 1 :unknown true)`, "unsupported option"},
		{"non-boolean clear-actions", `(restart-agent (agent 0) 1 :clear-actions :yes)`, ":clear-actions must be boolean"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runWithTimeout(t, tt.source)
			if result.err == nil || !strings.Contains(result.err.Error(), tt.want) {
				t.Fatalf("Run error = %v, want %q", result.err, tt.want)
			}
		})
	}
}

func TestReferenceStringHandlesCycles(t *testing.T) {
	if kind := os.Getenv("DOGGO_CYCLE_STRING_HELPER"); kind != "" {
		debug.SetMaxStack(1 << 20)
		source := map[string]string{
			"atom":  `(do (def a (atom nil)) (reset! a a) a)`,
			"agent": `(do (def a (agent nil)) (restart-agent a a) a)`,
			"ref":   `(do (def r (ref nil)) (dosync (ref-set r r)) r)`,
		}[kind]
		runner := MustNewRunner()
		value, err := runner.Run(source)
		if err != nil {
			_, _ = fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		_, _ = fmt.Fprint(os.Stdout, value.String())
		return
	}

	for _, kind := range []string{"atom", "agent", "ref"} {
		t.Run(kind, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestReferenceStringHandlesCycles$")
			cmd.Env = append(os.Environ(), "DOGGO_CYCLE_STRING_HELPER="+kind)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = io.Discard
			if err := cmd.Run(); err != nil {
				t.Fatalf("cyclic %s String terminated the process: %v", kind, err)
			}
			if !strings.HasPrefix(stdout.String(), "#<") {
				t.Fatalf("cyclic %s String = %q, want reference representation", kind, stdout.String())
			}
		})
	}
}
