package vm

import "testing"

func TestFinallyRecurDiscardsAbandonedArguments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "small finalizer recur remains supported",
			source: `(loop [n 0] (try (if (< n 3) (recur (inc n)) n)
			  (finally (if (< n 3) (recur (inc n)) nil))))`,
			want: "3",
		},
		{
			name: "finalizer recur exceeds stack capacity without growing operands",
			source: `(loop [n 0] (try (if (< n 3000) (recur (inc n)) n)
			  (finally (if (< n 3000) (recur (inc n)) nil))))`,
			want: "3000",
		},
		{
			name: "replacement recur keeps all new arguments in order",
			source: `(loop [n 0 value 0]
			  (try (if (< n 3000) (recur (inc n) -1) [n value])
			    (finally (if (< n 3000) (recur (inc n) (+ value 2)) nil))))`,
			want: "[3000 6000]",
		},
		{
			name: "contained finalizer loop preserves pending outer recur arguments",
			source: `(loop [n 0 value 10]
			  (try (if (< n 3) (recur (inc n) (+ value 10)) [n value])
			    (finally (loop [i 0] (if (< i 3) (recur (inc i)) i)))))`,
			want: "[3 40]",
		},
		{
			name: "contained finalizer loop can replace its own pending arguments",
			source: `(loop [n 0 value 10]
			  (try (if (< n 2) (recur (inc n) (+ value 10)) [n value])
			    (finally (loop [i 0]
			      (try (if (< i 3000) (recur (inc i)) i)
			        (finally (if (< i 3000) (recur (inc i)) nil)))))))`,
			want: "[2 30]",
		},
		{
			name: "nested finalizers can each replace a pending recur",
			source: `(loop [n 0]
			  (try (try (if (< n 3000) (recur (inc n)) n)
			    (finally (if (< n 3000) (recur (inc n)) nil)))
			    (finally (if (< n 3000) (recur (inc n)) nil))))`,
			want: "3000",
		},
		{
			name: "replacement arguments evaluate before the remaining cleanup",
			source: `(let [seen (atom []) result
			  (loop [n 0]
			    (try (try (if (= n 0) (recur (do (swap! seen conj :original) 1)) n)
			      (finally (if (= n 0) (recur (do (swap! seen conj :replacement) 2)) nil)))
			      (finally (swap! seen conj n))))]
			  [result @seen])`,
			want: "[2 [:original :replacement 0 2]]",
		},
		{
			name: "replacement argument exception reaches the remaining handler",
			source: `(let [seen (atom []) result
			  (loop [n 0]
			    (try (try (recur 1)
			      (finally (recur (throw (ex-info "replacement" {})))))
			      (catch [e] (ex-message e))
			      (finally (swap! seen conj n))))]
			  [result @seen])`,
			want: `["replacement" [0]]`,
		},
		{
			name: "caught replacement cleanup failure preserves the original recur",
			source: `(let [seen (atom []) result
			  (loop [n 0]
			    (try (if (= n 0) (recur 1) n)
			      (finally (if (= n 0)
			        (try (try (recur 2) (finally (throw (ex-info "cleanup" {}))))
			          (catch [e] (swap! seen conj :caught))) nil))))]
			  [result @seen])`,
			want: "[1 [:caught]]",
		},
		{
			name: "nested function has no inherited pending arguments",
			source: `(loop [n 0]
			  (try (if (= n 0) (recur 1) n)
			    (finally ((fn [] (loop [i 0]
			      (try (if (< i 3000) (recur (inc i)) i)
			        (finally (if (< i 3000) (recur (inc i)) nil)))))))))`,
			want: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, MustNewRunner(), tt.source, tt.want)
		})
	}
}
