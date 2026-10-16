package vm

import "testing"

func TestFnVariadicDestructureRestSlot(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "rest only reads first argument without destructuring it twice",
			source: `((fn [& [x]] x) [1 2] [3 4])`,
			want:   "[1 2]",
		},
		{
			name:   "required argument before destructured rest",
			source: `((fn [x & [y]] y) 1 2 3)`,
			want:   "2",
		},
		{
			name:   "nested rest pattern",
			source: `((fn [& [[x y]]] [x y]) [1 2] [3 4])`,
			want:   "[1 2]",
		},
		{
			name:   "fixed destructured argument before rest",
			source: `((fn [[x] & [y]] [x y]) [1] 2 3)`,
			want:   "[1 2]",
		},
		{
			name:   "empty rest reports ordinary missing item error",
			source: `(try ((fn [& [x]] x)) (catch [e] (ex-message e)))`,
			want:   `"nth: index 0 out of bounds for list of length 0"`,
		},
		{
			name:   "empty rest after required argument",
			source: `(try ((fn [x & [y]] [x y]) 1) (catch [e] (ex-message e)))`,
			want:   `"nth: index 0 out of bounds for list of length 0"`,
		},
		{
			name:   "ordinary rest symbol control",
			source: `((fn [x & ys] ys) 1 2 3)`,
			want:   "(2 3)",
		},
		{
			name:   "fixed destructured parameter control",
			source: `((fn [x [y]] [x y]) 1 [2])`,
			want:   "[1 2]",
		},
		{
			name:   "empty fixed destructured parameter control",
			source: `(try ((fn [[x]] x) '()) (catch [e] (ex-message e)))`,
			want:   `"nth: index 0 out of bounds for list of length 0"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.source, tt.want)
		})
	}
}

func TestSeqDestructureWithoutFixedItems(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "rest only shadows outer binding",
			source: `(let [xs :outer] (let [[& xs] [1 2]] xs))`,
			want:   "(1 2)",
		},
		{
			name:   "as only shadows outer binding",
			source: `(let [xs :outer] (let [[:as xs] [1 2]] xs))`,
			want:   "[1 2]",
		},
		{
			name:   "rest and as only bind sequence and original vector",
			source: `(let [[& xs :as all] [1 2]] [xs all])`,
			want:   "[(1 2) [1 2]]",
		},
		{
			name:   "empty rest only binds nil",
			source: `(let [xs :outer] (let [[& xs] []] xs))`,
			want:   "nil",
		},
		{
			name:   "empty as only preserves vector",
			source: `(let [xs :outer] (let [[:as xs] []] xs))`,
			want:   "[]",
		},
		{
			name:   "nested rest only",
			source: `(let [[[& xs]] [[1 2]]] xs)`,
			want:   "(1 2)",
		},
		{
			name:   "nested as only",
			source: `(let [[[:as xs]] [[1 2]]] xs)`,
			want:   "[1 2]",
		},
		{
			name:   "rest only parameter",
			source: `((fn [[& xs]] xs) [1 2])`,
			want:   "(1 2)",
		},
		{
			name:   "as only parameter",
			source: `((fn [[:as xs]] xs) [1 2])`,
			want:   "[1 2]",
		},
		{
			name:   "empty pattern still discards source once",
			source: `(let [count (atom 0)] (let [[] (do (swap! count inc) [1 2])] @count))`,
			want:   "1",
		},
		{
			name:   "fixed item with rest and as control",
			source: `(let [[x & xs :as all] [1 2 3]] [x xs all])`,
			want:   "[1 (2 3) [1 2 3]]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.source, tt.want)
		})
	}
}

func TestSeqDestructureGlobalShadowWithoutFixedItems(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "rest only let shadows global",
			source: `(do (def xs :global) [(let [[& xs] [1 2]] xs) xs])`,
			want:   "[(1 2) :global]",
		},
		{
			name:   "as only let shadows global",
			source: `(do (def xs :global) [(let [[:as xs] [1 2]] xs) xs])`,
			want:   "[[1 2] :global]",
		},
		{
			name:   "rest only parameter shadows global",
			source: `(do (def xs :global) [((fn [[& xs]] xs) [1 2]) xs])`,
			want:   "[(1 2) :global]",
		},
		{
			name:   "as only parameter shadows global",
			source: `(do (def xs :global) [((fn [[:as xs]] xs) [1 2]) xs])`,
			want:   "[[1 2] :global]",
		},
		{
			name:   "nested rest only shadows global",
			source: `(do (def xs :global) [(let [[[& xs]] [[1 2]]] xs) xs])`,
			want:   "[(1 2) :global]",
		},
		{
			name:   "nested as only shadows global",
			source: `(do (def xs :global) [(let [[[:as xs]] [[1 2]]] xs) xs])`,
			want:   "[[1 2] :global]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.source, tt.want)
		})
	}
}
