package vm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNsVarBasic は *ns* が ns/in-ns で正しく更新されることをテストします。
func TestNsVarBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ns updates *ns* to namespace symbol",
			input:    `(ns test.ns) *ns*`,
			expected: "test.ns",
		},
		{
			name:     "in-ns updates *ns*",
			input:    `(ns a) (in-ns 'b) *ns*`,
			expected: "b",
		},
		{
			name:     "multiple ns changes",
			input:    `(ns first) (ns second) (ns third) *ns*`,
			expected: "third",
		},
		{
			name:     "*ns* starts as user",
			input:    `*ns*`,
			expected: "user",
		},
		{
			name:     "ns with require still updates *ns*",
			input:    `(ns my.app (:require [doggo.core])) *ns*`,
			expected: "my.app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestNsVarInMacroExpansion は deftest 内で *ns* が正しく解決されることをテストします。
func TestNsVarInMacroExpansion(t *testing.T) {
	runner := MustNewRunner()

	// doggo.test を require
	_, err := runner.Run(`(require 'doggo.test)`)
	if err != nil {
		t.Fatalf("failed to require doggo.test: %v", err)
	}

	// 名前空間を切り替えてテストを定義・実行
	_, err = runner.Run(`(ns test.example)`)
	if err != nil {
		t.Fatalf("failed to switch namespace: %v", err)
	}

	// deftest が *ns* を使ってテストを登録できることを確認
	_, err = runner.Run(`
		(doggo.test/deftest simple-test
			(doggo.test/is (= 1 1)))
	`)
	if err != nil {
		t.Fatalf("failed to define test: %v", err)
	}

	// run-tests を実行して summary を検証
	counts, err := runner.Run(`(let [result (doggo.test/run-tests 'test.example)]
		[(:test (:summary result)) (:pass (:summary result))])`)
	if err != nil {
		t.Fatalf("failed to run tests: %v", err)
	}
	if counts.String() != "[1 1]" {
		t.Errorf("expected [1 1], got: %s", counts.String())
	}
}

// TestDoggoTestFramework は doggo.test の基本動作をテストします。
func TestDoggoTestFramework(t *testing.T) {
	runner := MustNewRunner()

	// 一連のテストを実行
	_, err := runner.Run(`
		(require 'doggo.test)
		(ns my.tests)
		
		(doggo.test/deftest arithmetic-test
			(doggo.test/is (= (+ 1 2) 3))
			(doggo.test/is (= (* 3 4) 12)))
		
		(doggo.test/deftest string-test
			(doggo.test/testing "string concatenation"
				(doggo.test/is (= (str "a" "b") "ab"))))
		
		(doggo.test/run-tests 'my.tests)
	`)
	if err != nil {
		t.Fatalf("test framework execution failed: %v", err)
	}

	counts, err := runner.Run(`(let [summary (:summary (doggo.test/run-tests 'my.tests))]
		[(:test summary) (:fail summary)])`)
	if err != nil {
		t.Fatalf("failed to read summary counts: %v", err)
	}
	if counts.String() != "[2 0]" {
		t.Errorf("expected [2 0], got: %s", counts.String())
	}
}

func TestLoadFileNsRequirePreservesNsVarAfterLoadingStdlib(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "ns_require.dog")
	source := `(ns drift.app
  (:require [doggo.time :as time]
            [doggo.test :as t]))
*ns*
`
	if err := os.WriteFile(filePath, []byte(source), 0644); err != nil {
		t.Fatalf("write ns_require.dog: %v", err)
	}

	runner := MustNewRunner()
	result, err := runner.LoadFile(filePath)
	if err != nil {
		t.Fatalf("load-file failed: %v", err)
	}
	if result.String() != "drift.app" {
		t.Fatalf("expected *ns* to remain drift.app after load-file, got %s", result.String())
	}

	after, err := runner.Run(`*ns*`)
	if err != nil {
		t.Fatalf("reading *ns* after load-file failed: %v", err)
	}
	if after.String() != "drift.app" {
		t.Fatalf("expected runtime *ns* = drift.app after load-file, got %s", after.String())
	}
}
