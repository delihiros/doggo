package vm

import "testing"

func TestDropDefersDiscardedInputEvaluation(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`(do (drop 1 (map inc ["bad"])) :ok)`)
	if err != nil {
		t.Fatalf("unused drop evaluated its input: %v", err)
	}
	if got := value.String(); got != ":ok" {
		t.Fatalf("Run() = %s, want :ok", got)
	}
}

func TestTakeTransducerStopsWithoutRealizingNextInput(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`(into [] (take 1) (map inc [1 "bad"]))`)
	if err != nil {
		t.Fatalf("take realized input after its limit: %v", err)
	}
	if got := value.String(); got != "[2]" {
		t.Fatalf("Run() = %s, want [2]", got)
	}
}

func TestLazyHigherOrderFunctionsPropagateEvaluationErrors(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{name: "builtin map", expr: `(vec (map inc [1 "bad" 3]))`},
		{name: "closure map", expr: `(vec (map (fn [x] (inc x)) [1 "bad" 3]))`},
		{name: "builtin filter", expr: `(vec (filter even? [2 "bad" 4]))`},
		{name: "closure filter", expr: `(vec (filter (fn [x] (even? x)) [2 "bad" 4]))`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			if value, err := runner.Run(tt.expr); err == nil {
				t.Fatalf("expected evaluation error, got %s", value.String())
			}
		})
	}
}

func TestLazyHigherOrderFunctionsOutliveFutureVM(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{name: "filter", expr: `(vec @(future (filter (fn [x] (> x 1)) [1 2 3])))`, want: `[2 3]`},
		{name: "keep", expr: `(vec @(future (keep (fn [x] (+ x 1)) [1 2])))`, want: `[2 3]`},
		{name: "remove", expr: `(vec @(future (remove (fn [x] (> x 1)) [1 2 3])))`, want: `[1]`},
		{name: "map-indexed", expr: `(vec @(future (map-indexed (fn [i x] (+ i x)) [10 20])))`, want: `[10 21]`},
		{name: "reductions", expr: `(vec @(future (reductions + 0 [1 2])))`, want: `[0 1 3]`},
		{name: "keep-indexed", expr: `(vec @(future (keep-indexed (fn [i x] [i x]) [10 20])))`, want: `[[0 10] [1 20]]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			value, err := runner.Run(tt.expr)
			if err != nil {
				t.Fatalf("realize future result: %v", err)
			}
			if got := value.String(); got != tt.want {
				t.Fatalf("Run() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLongLivedCallbacksOutliveFutureVM(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "comp",
			expr: `(let [f @(future (comp inc inc))]
			        (f 1))`,
			want: "3",
		},
		{
			name: "watch",
			expr: `(let [seen (atom 0)
			            watched @(future
			                       (let [a (atom 0)]
			                         (add-watch a :w (fn [_ _ _ new-value]
			                                           (reset! seen new-value)))))]
			        (reset! watched 7)
			        @seen)`,
			want: "7",
		},
		{
			name: "reify",
			expr: `(do
			        (import "fmt")
			        (let [implementation @(future
			                                   (reify fmt/Stringer
			                                          {:string (fn [] "ok")}))]
			          (. implementation String)))`,
			want: `"ok"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := MustNewRunner().Run(tt.expr)
			if err != nil {
				t.Fatalf("Run(): %v", err)
			}
			if got := value.String(); got != tt.want {
				t.Fatalf("Run() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestWatchErrorsAreReportedAfterStateChange(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(def watched-error (atom 0))
		(add-watch watched-error :failing (fn [_ _ _ _] (/ 1 0)))
		(reset! watched-error 1)
	`); err == nil {
		t.Fatal("reset! discarded watcher error")
	}

	value, err := runner.Run(`@watched-error`)
	if err != nil {
		t.Fatalf("read committed atom: %v", err)
	}
	if got := value.String(); got != "1" {
		t.Fatalf("atom state = %s, want committed value 1", got)
	}
}

func TestHigherOrderTransducersOutliveFutureVM(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		{
			name:     "map",
			expr:     `(into [] @(future (map (fn [x] (+ x 1)))) [1 2])`,
			expected: `[2 3]`,
		},
		{
			name:     "filter",
			expr:     `(into [] @(future (filter (fn [x] (> x 1)))) [1 2 3])`,
			expected: `[2 3]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()

			result, err := runner.Run(tt.expr)
			if err != nil {
				t.Fatalf("apply future transducer: %v", err)
			}
			if got := result.String(); got != tt.expected {
				t.Fatalf("result = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestComposedLazySequencesPropagateEvaluationErrors(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{name: "nested map materialization", expr: `(vec (map inc (map inc [1 "bad" 3])))`},
		{name: "filter over map", expr: `(vec (filter number? (map inc [1 "bad" 3])))`},
		{name: "nested map reduction", expr: `(reduce + 0 (map inc (map inc [1 "bad" 3])))`},
		{name: "first", expr: `(first (map inc ["bad"]))`},
		{name: "seq", expr: `(seq (map inc ["bad"]))`},
		{name: "take over map", expr: `(vec (take 3 (map inc [1 "bad" 3])))`},
		{name: "drop over map", expr: `(vec (drop 2 (map inc [1 "bad" 3])))`},
		{name: "keep over map", expr: `(vec (keep identity (map inc [1 "bad" 3])))`},
		{name: "remove over map", expr: `(vec (remove nil? (map inc [1 "bad" 3])))`},
		{name: "map-indexed over map", expr: `(vec (map-indexed (fn [i x] [i x]) (map inc [1 "bad" 3])))`},
		{name: "run over map", expr: `(run! identity (map inc [1 "bad" 3]))`},
		{name: "reduce-kv over map", expr: `(reduce-kv (fn [a k v] a) 0 (map (fn [x] (if (number? x) [x x] (inc x))) [1 "bad" 3]))`},
		{name: "reductions over map", expr: `(vec (reductions + 0 (map inc [1 "bad" 3])))`},
		{name: "reductions without init over map", expr: `(vec (reductions + (map inc [1 "bad" 3])))`},
		{name: "keep-indexed over map", expr: `(vec (keep-indexed (fn [i x] [i x]) (map inc [1 "bad" 3])))`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			if value, err := runner.Run(tt.expr); err == nil {
				t.Fatalf("expected evaluation error, got %s", value.String())
			}
		})
	}
}

func TestSequenceConsumersPropagateEvaluationErrors(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{name: "sort", expr: `(sort (map inc [1 "bad" 3]))`},
		{name: "sort-by", expr: `(sort-by identity (map inc [1 "bad" 3]))`},
		{name: "group-by", expr: `(group-by identity (map inc [1 "bad" 3]))`},
		{name: "reverse", expr: `(reverse (map inc [1 "bad" 3]))`},
		{name: "flatten", expr: `(flatten [(map inc [1 "bad" 3])])`},
		{name: "concat", expr: `(vec (concat (map inc [1 "bad"]) [3]))`},
		{name: "take-nth", expr: `(vec (take-nth 1 (map inc [1 "bad"])))`},
		{name: "every", expr: `(every? number? (map inc [1 "bad"]))`},
		{name: "count", expr: `(count (map inc [1 "bad" 3]))`},
		{name: "empty", expr: `(empty? (map inc ["bad"]))`},
		{name: "rest", expr: `(rest (map inc ["bad"]))`},
		{name: "next", expr: `(next (map inc ["bad"]))`},
		{name: "apply", expr: `(apply + (map inc [1 "bad" 3]))`},
		{name: "equality", expr: `(= (map inc ["bad"]) (list))`},
		{name: "equality after prefix", expr: `(= (map inc [1 "bad"]) (list 2))`},
		{name: "inequality", expr: `(not= (map inc ["bad"]) (list))`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			if value, err := runner.Run(tt.expr); err == nil {
				t.Fatalf("expected evaluation error, got %s", value.String())
			}
		})
	}
}

func TestReduceHonorsReducedFromUserReducers(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{
			name: "with init",
			expr: `(reduce (fn [a x] (if (= x 2) (reduced a) (+ a x))) 0 [1 2 3 4])`,
		},
		{
			name: "without init",
			expr: `(reduce (fn [a x] (if (= x 2) (reduced a) (+ a x))) [1 2 3 4])`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			value, err := runner.Run(tt.expr)
			if err != nil {
				t.Fatalf("reduce failed: %v", err)
			}
			if got := value.String(); got != "1" {
				t.Fatalf("reduce returned %s, want 1", got)
			}
		})
	}
}

func TestReduceEmptyReduciblePropagatesBuiltinErrors(t *testing.T) {
	runner := MustNewRunner()
	if value, err := runner.Run(`(reduce inc (range 0))`); err == nil {
		t.Fatalf("expected evaluation error, got %s", value.String())
	}
}

func TestReduceDoesNotReplayMappedSideEffectsAfterError(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(def reduce-calls (atom 0))`); err != nil {
		t.Fatalf("define counter: %v", err)
	}
	if _, err := runner.Run(`
		(def failing-map
		  (map (fn [x]
		         (swap! reduce-calls inc)
		         (if (= x 3) (inc "bad") x))
		       [1 2 3]))
	`); err != nil {
		t.Fatalf("define mapped sequence: %v", err)
	}

	if value, err := runner.Run(`(reduce + 0 failing-map)`); err == nil {
		t.Fatalf("expected mapped evaluation error, got %s", value.String())
	}
	value, err := runner.Run(`@reduce-calls`)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if got := value.String(); got != "3" {
		t.Fatalf("mapped function calls = %s, want 3", got)
	}
}
