package vm

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// TestBindingBasic は dynamic var の基本的な束縛をテストします。
func TestBindingBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "dynamic var binding changes value inside binding",
			input: `
				(def ^:dynamic *x* 1)
				(binding [*x* 2] *x*)
			`,
			expected: "2",
		},
		{
			name: "dynamic var returns to root after binding",
			input: `
				(def ^:dynamic *x* 1)
				(binding [*x* 2] *x*)
				*x*
			`,
			expected: "1",
		},
		{
			name: "binding with multiple vars",
			input: `
				(def ^:dynamic *a* 10)
				(def ^:dynamic *b* 20)
				(binding [*a* 100 *b* 200]
					(+ *a* *b*))
			`,
			expected: "300",
		},
		{
			name: "binding returns body result",
			input: `
				(def ^:dynamic *x* 1)
				(binding [*x* 42] (+ *x* 8))
			`,
			expected: "50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestDynamicBindingPrimitivesUseExecutionScopedNames(t *testing.T) {
	for _, name := range []string{"push-dynamic-bindings", "pop-dynamic-bindings", "dynamically-bound?"} {
		if _, ok := DefaultBuiltinCatalog().Lookup(name); !ok {
			t.Fatalf("dynamic binding primitive %q is not registered", name)
		}
	}

	runner := MustNewRunner()
	result, err := runner.Run(`
		(do
		  (def ^:dynamic *scoped* 1)
		  (push-dynamic-bindings {#'*scoped* 2})
		  (let [inside [(dynamically-bound? #'*scoped*) *scoped*]]
		    (pop-dynamic-bindings)
		    [inside (dynamically-bound? #'*scoped*) *scoped*]))
	`)
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if got := result.String(); got != "[[true 2] false 1]" {
		t.Fatalf("dynamic binding lifecycle = %s, want [[true 2] false 1]", got)
	}
}

func TestAtomResetWatchersPreserveDynamicBindings(t *testing.T) {
	for _, resetForm := range []string{
		`(reset! watched 1)`,
		`(reset-vals! watched 1)`,
	} {
		t.Run(resetForm, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(`
				(def ^:dynamic *watch-binding* 1)
				(let [seen (atom 0)
				      watched (atom 0)]
				  (add-watch watched :binding
				    (fn [_ _ _ _] (reset! seen *watch-binding*)))
				  (binding [*watch-binding* 42]
				    ` + resetForm + `)
				  @seen)
			`)
			if err != nil {
				t.Fatalf("Run(): %v", err)
			}
			if got := result.String(); got != "42" {
				t.Fatalf("watch observed binding %s, want 42", got)
			}
		})
	}
}

// TestBindingNested はネストした binding をテストします。
func TestBindingNested(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "nested binding inner takes precedence",
			input: `
				(def ^:dynamic *x* 1)
				(binding [*x* 10]
					(binding [*x* 100]
						*x*))
			`,
			expected: "100",
		},
		{
			name: "nested binding restores to outer after inner exits",
			input: `
				(def ^:dynamic *x* 1)
				(binding [*x* 10]
					(let [inner-result (binding [*x* 100] *x*)]
						(list inner-result *x*)))
			`,
			expected: "(100 10)",
		},
		{
			name: "deeply nested binding",
			input: `
				(def ^:dynamic *x* 0)
				(binding [*x* 1]
					(binding [*x* 2]
						(binding [*x* 3]
							*x*)))
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

func TestBindingPropagatesThroughApply(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(def ^:dynamic *x* 1)
		(let [f (fn [a b] *x*)]
		  (binding [*x* 42]
		    (apply f [1 2])))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "42" {
		t.Fatalf("expected 42, got %s", result.String())
	}
}

func TestBindingConcurrentFuturesAreIsolated(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(def ^:dynamic *concurrent-binding* 0)
		(let [ready-1 (chan 1)
		      release-1 (chan 1)
		      ready-2 (chan 1)
		      release-2 (chan 1)
		      f1 (future
		           (binding [*concurrent-binding* 1]
		             (>! ready-1 true)
		             (<! release-1)
		             *concurrent-binding*))
		      waited-1 (<! ready-1)
		      f2 (future
		           (binding [*concurrent-binding* 2]
		             (>! ready-2 true)
		             (<! release-2)
		             *concurrent-binding*))
		      waited-2 (<! ready-2)
		      sent-1 (>! release-1 true)
		      result-1 @f1
		      sent-2 (>! release-2 true)
		      result-2 @f2]
		  [result-1 result-2])
	`)
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if got := result.String(); got != "[1 2]" {
		t.Fatalf("concurrent future bindings = %s, want [1 2]", got)
	}
}

func TestBindingAsyncExecutionInheritsSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "future",
			source: `
				(def ^:dynamic *async-binding* 0)
				(let [release (chan 1)
				      f (binding [*async-binding* 42]
				          (future (<! release) *async-binding*))]
				  (>! release true)
				  @f)
			`,
		},
		{
			name: "go",
			source: `
				(def ^:dynamic *async-binding* 0)
				(let [release (chan 1)
				      result (chan 1)]
				  (binding [*async-binding* 42]
				    (go (<! release) (>! result *async-binding*)))
				  (>! release true)
				  (<! result))
			`,
		},
		{
			name: "agent action",
			source: `
				(def ^:dynamic *async-binding* 0)
				(let [release (chan 1)
				      a (agent 0)]
				  (binding [*async-binding* 42]
				    (send a (fn [_] (<! release) *async-binding*)))
				  (>! release true)
				  (await a)
				  @a)
			`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.source)
			if err != nil {
				t.Fatalf("Run(): %v", err)
			}
			if got := result.String(); got != "42" {
				t.Fatalf("async binding = %s, want 42", got)
			}
		})
	}
}

// TestBindingUnwind は例外発生時のunwindをテストします。
func TestBindingUnwind(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "binding restores after exception in try/catch",
			input: `
				(def ^:dynamic *x* 1)
				(try
					(binding [*x* 999]
						(throw (ex-info "test error" {:kind :test})))
					(catch [e] *x*))
				*x*
			`,
			expected: "1",
		},
		{
			name: "binding value visible inside catch if binding wraps try",
			input: `
				(def ^:dynamic *x* 1)
				(binding [*x* 42]
					(try
						(throw (ex-info "test" {:kind :test}))
					(catch [e] *x*)))
			`,
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

// TestBindingNonDynamicError は非dynamic varへのbindingがエラーになることをテストします。
func TestBindingNonDynamicError(t *testing.T) {
	runner := MustNewRunner()

	// 非dynamic varを定義
	_, err := runner.Run(`(def *y* 1)`)
	if err != nil {
		t.Fatalf("failed to define var: %v", err)
	}

	// bindingを試みる（エラーになるべき）
	_, err = runner.Run(`(binding [*y* 2] *y*)`)
	if err == nil {
		t.Fatal("expected error when binding non-dynamic var, got nil")
	}

	// エラーメッセージに "dynamic" が含まれることを確認
	if !strings.Contains(err.Error(), "dynamic") {
		t.Errorf("expected error message to contain 'dynamic', got: %s", err.Error())
	}
}

// TestBindingIsolatedAcrossConcurrentExecutions は並行する論理実行間の独立性をテストします。
// 別のcontextで binding しても他のcontextに影響しないことを確認します。
func TestBindingIsolatedAcrossConcurrentExecutions(t *testing.T) {
	// 2つの独立したRunnerを作成（各々が独自のcontextを持つ）
	runner1 := MustNewRunner()
	runner2 := MustNewRunner()

	// dynamic varを両方で定義
	_, err := runner1.Run(`(def ^:dynamic *shared* 0)`)
	if err != nil {
		t.Fatalf("runner1 failed to define var: %v", err)
	}
	_, err = runner2.Run(`(def ^:dynamic *shared* 0)`)
	if err != nil {
		t.Fatalf("runner2 failed to define var: %v", err)
	}

	// 並行実行で独立性を確認
	var wg sync.WaitGroup
	var result1, result2 string
	var err1, err2 error

	wg.Add(2)

	go func() {
		defer wg.Done()
		// runner1 は *shared* を 100 に束縛
		res, e := runner1.Run(`
			(binding [*shared* 100]
				(do
					; 少し待機して他のgoroutineと重なるようにする
					*shared*))
		`)
		if e != nil {
			err1 = e
			return
		}
		result1 = res.String()
	}()

	go func() {
		defer wg.Done()
		// runner2 は *shared* を 200 に束縛
		res, e := runner2.Run(`
			(binding [*shared* 200]
				(do
					*shared*))
		`)
		if e != nil {
			err2 = e
			return
		}
		result2 = res.String()
	}()

	wg.Wait()

	if err1 != nil {
		t.Fatalf("runner1 error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("runner2 error: %v", err2)
	}

	// 各runnerは自身の束縛値を見るべき
	if result1 != "100" {
		t.Errorf("runner1 expected 100, got %s", result1)
	}
	if result2 != "200" {
		t.Errorf("runner2 expected 200, got %s", result2)
	}
}

// TestBindingWithSameContext は同一contextでのbindingの独立性をテストします。
func TestBindingWithSameContext(t *testing.T) {
	runner := MustNewRunner()
	ctx := context.Background()
	runner.SetContext(ctx)

	// dynamic varを定義
	_, err := runner.Run(`(def ^:dynamic *ctx-var* "root")`)
	if err != nil {
		t.Fatalf("failed to define var: %v", err)
	}

	// binding内でのみ変更され、外側には影響しない
	result, err := runner.Run(`
		(let [inside (binding [*ctx-var* "bound"] *ctx-var*)
		      outside *ctx-var*]
			(list inside outside))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `("bound" "root")`
	if result.String() != expected {
		t.Errorf("expected %s, got %s", expected, result.String())
	}
}

// TestVarSpecialForm は #'x (var x) が Var オブジェクトを返すことをテストします。
func TestVarSpecialForm(t *testing.T) {
	runner := MustNewRunner()

	// Varオブジェクトが取得できることを確認（var?述語でチェック）
	result, err := runner.Run(`
		(def ^:dynamic *test-var* 42)
		(var? #'*test-var*)
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.String() != "true" {
		t.Errorf("expected var? to return true, got %s", result.String())
	}
}

// TestVarDeref は Varをderefするとその値が取得できることをテストします。
func TestVarDeref(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(def *my-var* 123)
		@#'*my-var*
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.String() != "123" {
		t.Errorf("expected 123, got %s", result.String())
	}
}

func TestVarSpecialFormRejectsProtectedTypeBindings(t *testing.T) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(defrecord User [name])
		#'User
	`)
	if err == nil {
		t.Fatalf("expected var quote on protected type binding to fail")
	}
	if !strings.Contains(err.Error(), "var can only reference vars") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBindingWithVarQuote は binding 内で #' を使って Var にアクセスできることをテストします。
func TestBindingWithVarQuote(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(def ^:dynamic *v* 1)
		(binding [*v* 99]
			(list *v* @#'*v*))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// binding中は *v* も @#'*v* も 99 を返すべき
	expected := "(99 99)"
	if result.String() != expected {
		t.Errorf("expected %s, got %s", expected, result.String())
	}
}
