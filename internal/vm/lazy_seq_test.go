package vm

import (
	"errors"
	"strings"
	"testing"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/coll"
)

func TestRecursiveLazySequencesReturnErrorsWithoutDeadlock(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{name: "lazy-seq", expr: `(do (def s (lazy-seq (list (first s)))) (first s))`},
		{name: "filter", expr: `(do (def s (filter (fn [x] (first s)) [1])) (first s))`},
		{name: "concat", expr: `(do (def s (concat (lazy-seq (list (first s))) [1])) (first s))`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() {
				_, err := MustNewRunner().Run(tt.expr)
				done <- err
			}()

			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "recursive lazy sequence realization") {
					t.Fatalf("Run() error = %v, want recursive realization error", err)
				}
			case <-time.After(time.Second):
				t.Fatal("recursive lazy sequence realization deadlocked")
			}
		})
	}
}

// dummyValue はテスト用のダミー Value 型です。
type dummyValue struct{}

func (d *dummyValue) String() string                { return "dummy" }
func (d *dummyValue) Type() string                  { return "dummy" }
func (d *dummyValue) Hash() uint32                  { return 0 }
func (d *dummyValue) Equals(other types.Value) bool { return false }
func (d *dummyValue) IsSelfEvaluating() bool        { return true }

// TestLazySeqFromCore はコアライブラリからロードされた lazy-seq 関数をテストします。
func TestLazySeqFromCore(t *testing.T) {
	// コアライブラリをロードした Runner を作成
	runner := MustNewRunner()

	// repeat がロードされたか確認（コアライブラリから）
	result, err := runner.Run(`(first (repeat 42))`)
	if err != nil {
		t.Fatalf("repeat failed: %v", err)
	}
	if result == nil {
		t.Fatalf("repeat returned nil result")
	}
	if result.String() != "42" {
		t.Errorf("expected 42, got %s", result.String())
	}

	// take 3 from repeat
	result, err = runner.Run(`(take 3 (repeat 5))`)
	if err != nil {
		t.Fatalf("take from repeat failed: %v", err)
	}
	if result.String() != "(5 5 5)" {
		t.Errorf("expected (5 5 5), got %s", result.String())
	}
}

// TestLazySeqBasic は lazy-seq の基本的な動作をテストします。
func TestLazySeqBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lazy-seq",
			input:    `(first (lazy-seq (cons 1 nil)))`,
			expected: "1",
		},
		{
			name:     "lazy-seq with list",
			input:    `(first (lazy-seq (list 1 2 3)))`,
			expected: "1",
		},
		{
			name:     "rest of lazy-seq",
			input:    `(first (rest (lazy-seq (list 1 2 3))))`,
			expected: "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestRepeat は repeat 関数をテストします。
func TestRepeat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "take 5 from repeat",
			input:    `(take 5 (repeat 1))`,
			expected: "(1 1 1 1 1)",
		},
		{
			name:     "take 3 from repeat string",
			input:    `(take 3 (repeat "hello"))`,
			expected: `("hello" "hello" "hello")`,
		},
		{
			name:     "first of repeat",
			input:    `(first (repeat 42))`,
			expected: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestIterate は iterate 関数をテストします。
func TestIterate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "take 5 from iterate inc",
			input:    `(take 5 (iterate inc 0))`,
			expected: "(0 1 2 3 4)",
		},
		{
			name:     "take 4 from iterate double",
			input:    `(take 4 (iterate (fn [x] (* x 2)) 1))`,
			expected: "(1 2 4 8)",
		},
		{
			name:     "first of iterate",
			input:    `(first (iterate inc 10))`,
			expected: "10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestCycle は cycle 関数をテストします。
func TestCycle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "take 7 from cycle",
			input:    `(take 7 (cycle [1 2 3]))`,
			expected: "(1 2 3 1 2 3 1)",
		},
		{
			name:     "take 5 from cycle list",
			input:    `(take 5 (cycle (list "a" "b")))`,
			expected: `("a" "b" "a" "b" "a")`,
		},
		{
			name:     "first of cycle",
			input:    `(first (cycle [10 20 30]))`,
			expected: "10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestTakeWhile は take-while 関数をテストします。
func TestTakeWhile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "take-while positive",
			input:    `(take 10 (take-while pos? [3 2 1 0 -1]))`,
			expected: "(3 2 1)",
		},
		{
			name:     "take-while all match",
			input:    `(take 10 (take-while pos? [1 2 3]))`,
			expected: "(1 2 3)",
		},
		{
			name:     "take-while none match",
			input:    `(take 10 (take-while pos? [-1 -2 -3]))`,
			expected: "()",
		},
		{
			name:     "take-while on infinite sequence",
			input:    `(take 10 (take-while (fn [x] (< x 5)) (iterate inc 0)))`,
			expected: "(0 1 2 3 4)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestDropWhile は drop-while 関数をテストします。
func TestDropWhile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "drop-while positive",
			input:    `(first (drop-while pos? [3 2 1 0 -1]))`,
			expected: "0",
		},
		{
			name:     "drop-while all match",
			input:    `(count (drop-while pos? [1 2 3]))`,
			expected: "0",
		},
		{
			name:     "drop-while none match",
			input:    `(first (drop-while pos? [-1 -2 -3]))`,
			expected: "-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestLazySeqChaining は遅延シーケンスのチェーンをテストします。
func TestLazySeqChaining(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "map on infinite sequence",
			input:    `(take 5 (map inc (iterate inc 0)))`,
			expected: "(1 2 3 4 5)",
		},
		{
			name:     "filter on finite lazy-seq",
			input:    `(filter even? [0 1 2 3 4 5 6 7 8 9])`, // filter は遅延シーケンスを返す
			expected: "(0 2 4 6 8)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestVMLazySeqErrorPropagation は VMLazySeq のエラー伝播をテストします。
func TestVMLazySeqErrorPropagation(t *testing.T) {
	// エラーを発生させるサンクを作成
	testErr := errors.New("test error in thunk")

	// ダミーの thunk（nil 以外の Value が必要）
	dummyThunk := &dummyValue{}

	// VMLazySeq を作成
	ls := NewVMLazySeq(dummyThunk, func(thunk types.Value) (types.Value, error) {
		return nil, testErr
	})

	// Seq() を呼び出すとエラーが発生し、LazySeqError が返る
	result := ls.Seq()

	// 結果が LazySeqError であることを確認
	lazySeqErr, ok := result.(*coll.LazySeqError)
	if !ok {
		t.Fatalf("expected *coll.LazySeqError, got %T", result)
	}

	// エラーの内容を確認
	if lazySeqErr.Err == nil {
		t.Error("expected error to be set")
	}
	if !strings.Contains(lazySeqErr.Err.Error(), "test error") {
		t.Errorf("expected error to contain 'test error', got %v", lazySeqErr.Err)
	}
}

func TestVMLazySeqFlattensAnEmptySourcedSequence(t *testing.T) {
	inner := coll.NewLazySeqFrom(coll.NewList(), func(types.Environment) (types.Value, error) {
		return nil, nil
	}, nil)
	seq := NewVMLazySeq(&dummyValue{}, func(types.Value) (types.Value, error) {
		return inner, nil
	})

	if got := seq.Seq(); got != nil {
		t.Fatalf("Seq() = %T, want nil", got)
	}
}

// TestVMLazySeqErrorString は VMLazySeq のエラー時の String() をテストします。
func TestVMLazySeqErrorString(t *testing.T) {
	// エラーを発生させるサンクを作成
	testErr := errors.New("detailed error message")
	dummyThunk := &dummyValue{}
	ls := NewVMLazySeq(dummyThunk, func(thunk types.Value) (types.Value, error) {
		return nil, testErr
	})

	// 評価を強制（Seq() を呼び出す）
	_ = ls.Seq()

	// String() がエラー詳細を含むことを確認
	str := ls.String()
	if !strings.Contains(str, "LazySeqError") {
		t.Errorf("expected String() to contain 'LazySeqError', got %s", str)
	}
	if !strings.Contains(str, "detailed error message") {
		t.Errorf("expected String() to contain error message, got %s", str)
	}
}

func TestVMLazySeqStringPreservesTailErrors(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`(lazy-seq (cons 1 (lazy-seq (inc "bad"))))`)
	if err != nil {
		t.Fatalf("creating lazy sequence failed: %v", err)
	}

	if got := value.String(); !strings.Contains(got, "inc: argument must be a number") {
		t.Fatalf("lazy sequence string = %s, want tail evaluation error", got)
	}
}

// TestLazySeqErrorType は LazySeqError の Value インターフェース実装をテストします。
func TestLazySeqErrorType(t *testing.T) {
	testErr := errors.New("test error")
	lazySeqErr := &coll.LazySeqError{Err: testErr}

	// Type()
	if lazySeqErr.Type() != "lazyseq-error" {
		t.Errorf("expected Type() to return 'lazyseq-error', got %s", lazySeqErr.Type())
	}

	// String()
	str := lazySeqErr.String()
	if !strings.Contains(str, "LazySeqError") {
		t.Errorf("expected String() to contain 'LazySeqError', got %s", str)
	}
	if !strings.Contains(str, "test error") {
		t.Errorf("expected String() to contain error message, got %s", str)
	}

	// Hash()
	if lazySeqErr.Hash() != 0 {
		t.Errorf("expected Hash() to return 0, got %d", lazySeqErr.Hash())
	}

	// Equals()
	if lazySeqErr.Equals(lazySeqErr) {
		t.Error("expected Equals() to return false")
	}

	// IsSelfEvaluating()
	if !lazySeqErr.IsSelfEvaluating() {
		t.Error("expected IsSelfEvaluating() to return true")
	}

	// Cause()
	if lazySeqErr.Cause() != testErr {
		t.Errorf("expected Cause() to return the original error, got %v", lazySeqErr.Cause())
	}
}

func TestNestedLazySequenceErrorsPropagateThroughEquality(t *testing.T) {
	runner := MustNewRunner()
	for _, expression := range []string{
		`(= (list (map inc ["bad"])) (list (list)))`,
		`(= [(map inc ["bad"])] [(list)])`,
		`(= {:value (map inc ["bad"])} {:value (list)})`,
		`(= {(map inc ["bad"]) :value} {(list) :value})`,
		`(= #{(map inc ["bad"])} #{(list)})`,
	} {
		_, err := runner.Run(expression)
		if err == nil || !strings.Contains(err.Error(), "inc: argument must be a number") {
			t.Fatalf("Run(%q) error = %v, want nested lazy evaluation error", expression, err)
		}
	}
}

func TestLazySequenceErrorsPropagateThroughHashedLookup(t *testing.T) {
	runner := MustNewRunner()
	for _, expression := range []string{
		`(contains? {(map inc ["bad"]) :value} (list))`,
		`(contains? #{(map inc ["bad"])} (list))`,
		`({(map inc ["bad"]) :value} (list))`,
		`(#{(map inc ["bad"])} (list))`,
	} {
		_, err := runner.Run(expression)
		if err == nil || !strings.Contains(err.Error(), "inc: argument must be a number") {
			t.Fatalf("Run(%q) error = %v, want lazy hash evaluation error", expression, err)
		}
	}
}

func TestLazySequenceErrorsRejectHashedCollectionConstruction(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(defrecord LazyKeyRecord [name])`); err != nil {
		t.Fatalf("defrecord setup failed: %v", err)
	}
	for _, expression := range []string{
		`{(map inc ["bad"]) :value}`,
		`#{(map inc ["bad"])}`,
		`(hash-map (map inc ["bad"]) :value)`,
		`(set [(map inc ["bad"])])`,
		`(assoc {} (map inc ["bad"]) :value)`,
		`(conj #{} (map inc ["bad"]))`,
		`(dissoc {:safe :value} (map inc ["bad"]))`,
		`(disj #{:safe} (map inc ["bad"]))`,
		`(persistent! (assoc! (transient {}) (map inc ["bad"]) :value))`,
	} {
		_, err := runner.Run(expression)
		if err == nil || !strings.Contains(err.Error(), "inc: argument must be a number") {
			t.Fatalf("Run(%q) error = %v, want lazy hash evaluation error", expression, err)
		}
	}
}

func TestRecordLookupDoesNotRealizeAProvablyDifferentKey(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(defrecord LazyKeyRecord [name])`); err != nil {
		t.Fatalf("defrecord setup failed: %v", err)
	}
	for _, expression := range []string{
		`(get (->LazyKeyRecord "Ada") (map inc ["bad"]) :missing)`,
		`(find (->LazyKeyRecord "Ada") (map inc ["bad"]))`,
		`(contains? (->LazyKeyRecord "Ada") (map inc ["bad"]))`,
		`(dissoc (->LazyKeyRecord "Ada") (map inc ["bad"]))`,
	} {
		if _, err := runner.Run(expression); err != nil {
			t.Fatalf("Run(%q) error = %v, want lookup without key realization", expression, err)
		}
	}
}
