package vm

import "testing"

func TestRecurUnwindsTryRegions(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "completed try cannot catch a later exception",
			source: `(let [seen (atom [])] (try (do (loop [n 0] (try (if (= n 0) (recur 1) :done) (catch [e] (swap! seen conj :stale)))) (swap! seen conj :after) (throw (ex-info "late" {}))) (catch [e] (swap! seen conj :outer))) @seen)`,
			want:   "[:after :outer]",
		},
		{
			name:   "recur outside a completed try",
			source: `(let [seen (atom [])] (try (do (loop [n 0] (try :body (catch [e] (swap! seen conj :stale))) (if (= n 0) (recur 1) :done)) (swap! seen conj :after) (throw (ex-info "late" {}))) (catch [e] (swap! seen conj :outer))) @seen)`,
			want:   "[:after :outer]",
		},
		{
			name:   "contained loop retains its surrounding handler",
			source: `(try (do (loop [n 0] (if (= n 0) (recur 1) :done)) (throw (ex-info "after" {}))) (catch [e] :retained))`,
			want:   ":retained",
		},
		{
			name:   "only crossed nested handlers are removed",
			source: `(let [seen (atom [])] (try (do (loop [n 0] (try (try (if (= n 0) (recur 1) :done) (catch [e] (swap! seen conj :inner))) (catch [e] (swap! seen conj :middle)))) (swap! seen conj :after) (throw (ex-info "late" {}))) (catch [e] (swap! seen conj :outer))) @seen)`,
			want:   "[:after :outer]",
		},
		{
			name:   "recur arguments still use the active handler",
			source: `(loop [n 0] (try (recur (throw (ex-info "argument" {}))) (catch [e] (ex-message e))))`,
			want:   `"argument"`,
		},
		{
			name:   "finally sees old bindings after arguments evaluate",
			source: `(let [seen (atom []) result (loop [n 0] (try (if (= n 0) (recur (do (swap! seen conj :argument) 1)) n) (finally (swap! seen conj n))))] [result @seen])`,
			want:   "[1 [:argument 0 1]]",
		},
		{
			name:   "nested finally runs once per exit from inner to outer",
			source: `(let [seen (atom [])] (loop [n 0] (try (try (if (= n 0) (recur 1) :done) (finally (swap! seen conj :inner))) (finally (swap! seen conj :outer)))) @seen)`,
			want:   "[:inner :outer :inner :outer]",
		},
		{
			name:   "finally throw bypasses its catch and cancels recur",
			source: `(let [seen (atom [])] (try (loop [n 0] (try (if (= n 0) (recur 1) :done) (catch [e] (swap! seen conj :stale)) (finally (swap! seen conj n) (throw (ex-info "finally" {}))))) (catch [e] (swap! seen conj (ex-message e)))) @seen)`,
			want:   `[0 "finally"]`,
		},
		{
			name:   "finally runtime error propagates through remaining outer finally",
			source: `(let [seen (atom [])] (try (loop [n 0] (try (try (if (= n 0) (recur 1) :done) (finally (swap! seen conj n) (/ 1 0))) (finally (swap! seen conj :outer)))) (catch [e] (swap! seen conj :caught))) @seen)`,
			want:   "[0 :outer :caught]",
		},
		{
			name:   "finally resolves names outside inner lexical bindings",
			source: `(let [x :outer seen (atom [])] (loop [n 0] (try (let [x :inner] (if (= n 0) (recur 1) :done)) (finally (swap! seen conj x)))) @seen)`,
			want:   "[:outer :outer]",
		},
		{
			name:   "nested function tracks its own try regions",
			source: `(let [seen (atom [])] (try (do ((fn [] (loop [n 0] (try (if (= n 0) (recur 1) :done) (catch [e] (swap! seen conj :stale)))))) (throw (ex-info "after" {}))) (catch [e] (swap! seen conj :outer))) @seen)`,
			want:   "[:outer]",
		},
		{
			name:   "contained loop in catch retains the finally guard",
			source: `(let [seen (atom [])] (try (try (throw (ex-info "body" {})) (catch [e] (loop [n 0] (try (if (= n 0) (recur 1) :done) (catch [inner] (swap! seen conj :stale)))) (throw (ex-info "catch" {}))) (finally (swap! seen conj :finally))) (catch [e] (swap! seen conj :outer))) @seen)`,
			want:   "[:finally :outer]",
		},
		{
			name: "multiple recur values survive finally temporaries",
			source: `(let [seen (atom []) result
			  (loop [a 0 b 1]
			    (try (if (= a 0) (recur b a) [a b])
			      (finally (let [[x y] [a b]] (swap! seen conj [x y])))))]
			  [result @seen])`,
			want: "[[1 0] [[0 1] [1 0]]]",
		},
		{
			name: "nested try within finally retains surrounding region metadata",
			source: `(let [seen (atom [])]
			  (loop [n 0]
			    (try (if (= n 0) (recur 1) :done)
			      (finally (try (throw (ex-info "cleanup" {}))
			        (catch [e] (swap! seen conj n))))))
			  @seen)`,
			want: "[0 1]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, MustNewRunner(), tt.source, tt.want)
		})
	}
}
