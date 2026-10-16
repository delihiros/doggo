package vm

import "testing"

func TestLetfnLocalLifetime(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "return function to an outer binding",
			source: `(let [x 42 f (letfn [(f [] x)] f)] (f))`,
			want:   "42",
		},
		{
			name:   "later number binding replaces dead cell",
			source: `(do (letfn [(f [] 42)] (f)) (let [x 7] (+ x 1)))`,
			want:   "8",
		},
		{
			name:   "later binding cannot change an escaped closure",
			source: `(let [saved (atom nil)] (letfn [(f [] 1)] (reset! saved (fn [] (f)))) (let [x 2] x) (@saved))`,
			want:   "1",
		},
		{
			name:   "ordinary returned closure control",
			source: `(let [x 42 f (let [f (fn [] x)] f)] (f))`,
			want:   "42",
		},
		{
			name:   "ordinary sequential binding control",
			source: `(do (let [f (fn [] 42)] (f)) (let [x 7] (+ x 1)))`,
			want:   "8",
		},
		{
			name:   "ordinary escaped closure control",
			source: `(let [saved (atom nil)] (let [f (fn [] 1)] (reset! saved (fn [] (f)))) (let [x 2] x) (@saved))`,
			want:   "1",
		},
		{
			name:   "later vector destructuring replaces dead cell",
			source: `(do (letfn [(f [] 42)] (f)) (let [[x] [7]] (+ x 1)))`,
			want:   "8",
		},
		{
			name:   "later map destructuring replaces dead cell",
			source: `(do (letfn [(f [] 42)] (f)) (let [{:keys [x]} {:x 7}] (+ x 1)))`,
			want:   "8",
		},
		{
			name:   "later loop binding replaces dead cell",
			source: `(do (letfn [(f [] 42)] (f)) (loop [n 0] (if (< n 2) (recur (inc n)) n)))`,
			want:   "2",
		},
		{
			name:   "sequential letfn cannot change an escaped function",
			source: `(let [saved (atom nil)] (letfn [(f [] 1)] (reset! saved (fn [] (f)))) (letfn [(g [] 2)] (g)) (@saved))`,
			want:   "1",
		},
		{
			name:   "escaped mutually recursive functions keep their captures",
			source: `(let [offset 42 f (letfn [(even? [n] (if (= n 0) offset (odd? (dec n)))) (odd? [n] (if (= n 0) 0 (even? (dec n))))] even?)] (f 4))`,
			want:   "42",
		},
		{
			name: "recur rebinding cannot change an escaped function",
			source: `(let [saved (atom nil)]
			  (loop [f (letfn [(g [] 1)] g) n 0]
			    (if (= n 0)
			      (do (reset! saved (fn [] (f))) (recur (fn [] 2) 1))
			      (@saved))))`,
			want: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, MustNewRunner(), tt.source, tt.want)
		})
	}
}
