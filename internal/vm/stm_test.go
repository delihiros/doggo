package vm

import (
	"strings"
	"testing"
	"time"

	"doggo/internal/types/atom"
)

func TestSTMRef(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "create ref",
			input:    `(ref? (ref 42))`,
			expected: "true",
		},
		{
			name:     "ref? false",
			input:    `(ref? 42)`,
			expected: "false",
		},
		{
			name:     "deref ref outside transaction",
			input:    `@(ref 42)`,
			expected: "42",
		},
		{
			name:     "deref ref with deref",
			input:    `(deref (ref "hello"))`,
			expected: `"hello"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestSTMDosync(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "simple dosync",
			input: `
				(def r (ref 10))
				(dosync (ref-set r 20))
				@r
			`,
			expected: "20",
		},
		{
			name: "dosync returns last value",
			input: `
				(def r (ref 0))
				(dosync
					(ref-set r 1)
					(ref-set r 2)
					42)
			`,
			expected: "42",
		},
		{
			name: "dosync with deref inside",
			input: `
				(def r (ref 10))
				(dosync
					(let [v @r]
						(ref-set r (+ v 5))))
				@r
			`,
			expected: "15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestSTMDosyncNestedExecutionRegression(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(def r (ref 1))
		(dosync
			((fn []
				(alter r (fn [v] (+ v (count [1 2 3])))))))
		@r
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "4" {
		t.Fatalf("expected 4, got %s", got)
	}
}

func TestSTMDosyncRetriesStaleReadAcrossVMBoundary(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(let [a (ref 0)
		      b (ref 0)
		      attempts (atom 0)
		      ready (chan 1)
		      release (chan 1)
		      f (future
		          (dosync
		            (let [x @a
		                  attempt (swap! attempts inc)]
		              (when (= attempt 1)
		                (>! ready true)
		                (<! release))
		              (+ x @b))))]
		  (<! ready)
		  (dosync (alter b inc))
		  (>! release true)
		  [@f @attempts])
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "[1 2]" {
		t.Fatalf("expected retried result [1 2], got %s", got)
	}
}

func TestSTMAlter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "alter with inc",
			input: `
				(def r (ref 10))
				(dosync (alter r inc))
				@r
			`,
			expected: "11",
		},
		{
			name: "alter with custom function",
			input: `
				(def r (ref 10))
				(dosync (alter r (fn [x] (* x 2))))
				@r
			`,
			expected: "20",
		},
		{
			name: "alter with extra args",
			input: `
				(def r (ref 10))
				(dosync (alter r + 5))
				@r
			`,
			expected: "15",
		},
		{
			name: "multiple alters",
			input: `
				(def r (ref 0))
				(dosync
					(alter r inc)
					(alter r inc)
					(alter r inc))
				@r
			`,
			expected: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestSTMMultipleRefs(t *testing.T) {
	runner := MustNewRunner()
	code := `
		(def a (ref 10))
		(def b (ref 20))
		(dosync
			(let [va @a
				  vb @b]
				(ref-set a vb)
				(ref-set b va)))
		[(deref a) (deref b)]
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	expected := "[20 10]"
	if result.String() != expected {
		t.Errorf("expected %s, got %s", expected, result.String())
	}
}

func TestSTMErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError string
	}{
		{
			name:        "ref-set outside dosync",
			input:       `(ref-set (ref 0) 1)`,
			expectError: "must be called inside dosync",
		},
		{
			name:        "alter outside dosync",
			input:       `(alter (ref 0) inc)`,
			expectError: "must be called inside dosync",
		},
		{
			name:        "ensure outside dosync",
			input:       `(ensure (ref 0))`,
			expectError: "must be called inside dosync",
		},
		{
			name:        "ref-set with non-ref",
			input:       `(dosync (ref-set 42 0))`,
			expectError: "must be a ref",
		},
		{
			name:        "alter with non-ref",
			input:       `(dosync (alter 42 inc))`,
			expectError: "must be a ref",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			_, err := runner.Run(tt.input)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.expectError)
			}
			if !strings.Contains(err.Error(), tt.expectError) {
				t.Errorf("expected error containing %q, got %q", tt.expectError, err.Error())
			}
		})
	}
}

func TestSTMWithClosure(t *testing.T) {
	runner := MustNewRunner()
	code := `
		(def counter (ref 0))
		(def increment (fn [] (dosync (alter counter inc))))
		(increment)
		(increment)
		(increment)
		@counter
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	expected := "3"
	if result.String() != expected {
		t.Errorf("expected %s, got %s", expected, result.String())
	}
}

func TestSTMRefWithMap(t *testing.T) {
	runner := MustNewRunner()
	code := `
		(def state (ref {:count 0 :name "test"}))
		(dosync
			(alter state (fn [s] (assoc s :count (inc (get s :count))))))
		(get @state :count)
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	// 結果は数値の1
	num, ok := result.(*atom.Number)
	if !ok {
		t.Fatalf("expected number, got %T", result)
	}
	if num.ToFloat64() != 1 {
		t.Errorf("expected 1, got %v", num.ToFloat64())
	}
}

func TestSTMEnsure(t *testing.T) {
	runner := MustNewRunner()
	code := `
		(def r (ref 42))
		(dosync
			(let [v (ensure r)]
				v))
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	expected := "42"
	if result.String() != expected {
		t.Errorf("expected %s, got %s", expected, result.String())
	}
}

func TestSTMNestedDosyncRollsBackWithOuterTransaction(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(def outer (ref 0))
		(def inner (ref 0))
		(try
			(dosync
				(alter outer inc)
				(dosync (alter inner inc))
				(throw (ex-info "boom" {:kind :test})))
			(catch [e] nil))
		[@outer @inner]
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "[0 0]" {
		t.Fatalf("expected both refs to roll back, got %s", got)
	}
}

func TestSTMAgentSendRunsOnceAfterRetry(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(let [r (ref 0)
		      a (agent 0)
		      attempts (atom 0)
		      ready (chan 1)
		      release (chan 1)
		      f (future
		          (dosync
		            (let [v @r
		                  n (swap! attempts inc)]
		              (send a inc)
		              (if (= n 1)
		                (do (>! ready true) (<! release)))
		              (ref-set r (inc v)))))]
		  (<! ready)
		  (dosync (alter r inc))
		  (>! release true)
		  @f
		  (await a)
		  [@r @a @attempts])
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "[2 1 2]" {
		t.Fatalf("expected one committed agent action after retry, got %s", got)
	}
}

func TestSTMAgentSendWaitsForReadOnlyTransactionCommit(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(let [a (agent 0)
		      ready (chan 1)
		      release (chan 1)
		      f (future
		          (dosync
		            (send a inc)
		            (>! ready true)
		            (<! release)))]
		  (<! ready)
		  (await a)
		  (let [before @a]
		    (>! release true)
		    @f
		    (await a)
		    [before @a]))
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "[0 1]" {
		t.Fatalf("expected agent action only after commit, got %s", got)
	}
}

func TestSTMAgentSendFailurePreventsCommit(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(let [r (ref 0)
		      a (agent 0)]
		  (send a (fn [_] (throw (ex-info "boom" {:kind :test}))))
		  (await-for 1000 a)
		  [(try
		     (dosync
		       (alter r inc)
		       (send a inc))
		     :no-error
		     (catch [e] :caught))
		   @r])
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "[:caught 0]" {
		t.Fatalf("failed send must abort before commit, got %s", got)
	}
}

func TestSTMRefAddWatch(t *testing.T) {
	t.Run("add-watch on ref", func(t *testing.T) {
		runner := MustNewRunner()
		// ウォッチャーを追加して、値が変更されたときにログを記録
		code := `
			(def r (ref 10))
			(def log (atom []))
			(add-watch r :test
				(fn [k ref old new]
					(swap! log conj {:old old :new new})))
			(dosync (ref-set r 20))
			@r  ; 変更後の値を確認
		`
		result, err := runner.Run(code)
		if err != nil {
			t.Fatalf("runner error: %s", err)
		}
		// 値が変更されたことを確認
		if result.String() != "20" {
			t.Errorf("expected 20, got %s", result.String())
		}

		// 非同期で実行されるウォッチャーを待つ
		time.Sleep(100 * time.Millisecond)

		// ログを確認
		logResult, err := runner.Run("@log")
		if err != nil {
			t.Fatalf("log check error: %s", err)
		}
		logStr := logResult.String()
		if !strings.Contains(logStr, ":old 10") || !strings.Contains(logStr, ":new 20") {
			t.Errorf("expected log to contain old=10, new=20, got %s", logStr)
		}
	})

	t.Run("remove-watch on ref", func(t *testing.T) {
		runner := MustNewRunner()
		code := `
			(def r (ref 10))
			(def log (atom []))
			(add-watch r :test
				(fn [k ref old new]
					(swap! log conj {:old old :new new})))
			(remove-watch r :test)
			(dosync (ref-set r 20))
			@r  ; 変更後の値を確認
		`
		assertRunResult(t, runner, code, "20")

		// 非同期で実行されるウォッチャーを待つ
		time.Sleep(100 * time.Millisecond)

		// ログが空であることを確認（ウォッチャーが削除されているため）
		logResult, err := runner.Run("@log")
		if err != nil {
			t.Fatalf("log check error: %s", err)
		}
		expected := "[]"
		if logResult.String() != expected {
			t.Errorf("expected %s, got %s", expected, logResult.String())
		}
	})
}
