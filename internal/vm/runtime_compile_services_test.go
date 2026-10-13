package vm

import (
	"errors"
	"strings"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/syntax/frontend"
	"doggo/internal/vm/engine"
)

func TestRunnerCompileServicesCompileSourceIncludesOriginSnippet(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	_, err := runner.runtimeServices.compile.compileSource("(undefined-var)", engine.SourceOrigin{File: "app/compile.dog"}, false)
	if err == nil {
		t.Fatal("expected compile error")
	}

	var compileErr *compiler.CompileError
	if !errors.As(err, &compileErr) {
		t.Fatalf("expected CompileError, got %T", err)
	}
	if compileErr.File != "app/compile.dog" {
		t.Fatalf("compileErr.File = %q, want app/compile.dog", compileErr.File)
	}
	if compileErr.Line != 1 {
		t.Fatalf("compileErr.Line = %d, want 1", compileErr.Line)
	}
	if compileErr.SnippetLine != "(undefined-var)" {
		t.Fatalf("compileErr.SnippetLine = %q", compileErr.SnippetLine)
	}
}

func TestRunnerCompileServicesCompileSourceUsesCompilerSession(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	_, err := runner.runtimeServices.compile.compileSource("(do (defmacro m [] 1) (m))", engine.SourceOrigin{File: "app/session.dog"}, false)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, ok := runner.macroExpander.ResolveMacroTarget("m"); !ok {
		t.Fatal("expected compileSource to publish macro definitions through session path")
	}
}

func TestRunnerCompileServicesCompileParsedRestoresStateOnFailure(t *testing.T) {
	runner := MustNewRunnerWithoutCore()
	source := "(do (def leaked 1) missing)"
	origin := engine.SourceOrigin{File: "app/parsed.dog"}
	runner.registerSource(origin, source)
	constantLen := runner.getOrCreateExecutionSession().constantStore.Len()

	reader, err := frontend.NewReader(origin.File, source)
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}
	nodes, err := reader.ReadSyntaxAll()
	if err != nil {
		t.Fatalf("ReadSyntaxAll failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d", len(nodes))
	}

	if _, err := runner.runtimeServices.compile.compileSyntaxNode(nodes[0], origin, false); err == nil {
		t.Fatal("expected compile error")
	}

	if got := runner.getOrCreateExecutionSession().constantStore.Len(); got != constantLen {
		t.Fatalf("constant store len = %d, want %d", got, constantLen)
	}
	if _, err := runner.Compile("leaked"); err == nil || !strings.Contains(err.Error(), "undefined variable: leaked") {
		t.Fatalf("expected leaked definition to be rolled back, got %v", err)
	}
}

func TestRunnerRunUsesSyntaxReaderAndSessionPath(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	if _, err := runner.Run("(defmacro m [] 3)"); err != nil {
		t.Fatalf("defmacro failed: %v", err)
	}

	result, err := runner.Run("(m)")
	if err != nil {
		t.Fatalf("macro call failed: %v", err)
	}
	if got := result.String(); got != "3" {
		t.Fatalf("result = %s, want 3", got)
	}
}

func TestRunnerCompileServicesPreserveSourceFormOffsetsForIntrospection(t *testing.T) {
	runner := MustNewRunner()

	source := `(ns my.app)
(def ^{:doc "double value"} my-fn (fn [x] (* x 2)))`
	if _, err := runner.RunWithOrigin(source, engine.SourceOrigin{File: "src/my_app.dog"}); err != nil {
		t.Fatalf("RunWithOrigin failed: %v", err)
	}

	result, err := runner.Run(`(source-str* 'my-fn)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(result.String(), `(def ^{:doc "double value"} my-fn (fn [x] (* x 2)))`) {
		t.Fatalf("expected original definition form, got %s", result.String())
	}

	docResult, err := runner.Run(`(doc* 'my-fn)`)
	if err != nil {
		t.Fatalf("doc* failed: %v", err)
	}
	if !strings.Contains(docResult.String(), "src/my_app.dog:2:1") {
		t.Fatalf("expected source span metadata to survive compile path, got %s", docResult.String())
	}
}
