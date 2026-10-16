package vm

import "testing"

func TestLetfnFunctionsAreOrdinaryValues(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "function predicates agree in local and nested positions",
			source: `(letfn [(f [x] (inc x))] [(fn? f) (ifn? f) ((fn [] [(fn? f) (ifn? f)]))])`,
			want:   `[true true [true true]]`,
		},
		{
			name:   "apply accepts a local function",
			source: `(letfn [(f [x y] (+ x y))] (apply f [2 3]))`,
			want:   `5`,
		},
		{
			name:   "map accepts a local function",
			source: `(letfn [(f [x] (inc x))] (vec (map f [1 2 3])))`,
			want:   `[2 3 4]`,
		},
		{
			name:   "partial accepts a local function",
			source: `(letfn [(f [x y] (+ x y))] ((partial f 2) 3))`,
			want:   `5`,
		},
		{
			name:   "swap callback accepts a local function",
			source: `(let [a (atom 1)] (letfn [(f [x] (+ x 4))] (swap! a f)))`,
			want:   `5`,
		},
		{
			name: "watch callback accepts a local function",
			source: `(let [a (atom 1) observed (atom nil)]
			  (letfn [(watch [key target old new] (reset! observed [key old new]))]
			    (add-watch a :change watch)
			    (reset! a 5)
			    @observed))`,
			want: `[:change 1 5]`,
		},
		{
			name: "escaped collection stores function values",
			source: `(let [saved (letfn [(f [x] (+ x 4))] [f {:f f} (list f)])]
			  [(fn? (first saved))
			   (apply (first saved) [1])
			   (apply (:f (nth saved 1)) [2])
			   (apply (first (nth saved 2)) [3])])`,
			want: `[true 5 6 7]`,
		},
		{
			name: "escaped mutually recursive values survive slot reuse",
			source: `(let [saved (letfn [(even [n] (if (zero? n) true (odd (dec n))))
			                            (odd [n] (if (zero? n) false (even (dec n))))]
			                       [even odd])]
			  (letfn [(replacement [] 99)] (replacement))
			  [(apply (first saved) [6]) (apply (nth saved 1) [5])])`,
			want: `[true true]`,
		},
		{
			name: "nested transitive captures retain mutual recursion",
			source: `(let [offset 42
			              saved (letfn [(f [n] (if (zero? n) offset (g (dec n))))
			                            (g [n] (if (zero? n) 0 (f (dec n))))]
			                      ((fn [] (fn [] [f g]))))]
			  [(apply (first (saved)) [4]) (apply (nth (saved) 1) [3])])`,
			want: `[42 42]`,
		},
		{
			name: "edn default callback accepts a local function",
			source: `(require '[doggo.edn :as edn])
			(letfn [(fallback [tag value] [tag value])]
			  (edn/read-string "#custom 7" {:default fallback}))`,
			want: `[custom 7]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, MustNewRunner(), tt.source, tt.want)
		})
	}
}
