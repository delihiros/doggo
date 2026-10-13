package vm

import (
	"fmt"
	"strings"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/types/atom"
	"doggo/internal/vm/engine"
)

func TestRunnerWithGlobals(t *testing.T) {
	runner := MustNewRunner()

	// defで定義
	_, err := runner.Run("(def x 10)")
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// 定義した変数を使用
	result, err := runner.Run("(+ x 5)")
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	err = testIntegerObject(15, result)
	if err != nil {
		t.Errorf("testIntegerObject failed: %s", err)
	}
}

func TestNewRunnerWithConfigNilModulesUsesDefaultRuntimeModules(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	result, err := runner.Run(`(if load-file 1 0)`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if err := testIntegerObject(1, result); err != nil {
		t.Fatalf("unexpected result: %s", err)
	}
}

func TestNewRunnerWithConfigAllowsExplicitModuleComposition(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
		RuntimeProfile: testExactDefaultRuntimeProfileWithModules([]RunnerModule{
			NewRunnerModule("test-value", func(r *Runner) error {
				sym := r.symbolTable.DefineGlobalInNamespace(compiler.CoreNamespace, "test-value")
				r.getOrCreateExecutionSession().globals[sym.Index] = atom.NewNumberInt(7)
				return nil
			}),
		}),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	result, err := runner.Run("test-value")
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if err := testIntegerObject(7, result); err != nil {
		t.Fatalf("unexpected result: %s", err)
	}

	assertRunErrorContains(t, runner, "load-file", "undefined variable: load-file")
}

func TestNewRunnerWithConfigBootstrapCoreBootstrapsMinimalRuntime(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  true,
		RuntimeProfile: testExactDefaultRuntimeProfile(),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`(take 3 (repeat 1))`)
	if err != nil {
		t.Fatalf("repeat should work after core bootstrap: %v", err)
	}

	if value.String() != "(1 1 1)" {
		t.Fatalf("unexpected repeat result: %s", value.String())
	}
}

func TestBootstrapCoreBootstrapsMinimalRuntime(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: testExactDefaultRuntimeProfile(),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	if err := runner.BootstrapCore(); err != nil {
		t.Fatalf("load core: %v", err)
	}

	value, err := runner.Run(`(take 3 (repeat 2))`)
	if err != nil {
		t.Fatalf("repeat should work after manual core bootstrap: %v", err)
	}

	if value.String() != "(2 2 2)" {
		t.Fatalf("unexpected repeat result: %s", value.String())
	}
}

func TestRunnerRunSkipsTopLevelDiscard(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run("#_ 1\n2")
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	if err := testIntegerObject(2, result); err != nil {
		t.Fatalf("unexpected result: %s", err)
	}
}

func TestRunnerLoopRecur(t *testing.T) {
	runner := MustNewRunner()

	// フィボナッチ数列の10番目（0始まり）
	result, err := runner.Run(`
		(loop [n 10 a 0 b 1]
			(if (= n 0)
				a
				(recur (- n 1) b (+ a b))))
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// fib(10) = 55
	err = testIntegerObject(55, result)
	if err != nil {
		t.Errorf("testIntegerObject failed: %s", err)
	}
}

func TestRunnerFunction(t *testing.T) {
	runner := MustNewRunner()

	// 関数定義と呼び出し
	result, err := runner.Run(`
		(do
			(def add (fn [a b] (+ a b)))
			(add 3 4))
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	err = testIntegerObject(7, result)
	if err != nil {
		t.Errorf("testIntegerObject failed: %s", err)
	}
}

func TestRunnerReadsRedefinedGlobals(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `(do
		(def value 100)
		(def read-value (fn [] value))
		(def value 200)
		(defn factorial [n]
			(if (<= n 1) 1 (* n (factorial (dec n)))))
		(def wrap (fn [f] (fn [n] (f n))))
		(def factorial (wrap factorial))
		[(read-value) (factorial 5)])`, `[200 120]`)
}

func TestRunnerQuasiquote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// 基本的なアトム
		{
			name:     "quasiquote number",
			input:    "(quasiquote 1)",
			expected: "1",
		},
		// シンボル（quote と同じくシンボルそのものを返す）
		{
			name:     "quasiquote symbol",
			input:    "(quasiquote x)",
			expected: "user/x",
		},
		// リスト（unquote なし）
		{
			name:     "quasiquote list",
			input:    "(quasiquote (1 2 3))",
			expected: "(1 2 3)",
		},
		// unquote を含むリスト
		{
			name:     "quasiquote with unquote",
			input:    "(do (def a 10) (quasiquote (1 (unquote a) 3)))",
			expected: "(1 10 3)",
		},
		// ネストしたリスト
		{
			name:     "nested quasiquote",
			input:    "(quasiquote (1 (2 3) 4))",
			expected: "(1 (2 3) 4)",
		},
		{
			name:     "nested quasiquote preserves inner unquote",
			input:    "(do (def x 10) `(1 `(2 ~x) 3))",
			expected: "(1 (quasiquote (2 (unquote user/x))) 3)",
		},
		// ネストした unquote
		{
			name:     "nested unquote",
			input:    "(do (def b 20) (quasiquote (1 ((unquote b) 3) 4)))",
			expected: "(1 (20 3) 4)",
		},
		// 計算式の unquote
		{
			name:     "unquote with computation",
			input:    "(quasiquote (1 (unquote (+ 1 1)) 3))",
			expected: "(1 2 3)",
		},
		// 空リスト
		{
			name:     "quasiquote empty list",
			input:    "(quasiquote ())",
			expected: "()",
		},
		{
			name:     "quasiquote map with unquote",
			input:    "(do (def x 10) `{:a ~x})",
			expected: "{:a 10}",
		},
		{
			name:     "quasiquote map preserves source order",
			input:    "`{:z 1 :a 2}",
			expected: "{:z 1, :a 2}",
		},
		{
			name:     "quasiquote set with unquote",
			input:    "(do (def x 10) `(1 #{~x} 3))",
			expected: "(1 #{10} 3)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 各テストで新しい Runner を作成（状態の干渉を防ぐ）
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestRunnerUnquoteSplicing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// 基本的なスプライシング
		{
			name:     "basic splicing",
			input:    "(do (def items [2 3 4]) (quasiquote (1 (unquote-splicing items) 5)))",
			expected: "(1 2 3 4 5)",
		},
		// 先頭でのスプライシング
		{
			name:     "splicing at start",
			input:    "(do (def items [1 2 3]) (quasiquote ((unquote-splicing items) 4 5)))",
			expected: "(1 2 3 4 5)",
		},
		// 末尾でのスプライシング
		{
			name:     "splicing at end",
			input:    "(do (def items [3 4 5]) (quasiquote (1 2 (unquote-splicing items))))",
			expected: "(1 2 3 4 5)",
		},
		// 空リストのスプライシング
		{
			name:     "splicing empty list",
			input:    "(do (def items []) (quasiquote (1 (unquote-splicing items) 2)))",
			expected: "(1 2)",
		},
		// 複数のスプライシング
		{
			name:     "multiple splicing",
			input:    "(do (def a [1 2]) (def b [3 4]) (quasiquote ((unquote-splicing a) (unquote-splicing b))))",
			expected: "(1 2 3 4)",
		},
		// スプライシングと unquote の混合
		{
			name:     "splicing with unquote",
			input:    "(do (def x 10) (def items [2 3]) (quasiquote (1 (unquote x) (unquote-splicing items) 4)))",
			expected: "(1 10 2 3 4)",
		},
		// ネストしたリスト内でのスプライシング
		{
			name:     "splicing in nested list",
			input:    "(do (def items [2 3]) (quasiquote (1 (a (unquote-splicing items) b) 4)))",
			expected: "(1 (user/a 2 3 user/b) 4)",
		},
		// ベクター内でのスプライシング
		{
			name:     "splicing in vector",
			input:    "(do (def items [2 3]) (quasiquote [1 (unquote-splicing items) 4]))",
			expected: "[1 2 3 4]",
		},
		// ~@ 略記形式
		{
			name:     "~@ shorthand",
			input:    "(do (def items [2 3 4]) `(1 ~@items 5))",
			expected: "(1 2 3 4 5)",
		},
		// 計算結果のスプライシング
		{
			name:     "splicing computed value",
			input:    "`(1 ~@(map inc [1 2 3]) 5)",
			expected: "(1 2 3 4 5)",
		},
		{
			name:     "splicing in set",
			input:    "(do (def items [1 2]) `#{~@items})",
			expected: "#{1 2}",
		},
		{
			name:     "splicing ignores local concat shadowing",
			input:    "(let [concat (fn [& xs] [:shadow xs]) apply (fn [& xs] [:shadow xs])] `(1 ~@[2 3] 4))",
			expected: "(1 2 3 4)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestRunnerQuasiquoteIgnoresLocalHelperShadowing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "list constructor",
			input:    "(let [list (fn [& xs] [:shadow xs])] `(1 2))",
			expected: "(1 2)",
		},
		{
			name:     "map constructor",
			input:    "(let [hash-map (fn [& xs] [:shadow xs])] `{:a 1})",
			expected: "{:a 1}",
		},
		{
			name:     "set constructor",
			input:    "(let [set (fn [& xs] [:shadow xs])] `#{1 2})",
			expected: "#{1 2}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestRunnerQuasiquoteIgnoresNamespaceAliasShadowing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "alias named core does not hijack syntax-quote helpers",
			input:    "(ns user (:require [doggo.walk :as core]))\n`(1 2)",
			expected: "(1 2)",
		},
		{
			name:     "alias named core does not break literal metadata compilation",
			input:    "(ns user (:require [doggo.walk :as core]))\n(meta ^{:tag :x} [1 2])",
			expected: "{:tag :x}",
		},
		{
			name:     "user namespace named core does not hijack syntax-quote helpers",
			input:    "(ns core)\n(def list (fn [& xs] [:shadow xs]))\n`(1 2)",
			expected: "(1 2)",
		},
		{
			name:     "user namespace named core does not hijack with-meta helper",
			input:    "(ns core)\n(def with-meta (fn [& _] :shadow))\n(meta ^{:tag :x} [1 2])",
			expected: "{:tag :x}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestRunnerQuasiquoteQualifiesSymbolsToDefinitionNamespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "macro expansion stays pinned to macro definition namespace",
			input:    "(ns a)\n(def helper 1)\n(defmacro m [] `helper)\n(ns b)\n(def helper 2)\n(a/m)",
			expected: "1",
		},
		{
			name:     "macroexpand shows qualified symbol for macro body reference",
			input:    "(ns a)\n(def helper 1)\n(pr-str (macroexpand `helper))",
			expected: "\"a/helper\"",
		},
		{
			name:     "macroexpand qualifies referred vars to their source namespace",
			input:    "(ns user (:require [doggo.string :refer [join]]))\n(pr-str (macroexpand `join))",
			expected: "\"doggo.string/join\"",
		},
		{
			name:     "macroexpand qualifies core vars instead of caller locals",
			input:    "(ns user)\n(pr-str (macroexpand `map))",
			expected: "\"doggo.core/map\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestRunnerQuasiquoteAutoGensymReusesBindings(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run("(do (defmacro m [] `(let [x# 1] [x# x#])) (m))")
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "[1 1]" {
		t.Fatalf("expected auto-gensym expansion to reuse the same binding, got %q", got)
	}
}

func TestRunnerQuasiquoteAutoGensymBeatsExistingBindings(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run("(do\n\t\t(def x# 1)\n\t\t(pr-str (macroexpand `x#)))")
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "\"x__1__auto__\"" {
		t.Fatalf("expected syntax-quote to auto-gensym x#, got %q", got)
	}
}

func TestRunnerMacroResolutionRespectsShadowing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "global def disables later macro expansion",
			input: `(do
				(defmacro hi [] 1)
				(pr-str (macroexpand '(hi)))
				(def hi (fn [] 2))
				(pr-str [(macroexpand '(hi)) (hi)]))`,
			expected: "\"[(hi) 2]\"",
		},
		{
			name: "later defmacro overrides existing global var",
			input: `(do
				(def hi (fn [] 2))
				(defmacro hi [] 1)
				(pr-str [(macroexpand '(hi)) (hi)]))`,
			expected: "\"[1 1]\"",
		},
		{
			name: "local binding shadows macro in operator position",
			input: `(do
				(defmacro hi [] 1)
				(let [hi (fn [] 2)]
					(hi)))`,
			expected: "2",
		},
		{
			name: "macro overrides builtin with same name",
			input: `(do
				(defmacro map [] 1)
				(pr-str [(macroexpand '(map)) (map)]))`,
			expected: "\"[1 1]\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("runner error: %s", err)
			}
			if got := result.String(); got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestRunnerLetAndLoopBindingsDoNotLeak(t *testing.T) {
	tests := []string{
		`(do (let [x 1] x) x)`,
		`(do (loop [x 1] x) x)`,
	}

	for _, input := range tests {
		runner := MustNewRunner()
		if _, err := runner.Run(input); err == nil {
			t.Fatalf("expected binding to remain local for %q", input)
		}
	}
}

func TestRunnerCompileFailureDoesNotLeakConstants(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	if _, err := runner.Compile("(do 1 missing)"); err == nil {
		t.Fatal("expected compile error")
	}

	code, err := runner.Compile("42")
	if err != nil {
		t.Fatalf("compile error: %s", err)
	}
	if got := len(code.Constants); got != 1 {
		t.Fatalf("expected failed compile to leave constant pool unchanged, got %d constants", got)
	}
}

func TestRunnerCompileReusesTopLevelLocalsAcrossForms(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	code, err := runner.Compile("(let [x 1] x)\n(let [y 2] y)")
	if err != nil {
		t.Fatalf("compile error: %s", err)
	}
	if got := code.Bytecode.NumLocals; got != 1 {
		t.Fatalf("expected multi-form compile to reuse 1 top-level local slot, got %d", got)
	}

	result, err := runner.RunPrecompiled(code)
	if err != nil {
		t.Fatalf("run precompiled error: %s", err)
	}
	if got := result.String(); got != "2" {
		t.Fatalf("expected last form result 2, got %s", got)
	}
}

func TestRunnerCompileWithOriginDoesNotTrustCallerOriginForStdlibPrivateBuiltins(t *testing.T) {
	runner := MustNewRunnerWithoutCore()
	source := `(ns doggo.math)
(def sqrt
  (fn [x]
    (math-sqrt* x)))`

	if _, err := runner.Compile(source); err == nil || !strings.Contains(err.Error(), "undefined variable: math-sqrt*") {
		t.Fatalf("expected anonymous compile to reject private builtin, got %v", err)
	}

	if _, err := runner.CompileWithOrigin(source, engine.SourceOrigin{File: "stdlib/doggo/math.dog"}); err == nil || !strings.Contains(err.Error(), "undefined variable: math-sqrt*") {
		t.Fatalf("expected CompileWithOrigin to reject caller-spoofed private builtin access, got %v", err)
	}
}

func TestRunnerTrustedCompileWithOriginAllowsStdlibPrivateBuiltins(t *testing.T) {
	runner := MustNewRunnerWithoutCore()
	source := `(ns doggo.math)
(def sqrt
  (fn [x]
    (math-sqrt* x)))`

	code, err := runner.compileWithOriginTrusted(source, engine.SourceOrigin{File: "stdlib/doggo/math.dog"})
	if err != nil {
		t.Fatalf("trusted compile failed: %v", err)
	}

	if _, err := runner.RunPrecompiled(code); err != nil {
		t.Fatalf("RunPrecompiled failed: %v", err)
	}

	result, err := runner.Run(`(doggo.math/sqrt 4)`)
	if err != nil {
		t.Fatalf("doggo.math/sqrt failed: %v", err)
	}
	if got := result.String(); got != "2" {
		t.Fatalf("expected doggo.math/sqrt 4 to be 2, got %s", got)
	}
}

func TestRunnerRunWithOriginDoesNotTrustCallerOriginForStdlibPrivateBuiltins(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	if _, err := runner.RunWithOrigin(`(math-sqrt* 4)`, engine.SourceOrigin{File: "stdlib/doggo/fake.dog"}); err == nil || !strings.Contains(err.Error(), "undefined variable: math-sqrt*") {
		t.Fatalf("expected RunWithOrigin to reject caller-spoofed private builtin access, got %v", err)
	}
}

func TestRunnerAllowsManySequentialSingleSlotBlocks(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	var src strings.Builder
	src.WriteString("(do")
	for i := 0; i <= compiler.MaxLocalSlots; i++ {
		fmt.Fprintf(&src, " (let [x%d %d] %d)", i, i, i)
	}
	src.WriteString(")")

	result, err := runner.Run(src.String())
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != fmt.Sprintf("%d", compiler.MaxLocalSlots) {
		t.Fatalf("expected final sequential block result %d, got %s", compiler.MaxLocalSlots, got)
	}
}

func TestRunnerCompileFailureDoesNotLeakDefinitions(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	if _, err := runner.Compile("(do (def leaked 1) missing)"); err == nil {
		t.Fatal("expected compile error")
	}

	if _, err := runner.Compile("leaked"); err == nil {
		t.Fatal("expected leaked definition to be rolled back")
	}
}

func TestRunnerCompileFailureDoesNotLeakMacros(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Compile("(do (defmacro leaked [] 1) missing)"); err == nil {
		t.Fatal("expected compile error")
	}

	if _, err := runner.Compile("(leaked)"); err == nil {
		t.Fatal("expected leaked macro to be rolled back")
	}
}

func TestRunnerRunFailureDoesNotLeakDefinitionsOrMacros(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Run("(do (def leaked 1) missing)"); err == nil {
		t.Fatal("expected compile error")
	}
	if _, err := runner.Run("leaked"); err == nil {
		t.Fatal("expected leaked definition to be rolled back after Run failure")
	}

	if _, err := runner.Run("(do (defmacro leaked-macro [] 1) missing)"); err == nil {
		t.Fatal("expected compile error")
	}
	if _, err := runner.Run("(leaked-macro)"); err == nil {
		t.Fatal("expected leaked macro to be rolled back after Run failure")
	}
}

func TestRunnerCompileFailureRestoresNamespaceState(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Run("(ns base)"); err != nil {
		t.Fatalf("runner error: %s", err)
	}

	if _, err := runner.Compile("(do (ns leaked.ns) missing)"); err == nil {
		t.Fatal("expected compile error")
	}

	if got := runner.CurrentNamespace(); got != "base" {
		t.Fatalf("expected compile failure to restore current namespace to base, got %q", got)
	}

	result, err := runner.Run("*ns*")
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "base" {
		t.Fatalf("expected *ns* to remain base after compile failure, got %q", got)
	}
}

func TestRunnerRunFailureRestoresNamespaceState(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Run("(ns base)"); err != nil {
		t.Fatalf("runner error: %s", err)
	}

	if _, err := runner.Run("(do (ns leaked.ns) missing)"); err == nil {
		t.Fatal("expected compile error")
	}

	if got := runner.CurrentNamespace(); got != "base" {
		t.Fatalf("expected run failure to restore current namespace to base, got %q", got)
	}

	result, err := runner.Run("*ns*")
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "base" {
		t.Fatalf("expected *ns* to remain base after run failure, got %q", got)
	}
}
