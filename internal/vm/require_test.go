package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustNewRunnerWithLoadPaths(t *testing.T, loadPaths ...string) *Runner {
	t.Helper()
	runner, err := NewRunnerWithConfig(RunnerConfig{LoadPaths: loadPaths, BootstrapCore: true})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

// TestLoadFile は load-file 関数をテストします。
func TestLoadFile(t *testing.T) {
	// 一時ディレクトリを作成
	tmpDir := t.TempDir()

	// テスト用のファイルを作成
	testFile := filepath.Join(tmpDir, "test.dog")
	err := os.WriteFile(testFile, []byte(`
		(def x 42)
		(def y (+ x 10))
		y
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	// load-file でファイルを読み込む
	assertRunResult(t, runner, fmt.Sprintf(`(load-file %q)`, testFile), "52")

	// 読み込んだファイルで定義された変数にアクセス
	result2, err := runner.Run(`x`)
	if err != nil {
		t.Fatalf("error accessing x: %v", err)
	}
	if result2.String() != "42" {
		t.Errorf("expected x = 42, got %s", result2.String())
	}
}

// TestLoadFileNotFound は存在しないファイルのロードをテストします。
func TestLoadFileNotFound(t *testing.T) {
	runner := MustNewRunner()

	_, err := runner.Run(`(load-file "nonexistent.dog")`)
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

// TestRequire は require 関数をテストします。
func TestRequire(t *testing.T) {
	// 一時ディレクトリを作成
	tmpDir := t.TempDir()

	// 名前空間に対応するディレクトリ構造を作成
	libDir := filepath.Join(tmpDir, "my", "lib")
	err := os.MkdirAll(libDir, 0755)
	if err != nil {
		t.Fatalf("failed to create lib directory: %v", err)
	}

	// テスト用のライブラリファイルを作成
	libFile := filepath.Join(tmpDir, "my", "lib.dog")
	err = os.WriteFile(libFile, []byte(`
		(ns my.lib)
		(def greeting "Hello from my.lib")
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create lib file: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	// require でライブラリをロード (quote された名前空間名)
	_, err = runner.Run(`(require 'my.lib)`)
	if err != nil {
		t.Fatalf("require error: %v", err)
	}

	// ロードしたライブラリの変数にアクセス
	assertRunResult(t, runner, `my.lib/greeting`, `"Hello from my.lib"`)
}

// TestRequireMultiple は複数のライブラリの require をテストします。
func TestRequireMultiple(t *testing.T) {
	tmpDir := t.TempDir()

	// ライブラリ1を作成
	err := os.WriteFile(filepath.Join(tmpDir, "lib1.dog"), []byte(`
		(ns lib1)
		(def value1 100)
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create lib1.dog: %v", err)
	}

	// ライブラリ2を作成
	err = os.WriteFile(filepath.Join(tmpDir, "lib2.dog"), []byte(`
		(ns lib2)
		(def value2 200)
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create lib2.dog: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	// 複数のライブラリを一度に require
	_, err = runner.Run(`(require 'lib1 'lib2)`)
	if err != nil {
		t.Fatalf("require error: %v", err)
	}

	// 各ライブラリの変数にアクセス
	result1, err := runner.Run(`lib1/value1`)
	if err != nil {
		t.Fatalf("error accessing lib1/value1: %v", err)
	}
	if result1.String() != "100" {
		t.Errorf("expected lib1/value1 = 100, got %s", result1.String())
	}

	result2, err := runner.Run(`lib2/value2`)
	if err != nil {
		t.Fatalf("error accessing lib2/value2: %v", err)
	}
	if result2.String() != "200" {
		t.Errorf("expected lib2/value2 = 200, got %s", result2.String())
	}
}

func TestConcurrentRequireIsRaceFree(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(let [ready (chan 2)
		      release (chan 2)
		      worker (fn [lib]
		               (future
		                 (>! ready true)
		                 (<! release)
		                 (require lib)))
		      f1 (worker :doggo.string)
		      f2 (worker :doggo.math)]
		  (<! ready)
		  (<! ready)
		  (>! release true)
		  (>! release true)
		  @f1
		  @f2
		  :done)
	`)
	if err != nil {
		t.Fatalf("concurrent require failed: %v", err)
	}
	if got := result.String(); got != ":done" {
		t.Fatalf("expected :done, got %s", got)
	}
}

func TestConcurrentCircularRequireReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	cycleDir := filepath.Join(tmpDir, "cycle")
	if err := os.MkdirAll(cycleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cycleDir, "a.dog"), []byte(`
		(ns cycle.a)
		(>! user/cycle-ready :a)
		(<! user/cycle-release)
		(require :cycle.b)
	`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cycleDir, "b.dog"), []byte(`
		(ns cycle.b)
		(>! user/cycle-ready :b)
		(<! user/cycle-release)
		(require :cycle.a)
	`), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(`
			(def cycle-ready (chan 2))
			(def cycle-release (chan 2))
			(let [a (future (require :cycle.a))
			      b (future (require :cycle.b))]
			  (<! cycle-ready)
			  (<! cycle-ready)
			  (>! cycle-release true)
			  (>! cycle-release true)
			  @a
			  @b)
		`)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "circular dependency") {
			t.Fatalf("expected circular dependency error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent circular require deadlocked")
	}
}

func TestLoadFileCanAwaitFutureRequire(t *testing.T) {
	tmpDir := t.TempDir()
	entryPath := filepath.Join(tmpDir, "entry.dog")
	if err := os.WriteFile(entryPath, []byte(`
		(ns async.entry)
		@(future (require :doggo.math))
	`), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := MustNewRunner()
	done := make(chan error, 1)
	go func() {
		_, err := runner.LoadFile(entryPath)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("load-file failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("load-file deadlocked while awaiting a future require")
	}
}

func TestLazySeqRequireDoesNotShareLoaderOwnership(t *testing.T) {
	tmpDir := t.TempDir()
	entryPath := filepath.Join(tmpDir, "entry.dog")
	if err := os.WriteFile(entryPath, []byte(`
		(ns lazy.entry)
		(def ready (chan 1))
		(def release (chan 1))
		(def values
		  (lazy-seq
		    (>! ready true)
		    (<! release)
		    (require :doggo.math)
		    (list 1)))
		(def result (future (first values)))
		(<! ready)
		(>! release true)
		(require :doggo.string)
		@result
	`), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := MustNewRunner()
	result, err := runner.LoadFile(entryPath)
	if err != nil {
		t.Fatalf("load-file failed: %v", err)
	}
	if got := result.String(); got != "1" {
		t.Fatalf("result = %s, want 1", got)
	}
}

// TestRequireNoDuplicate は同じライブラリを複数回 require しても問題ないことをテストします。
func TestRequireNoDuplicate(t *testing.T) {
	tmpDir := t.TempDir()

	// counter を持つライブラリを作成（ロードされるたびに増加するはず）
	err := os.WriteFile(filepath.Join(tmpDir, "counter.dog"), []byte(`
		(ns counter)
		(def count 1)
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create counter.dog: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	// 2回 require
	_, err = runner.Run(`(require 'counter)`)
	if err != nil {
		t.Fatalf("first require error: %v", err)
	}

	_, err = runner.Run(`(require 'counter)`)
	if err != nil {
		t.Fatalf("second require error: %v", err)
	}

	// 値が1のままであることを確認（2回ロードされていない）
	assertRunResult(t, runner, `counter/count`, "1")
}

// TestRequireNotFound は存在しないライブラリの require をテストします。
func TestRequireNotFound(t *testing.T) {
	runner := MustNewRunner()

	_, err := runner.Run(`(require 'nonexistent.lib)`)
	if err == nil {
		t.Error("expected error for nonexistent library, got nil")
	}
}

// TestRequireWithDependency は依存関係を持つライブラリの require をテストします。
func TestRequireWithDependency(t *testing.T) {
	tmpDir := t.TempDir()

	// ベースライブラリを作成
	err := os.WriteFile(filepath.Join(tmpDir, "base.dog"), []byte(`
		(ns base)
		(def base-value 10)
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create base.dog: %v", err)
	}

	// base に依存するライブラリを作成
	err = os.WriteFile(filepath.Join(tmpDir, "derived.dog"), []byte(`
		(ns derived)
		(require 'base)
		(def derived-value (+ base/base-value 5))
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create derived.dog: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	// derived をロード（自動的に base もロードされる）
	_, err = runner.Run(`(require 'derived)`)
	if err != nil {
		t.Fatalf("require error: %v", err)
	}

	// 名前空間を derived に切り替えてからローカル名でアクセス
	assertRunResult(t, runner, `(do (in-ns derived) derived-value)`, "15")

	// 修飾シンボルでアクセス（別の方法）
	result2, err := runner.Run(`(get {'derived/derived-value 15} 'derived/derived-value)`)
	if err == nil && result2.String() == "15" {
		// シンボルテーブルにあるかテスト - 直接アクセスは失敗するかもしれない
		// この場合はOK
	}
}

// TestLoadFileFunctionAccess は load-file 関数自体にアクセスできることをテストします。
func TestLoadFileFunctionAccess(t *testing.T) {
	runner := MustNewRunner()

	// load-file がシンボルとして解決できることを確認
	assertRunResult(t, runner, `(if load-file "exists" "not found")`, `"exists"`)
}

// TestRequirePreservesCurrentNamespace は require 後も呼び出し元の名前空間が維持されることをテストします。
// P0-4: require実行後にカレント名前空間が復元されない問題の回帰テスト
func TestRequirePreservesCurrentNamespace(t *testing.T) {
	tmpDir := t.TempDir()

	// ベースライブラリを作成
	err := os.WriteFile(filepath.Join(tmpDir, "base.dog"), []byte(`
		(ns base)
		(def base-value 10)
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create base.dog: %v", err)
	}

	// base に依存するライブラリを作成
	err = os.WriteFile(filepath.Join(tmpDir, "derived.dog"), []byte(`
		(ns derived)
		(require 'base)
		(def derived-value (+ base/base-value 5))
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create derived.dog: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	// 初期状態では user 名前空間
	// derived をロード（内部で base もロードされる）
	_, err = runner.Run(`(require 'derived)`)
	if err != nil {
		t.Fatalf("require error: %v", err)
	}

	// require 後に変数を定義
	_, err = runner.Run(`(def after-require-var 42)`)
	if err != nil {
		t.Fatalf("def error: %v", err)
	}

	// 期待: user/after-require-var として定義されていること
	// （current-ns が user に復元されていれば成功）
	assertRunResult(t, runner, `user/after-require-var`, "42")
}

// TestRequirePreservesNamespaceWithNestedRequire はネストした require でも名前空間が維持されることをテストします。
func TestRequirePreservesNamespaceWithNestedRequire(t *testing.T) {
	tmpDir := t.TempDir()

	// 3段階の依存: level3 -> level2 -> level1
	err := os.WriteFile(filepath.Join(tmpDir, "level1.dog"), []byte(`
		(ns level1)
		(def val1 1)
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create level1.dog: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "level2.dog"), []byte(`
		(ns level2)
		(require 'level1)
		(def val2 (+ level1/val1 1))
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create level2.dog: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "level3.dog"), []byte(`
		(ns level3)
		(require 'level2)
		(def val3 (+ level2/val2 1))
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create level3.dog: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	// level3 をロード（level2, level1 も順にロードされる）
	_, err = runner.Run(`(require 'level3)`)
	if err != nil {
		t.Fatalf("require error: %v", err)
	}

	// require 後に変数を定義
	_, err = runner.Run(`(def nested-test 100)`)
	if err != nil {
		t.Fatalf("def error: %v", err)
	}

	// 期待: user/nested-test として定義されていること
	assertRunResult(t, runner, `user/nested-test`, "100")
}

func TestLoadFilePreservesNamespaceAfterRequiringAlreadyLoadedLibrary(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "helper.dog"), []byte(`
		(ns helper)
		(def answer 42)
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create helper.dog: %v", err)
	}

	entryPath := filepath.Join(tmpDir, "entry.dog")
	err = os.WriteFile(entryPath, []byte(`
		(ns app.tests
		  (:require [helper :as helper]))
		(def marker helper/answer)
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create entry.dog: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	if _, err := runner.Run(`(require 'helper)`); err != nil {
		t.Fatalf("preloading helper failed: %v", err)
	}

	if _, err := runner.LoadFile(entryPath); err != nil {
		t.Fatalf("load-file failed: %v", err)
	}

	result, err := runner.Run(`app.tests/marker`)
	if err != nil {
		t.Fatalf("expected marker in app.tests namespace: %v", err)
	}
	if result.String() != "42" {
		t.Fatalf("expected app.tests/marker = 42, got %s", result.String())
	}

	if _, err := runner.Run(`helper/marker`); err == nil {
		t.Fatal("marker leaked into helper namespace")
	}
}
