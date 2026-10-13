package vm

import (
	"testing"
	"time"
)

// TestDerefChannel tests that deref works with channels
func TestDerefChannel(t *testing.T) {
	runner := MustNewRunner()

	// チャネルにderefを使用
	result, err := runner.Run(`
		(let [ch (chan 1)]
		  (>! ch 42)
		  (deref ch))
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if result.String() != "42" {
		t.Errorf("expected 42, got %s", result.String())
	}
}

// TestDerefAtomStillWorks tests that deref still works with atoms
func TestDerefAtomStillWorks(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [a (atom 100)]
		  (deref a))
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if result.String() != "100" {
		t.Errorf("expected 100, got %s", result.String())
	}
}

// TestFuture tests the future macro
func TestFuture(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple future",
			input:    `@(future (* 6 7))`,
			expected: "42",
		},
		{
			name:     "future with multiple expressions",
			input:    `@(future (+ 1 2) (+ 3 4))`,
			expected: "7",
		},
		{
			name:     "future with deref function",
			input:    `(deref (future (+ 10 20)))`,
			expected: "30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestFutureMemoizesResult(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`(let [f (future 42)] [@f @f])`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if result.String() != "[42 42]" {
		t.Fatalf("repeated future deref = %s, want [42 42]", result.String())
	}
}

func TestRealizedReportsFutureCompletion(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [release (chan 1)
		      f (future (<! release))
		      before (realized? f)]
		  (>! release 42)
		  [before @f (realized? f)])`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "[false 42 true]" {
		t.Fatalf("future realization state = %s, want [false 42 true]", got)
	}
}

func TestSharedLazySequencesPublishRealizationAtomically(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "map",
			expr: `(do
				(import "time")
				(let [calls (atom 0)
				      s (map (fn [x] (swap! calls inc) (time/Sleep 50000000) x) [1])
				      a (future (first s))
				      b (future (first s))]
				  [@a @b @calls]))`,
			want: "[1 1 1]",
		},
		{
			name: "concat",
			expr: `(do
				(import "time")
				(let [slow (lazy-seq (do (time/Sleep 50000000) (list 1)))
				      s (concat slow [2])
				      a (future (first s))
				      b (future (first s))]
				  [@a @b]))`,
			want: "[1 1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.expr)
			if err != nil {
				t.Fatalf("shared realization failed: %v", err)
			}
			if got := result.String(); got != tt.want {
				t.Fatalf("shared realization = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFuturePropagatesErrorWithoutHanging(t *testing.T) {
	runner := MustNewRunner()
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(`@(future (/ 1 0))`)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("failed future deref succeeded, want evaluation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("failed future deref hung instead of returning its error")
	}
}

// TestFutureAsync tests that future executes asynchronously
func TestFutureAsync(t *testing.T) {
	runner := MustNewRunner()

	// Futureは非同期に開始するため、すぐに制御が戻る
	done := make(chan struct{})
	go func() {
		_, err := runner.Run(`
			(def f (future (* 6 7)))
			@f
		`)
		if err != nil {
			t.Errorf("runner error: %s", err)
		}
		close(done)
	}()

	select {
	case <-done:
		// 正常に完了
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: future seems to be blocking")
	}
}

// TestPcalls tests the pcalls function
func TestPcalls(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(pcalls (fn [] 1) (fn [] 2) (fn [] 3))
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// 結果はリストで [1, 2, 3]
	resultStr := result.String()
	// リスト形式 "(1 2 3)" または "[1 2 3]" をチェック
	if resultStr != "(1 2 3)" && resultStr != "[1 2 3]" {
		t.Errorf("expected (1 2 3) or [1 2 3], got %s", resultStr)
	}
}

// TestPcallsParallelism tests that pcalls executes functions in parallel
func TestPcallsParallelism(t *testing.T) {
	runner := MustNewRunner()

	assertConcurrentResult(t, runner, `
		(let [ready (chan 2)
		      release (chan 2)]
		  (go (do
		    (<! ready)
		    (<! ready)
		    (>! release true)
		    (>! release true)))
		  (doall (pcalls
		    (fn [] (>! ready 1) (<! release) 1)
		    (fn [] (>! ready 2) (<! release) 2))))
	`, "(1 2)")
}

// TestPmap tests the pmap function
func TestPmap(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(pmap inc [1 2 3 4 5])
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// 結果は [2 3 4 5 6]
	resultStr := result.String()
	if resultStr != "(2 3 4 5 6)" && resultStr != "[2 3 4 5 6]" {
		t.Errorf("expected (2 3 4 5 6) or [2 3 4 5 6], got %s", resultStr)
	}
}

func TestPmapParallelism(t *testing.T) {
	runner := MustNewRunner()

	assertConcurrentResult(t, runner, `
		(let [ready (chan 2)
		      release (chan 2)]
		  (go (do
		    (<! ready)
		    (<! ready)
		    (>! release true)
		    (>! release true)))
		  (doall (pmap
		    (fn [x] (>! ready x) (<! release) x)
		    [1 2])))
	`, "(1 2)")
}

func TestPmapDoesNotEagerlyRealizeEntireInput(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [realized (atom 0)
		      values (repeatedly 100 (fn [] (swap! realized inc)))
		      first-result (first (pmap inc values))]
		  [first-result (< @realized 100)])
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if result.String() != "[2 true]" {
		t.Fatalf("lazy pmap result = %s, want [2 true]", result.String())
	}
}

func TestLazyMapKeepsCallableRuntimeAfterThunkReturns(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(first
		  (lazy-seq
		    (let [futures (doall (map (fn [x] (future x)) [1]))]
		      (map deref futures))))
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if result.String() != "1" {
		t.Fatalf("lazy mapped future result = %s, want 1", result.String())
	}
}

func assertConcurrentResult(t *testing.T, runner *Runner, source, want string) {
	t.Helper()
	type outcome struct {
		result string
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := runner.Run(source)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		done <- outcome{result: result.String()}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("runner error: %s", got.err)
		}
		if got.result != want {
			t.Fatalf("parallel result = %s, want %s", got.result, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parallel operations did not start before the first result was dereferenced")
	}
}

// TestPmapWithComplexFunction tests pmap with more complex function
func TestPmapWithComplexFunction(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(pmap (fn [x] (* x x)) [1 2 3 4])
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// 結果は [1 4 9 16]
	resultStr := result.String()
	if resultStr != "(1 4 9 16)" && resultStr != "[1 4 9 16]" {
		t.Errorf("expected (1 4 9 16) or [1 4 9 16], got %s", resultStr)
	}
}

// TestFutureWithMultipleFutures tests multiple futures running concurrently
func TestFutureWithMultipleFutures(t *testing.T) {
	runner := MustNewRunner()

	// 注意: + は2引数のみサポートなので、ネストして使用
	result, err := runner.Run(`
		(let [f1 (future (+ 1 1))
		      f2 (future (+ 2 2))
		      f3 (future (+ 3 3))]
		  (+ (+ @f1 @f2) @f3))
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if result.String() != "12" {
		t.Errorf("expected 12, got %s", result.String())
	}
}
