package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/vm/engine"
)

func TestRunWithOriginCompileErrorIncludesFileLineColumnAndSnippet(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	_, err := runner.RunWithOrigin("(undefined-var)", engine.SourceOrigin{File: "app/main.dog"})
	if err == nil {
		t.Fatal("expected compile error, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "app/main.dog:1:") || !strings.Contains(errStr, "undefined variable: undefined-var") {
		t.Fatalf("expected file:line:column compile error, got:\n%s", errStr)
	}
	if !strings.Contains(errStr, "1 | (undefined-var)") {
		t.Fatalf("expected snippet line, got:\n%s", errStr)
	}
	if !strings.Contains(errStr, "^") {
		t.Fatalf("expected caret line, got:\n%s", errStr)
	}
}

func TestRunWithOriginParserErrorIncludesFileLineColumnAndSnippet(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	_, err := runner.RunWithOrigin("(+ 1", engine.SourceOrigin{File: "app/parse.dog"})
	if err == nil {
		t.Fatal("expected parser error, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "app/parse.dog:1:") || !strings.Contains(errStr, "unexpected EOF while parsing list") {
		t.Fatalf("expected parser diagnostic with file:line:column, got:\n%s", errStr)
	}
	if !strings.Contains(errStr, "1 | (+ 1") {
		t.Fatalf("expected parser snippet line, got:\n%s", errStr)
	}
	if !strings.Contains(errStr, "^") {
		t.Fatalf("expected parser caret, got:\n%s", errStr)
	}
}

func TestRunWithOriginRuntimeErrorIncludesFileLineColumnAndSnippet(t *testing.T) {
	runner := MustNewRunnerWithoutCore()

	_, err := runner.RunWithOrigin("(/ 1 0)", engine.SourceOrigin{File: "app/runtime.dog"})
	if err == nil {
		t.Fatal("expected runtime error, got nil")
	}

	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.File != "app/runtime.dog" {
		t.Fatalf("unexpected runtime file: %q", rtErr.File)
	}
	if rtErr.Line != 1 {
		t.Fatalf("unexpected runtime line: %d", rtErr.Line)
	}
	if rtErr.Column <= 0 {
		t.Fatalf("expected runtime column > 0, got %d", rtErr.Column)
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "app/runtime.dog:1:") || !strings.Contains(errStr, "division by zero") {
		t.Fatalf("expected formatted runtime error, got:\n%s", errStr)
	}
	if !strings.Contains(errStr, "1 | (/ 1 0)") {
		t.Fatalf("expected runtime snippet line, got:\n%s", errStr)
	}
	if !strings.Contains(errStr, "Stack trace:") {
		t.Fatalf("expected stack trace, got:\n%s", errStr)
	}
	if len(rtErr.StackTrace) == 0 {
		t.Fatal("expected non-empty stack trace")
	}
	if rtErr.StackTrace[0].File != "app/runtime.dog" {
		t.Fatalf("unexpected frame file: %q", rtErr.StackTrace[0].File)
	}
}

func TestRuntimeErrorDiagnosticsExposeCanonicalExData(t *testing.T) {
	t.Run("runtime_error", func(t *testing.T) {
		rtErr := &RuntimeError{
			Message:       "division by zero",
			Value:         atom.NewString("(/ 1 0)"),
			File:          "app/runtime.dog",
			Line:          7,
			Column:        3,
			CorrelationID: "corr-123",
			Snippet:       "(/ 1 0)",
			SnippetCol:    3,
			StackTrace: []StackFrame{{
				File:   "app/runtime.dog",
				Line:   7,
				Column: 3,
				Name:   "boom",
			}},
		}

		data := rtErr.ExceptionData()
		if got := data.Data["correlation-id"]; got != "corr-123" {
			t.Fatalf("unexpected runtime correlation ID payload: %#v", data.Data)
		}
		if got := data.Data["snippet"]; got != "(/ 1 0)" {
			t.Fatalf("unexpected runtime snippet payload: %#v", data.Data)
		}
		if got := data.Data["snippet_column"]; got != 3 {
			t.Fatalf("unexpected runtime snippet column payload: %#v", data.Data)
		}
		if got := data.At.File; got != "app/runtime.dog" {
			t.Fatalf("unexpected runtime at file: %q", got)
		}

		mapped := mustMap(t, canonicalExceptionMapValue(data))
		if got := mustString(t, mapped.Get(atom.NewKeyword("phase"))); got != "runtime" {
			t.Fatalf("unexpected phase: %s", got)
		}
		if mapped.Get(atom.NewKeyword("cause")) != atom.NilValue {
			t.Fatalf("expected nil runtime root cause, got %T %v", mapped.Get(atom.NewKeyword("cause")), mapped.Get(atom.NewKeyword("cause")))
		}
		payload := mustMap(t, mapped.Get(atom.NewKeyword("data")))
		if got := mustString(t, payload.Get(atom.NewKeyword("correlation-id"))); got != "corr-123" {
			t.Fatalf("unexpected mapped correlation ID: %s", got)
		}
		if got := mustString(t, payload.Get(atom.NewKeyword("snippet"))); got != "(/ 1 0)" {
			t.Fatalf("unexpected mapped snippet: %s", got)
		}
		trace := mustVector(t, mapped.Get(atom.NewKeyword("trace")))
		if len(trace.Items) != 1 {
			t.Fatalf("unexpected trace length: %d", len(trace.Items))
		}
	})

	t.Run("compile_error", func(t *testing.T) {
		runner := MustNewRunnerWithoutCore()
		value := runner.exceptionFromError(&compiler.CompileError{
			File:        "app/compile.dog",
			Message:     "undefined variable: demo",
			Line:        5,
			Column:      11,
			SnippetLine: "(demo)",
			SnippetCol:  6,
		})

		exc, ok := value.(*atom.Exception)
		if !ok {
			t.Fatalf("expected Exception, got %T", value)
		}
		if exc.CanonicalData == nil {
			t.Fatal("expected canonical compile data")
		}
		if exc.CanonicalData.Type != "CompileError" {
			t.Fatalf("unexpected compile type: %q", exc.CanonicalData.Type)
		}
		if got := exc.CanonicalData.Data["snippet"]; got != "(demo)" {
			t.Fatalf("unexpected compile snippet payload: %#v", exc.CanonicalData.Data)
		}
		if got := exc.CanonicalData.Data["snippet_column"]; got != 6 {
			t.Fatalf("unexpected compile snippet column payload: %#v", exc.CanonicalData.Data)
		}
		if got := exc.CanonicalData.Data["formatted_snippet"]; got != "5 | (demo)\n  |      ^" {
			t.Fatalf("unexpected formatted snippet payload: %#v", exc.CanonicalData.Data)
		}

		mapped := mustMap(t, canonicalExceptionMapValue(exc.CanonicalData))
		if got := mustString(t, mapped.Get(atom.NewKeyword("phase"))); got != "compile" {
			t.Fatalf("unexpected compile phase: %s", got)
		}
		if got := mustString(t, mapped.Get(atom.NewKeyword("message"))); got != "undefined variable: demo" {
			t.Fatalf("unexpected compile message: %s", got)
		}
		at := mustMap(t, mapped.Get(atom.NewKeyword("at")))
		if got := mustString(t, at.Get(atom.NewKeyword("file"))); got != "app/compile.dog" {
			t.Fatalf("unexpected compile at file: %s", got)
		}
		if got := mustInt(t, at.Get(atom.NewKeyword("line"))); got != 5 {
			t.Fatalf("unexpected compile at line: %d", got)
		}
		if got := mustInt(t, at.Get(atom.NewKeyword("column"))); got != 11 {
			t.Fatalf("unexpected compile at column: %d", got)
		}
		payload := mustMap(t, mapped.Get(atom.NewKeyword("data")))
		if got := mustString(t, payload.Get(atom.NewKeyword("snippet"))); got != "(demo)" {
			t.Fatalf("unexpected mapped compile snippet: %s", got)
		}
	})
}

func TestLoadFileRuntimeErrorKeepsFileOrigin(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "broken.dog")
	if err := os.WriteFile(filePath, []byte("(/ 1 0)\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	runner := MustNewRunnerWithoutCore()
	_, err := runner.Run(fmt.Sprintf("(load-file %q)", filePath))
	if err == nil {
		t.Fatal("expected runtime error, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "broken.dog:1:") || !strings.Contains(errStr, "division by zero") {
		t.Fatalf("expected load-file origin in runtime error, got:\n%s", errStr)
	}
}

func TestLoadFileResolvesRelativePathFromCallingFile(t *testing.T) {
	projectRoot := t.TempDir()
	entryPath := filepath.Join(projectRoot, "scripts", "main.dog")
	helperPath := filepath.Join(projectRoot, "scripts", "helpers", "nested.dog")
	sharedPath := filepath.Join(projectRoot, "scripts", "shared", "value.dog")

	for _, path := range []string{entryPath, helperPath, sharedPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir parent dir: %v", err)
		}
	}
	if err := os.WriteFile(entryPath, []byte("(load-file \"helpers/nested.dog\")\nshared-value\n"), 0644); err != nil {
		t.Fatalf("write entry file: %v", err)
	}
	if err := os.WriteFile(helperPath, []byte("(load-file \"../shared/value.dog\")\n"), 0644); err != nil {
		t.Fatalf("write helper file: %v", err)
	}
	if err := os.WriteFile(sharedPath, []byte("(def shared-value 42)\n"), 0644); err != nil {
		t.Fatalf("write shared file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: false})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	value, err := runner.LoadFile(entryPath)
	if err != nil {
		t.Fatalf("load entry file: %v", err)
	}
	if got := value.String(); got != "42" {
		t.Fatalf("unexpected loaded value: %s", got)
	}
}

func TestNewRunnerWithProjectConfigLoadFileFromREPLUsesProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	libPath := filepath.Join(projectRoot, "support", "answer.dog")
	if err := os.MkdirAll(filepath.Dir(libPath), 0755); err != nil {
		t.Fatalf("mkdir lib dir: %v", err)
	}
	if err := os.WriteFile(libPath, []byte("(def answer 42)\n"), 0644); err != nil {
		t.Fatalf("write lib file: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	outsideDir := t.TempDir()
	if err := os.Chdir(outsideDir); err != nil {
		t.Fatalf("chdir outside project: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: true})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	value, err := runner.Run("(load-file \"support/answer.dog\")\nanswer")
	if err != nil {
		t.Fatalf("load-file from repl source: %v", err)
	}
	if got := value.String(); got != "42" {
		t.Fatalf("unexpected answer: %s", got)
	}
}

func TestLoadFileTrustsResolvedProjectStdlibImplementationFiles(t *testing.T) {
	projectRoot := t.TempDir()
	stdlibPath := filepath.Join(projectRoot, "stdlib", "doggo", "math.dog")
	if err := os.MkdirAll(filepath.Dir(stdlibPath), 0755); err != nil {
		t.Fatalf("mkdir stdlib dir: %v", err)
	}
	source := `(ns doggo.math)
(def sqrt
  (fn [x]
    (math-sqrt* x)))
(sqrt 4)
`
	if err := os.WriteFile(stdlibPath, []byte(source), 0644); err != nil {
		t.Fatalf("write stdlib file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: false})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	value, err := runner.LoadFile(stdlibPath)
	if err != nil {
		t.Fatalf("load stdlib file: %v", err)
	}
	if got := value.String(); got != "2" {
		t.Fatalf("unexpected stdlib result: %s", got)
	}
}

func TestLoadFileDoesNotTrustProjectStdlibTestFiles(t *testing.T) {
	projectRoot := t.TempDir()
	testPath := filepath.Join(projectRoot, "stdlib", "doggo", "math_test.dog")
	if err := os.MkdirAll(filepath.Dir(testPath), 0755); err != nil {
		t.Fatalf("mkdir stdlib dir: %v", err)
	}
	if err := os.WriteFile(testPath, []byte("(math-sqrt* 4)\n"), 0644); err != nil {
		t.Fatalf("write stdlib test file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: false})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	if _, err := runner.LoadFile(testPath); err == nil || !strings.Contains(err.Error(), "undefined variable: math-sqrt*") {
		t.Fatalf("expected stdlib test file to stay untrusted, got %v", err)
	}
}

func TestLoadFileInFunctionUsesFunctionSourceFile(t *testing.T) {
	projectRoot := t.TempDir()
	entryPath := filepath.Join(projectRoot, "main.dog")
	loaderPath := filepath.Join(projectRoot, "lib", "loader.dog")
	sharedPath := filepath.Join(projectRoot, "lib", "shared", "value.dog")

	for _, path := range []string{entryPath, loaderPath, sharedPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir parent dir: %v", err)
		}
	}
	if err := os.WriteFile(entryPath, []byte("(load-file \"lib/loader.dog\")\n(load-shared)\n"), 0644); err != nil {
		t.Fatalf("write entry file: %v", err)
	}
	if err := os.WriteFile(loaderPath, []byte("(def load-shared\n  (fn []\n    (load-file \"shared/value.dog\")))\n"), 0644); err != nil {
		t.Fatalf("write loader file: %v", err)
	}
	if err := os.WriteFile(sharedPath, []byte("42\n"), 0644); err != nil {
		t.Fatalf("write shared file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: false})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	value, err := runner.LoadFile(entryPath)
	if err != nil {
		t.Fatalf("load entry file: %v", err)
	}
	if got := value.String(); got != "42" {
		t.Fatalf("unexpected loaded value: %s", got)
	}
}

func TestVMApplyUsesVMContextForTypesFn(t *testing.T) {
	projectRoot := t.TempDir()
	sharedPath := filepath.Join(projectRoot, "lib", "shared", "value.dog")
	if err := os.MkdirAll(filepath.Dir(sharedPath), 0755); err != nil {
		t.Fatalf("mkdir shared dir: %v", err)
	}
	if err := os.WriteFile(sharedPath, []byte("42\n"), 0644); err != nil {
		t.Fatalf("write shared file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: false})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	fn := &loadFileFn{namespaces: runner.runtimeNamespaces()}
	vm, err := New(&compiler.Bytecode{SourceFile: "lib/loader.dog"})
	if err != nil {
		t.Fatalf("new vm: %v", err)
	}
	vm.installCallableRuntime(newRunnerExactCallableRuntime(runner))

	value, err := vm.Apply(context.Background(), fn, []types.Value{atom.NewString("shared/value.dog")})
	if err != nil {
		t.Fatalf("vm.Apply failed: %v", err)
	}
	if got := value.String(); got != "42" {
		t.Fatalf("unexpected loaded value: %s", got)
	}
}

func TestVMCallbackInvokerUsesVMContextForTypesFn(t *testing.T) {
	projectRoot := t.TempDir()
	sharedPath := filepath.Join(projectRoot, "lib", "shared", "value.dog")
	if err := os.MkdirAll(filepath.Dir(sharedPath), 0755); err != nil {
		t.Fatalf("mkdir shared dir: %v", err)
	}
	if err := os.WriteFile(sharedPath, []byte("42\n"), 0644); err != nil {
		t.Fatalf("write shared file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: false})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	fn := &loadFileFn{namespaces: runner.runtimeNamespaces()}
	vm, err := New(&compiler.Bytecode{SourceFile: "lib/loader.dog"})
	if err != nil {
		t.Fatalf("new vm: %v", err)
	}
	vm.installCallableRuntime(newRunnerExactCallableRuntime(runner))
	invoker := NewVMCallbackInvoker(vm)

	value, err := invoker.Invoke(context.Background(), fn, []types.Value{atom.NewString("shared/value.dog")}, nil)
	if err != nil {
		t.Fatalf("callback invoker failed: %v", err)
	}
	if got := value.String(); got != "42" {
		t.Fatalf("unexpected loaded value: %s", got)
	}
}

func TestGenerationSourceVMCallbackInvokerRetainsVerifiedOrigin(t *testing.T) {
	_, session, _ := openVMGenerationSession(t)
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:       session.Config.ProjectRoot,
		LoadPaths:         session.Config.LoadPaths,
		GenerationSources: session.Config.GenerationSources,
		Lock:              session.Config.Lock,
		BootstrapCore:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run("(require 'dep.core)"); err != nil {
		t.Fatalf("require dependency: %v", err)
	}

	generationSource := session.Config.GenerationSources[0]
	originFile := generationSource.DisplayPath("dep/core.dog")
	origin, found := runner.resolveSourceOrigin(originFile)
	if !found || origin.GenerationSource != generationSource || origin.RelativePath != "dep/core.dog" {
		t.Fatalf("registered origin = %#v, found = %v", origin, found)
	}

	vm, err := runner.runtimeServices.execution.newMachine(
		&compiler.Bytecode{SourceFile: originFile},
		runner.contextForExecution(),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if frame := vm.currentFrame(); frame == nil || frame.CurrentFile() != originFile {
		t.Fatalf("generation frame = %#v", frame)
	}
	invoker := NewVMCallbackInvoker(vm)
	fn := &loadFileFn{namespaces: runner.runtimeNamespaces()}
	value, err := invoker.Invoke(context.Background(), fn, []types.Value{atom.NewString("value.dog")}, nil)
	if err != nil {
		t.Fatalf("callback invoker failed: %v", err)
	}
	if got := value.String(); got != ":verified" {
		t.Fatalf("callback relative load = %s, want retained :verified content", got)
	}
}

func TestRequireEmbeddedStdlibUsesStdlibOrigin(t *testing.T) {
	exec := &capturingExecutor{}
	loader := NewLoader("", nil, nil, exec)

	if err := loader.Require("doggo.test"); err != nil {
		t.Fatalf("require failed: %v", err)
	}
	if exec.lastOrigin.File == "" {
		t.Fatal("expected captured origin file")
	}
	if !strings.HasPrefix(exec.lastOrigin.File, "stdlib://") {
		t.Fatalf("expected stdlib origin, got %q", exec.lastOrigin.File)
	}
}

func TestRequireProjectStdlibUsesDiskOrigin(t *testing.T) {
	tmpDir := t.TempDir()
	stdlibPath := filepath.Join(tmpDir, "stdlib", "doggo", "sample.dog")
	if err := os.MkdirAll(filepath.Dir(stdlibPath), 0755); err != nil {
		t.Fatalf("mkdir stdlib dir: %v", err)
	}
	if err := os.WriteFile(stdlibPath, []byte("(ns doggo.sample)\n(def loaded-from :disk)\n"), 0644); err != nil {
		t.Fatalf("write stdlib file: %v", err)
	}

	exec := &capturingExecutor{}
	loader := NewLoader(tmpDir, []string{}, nil, exec)

	if err := loader.Require("doggo.sample"); err != nil {
		t.Fatalf("require failed: %v", err)
	}
	if got, want := exec.lastOrigin.File, "stdlib/doggo/sample.dog"; got != want {
		t.Fatalf("unexpected origin: got=%q want=%q", got, want)
	}
}

func TestRequireReservedDoggoNamespaceIgnoresDependencyStdlibOverride(t *testing.T) {
	projectRoot := t.TempDir()
	depRoot := t.TempDir()
	overridePath := filepath.Join(depRoot, "stdlib", "doggo", "test.dog")
	if err := os.MkdirAll(filepath.Dir(overridePath), 0755); err != nil {
		t.Fatalf("mkdir override dir: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte("(ns doggo.test)\n(def compromised true)\n"), 0644); err != nil {
		t.Fatalf("write override stdlib file: %v", err)
	}

	exec := &capturingExecutor{}
	loader := NewLoader(projectRoot, []string{depRoot}, nil, exec)

	projectStdlib := filepath.Join(projectRoot, "stdlib", "doggo", "test.dog")
	if err := os.MkdirAll(filepath.Dir(projectStdlib), 0755); err != nil {
		t.Fatalf("mkdir project stdlib dir: %v", err)
	}
	if err := os.WriteFile(projectStdlib, []byte("(ns doggo.test)\n(def source :project)\n"), 0644); err != nil {
		t.Fatalf("write project stdlib file: %v", err)
	}

	if err := loader.Require("doggo.test"); err != nil {
		t.Fatalf("require failed: %v", err)
	}
	if got, want := exec.lastOrigin.File, "stdlib/doggo/test.dog"; got != want {
		t.Fatalf("expected project stdlib origin %q, got %q", want, got)
	}
}

func TestRequireReservedDoggoNamespaceLoadPathDoesNotChangeProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	depRoot := t.TempDir()
	projectStdlib := filepath.Join(projectRoot, "stdlib", "doggo", "sample.dog")
	depStdlib := filepath.Join(depRoot, "stdlib", "doggo", "sample.dog")
	for _, path := range []string{projectStdlib, depStdlib} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir stdlib dir: %v", err)
		}
	}
	if err := os.WriteFile(projectStdlib, []byte("(ns doggo.sample)\n(def source :project)\n"), 0644); err != nil {
		t.Fatalf("write project stdlib file: %v", err)
	}
	if err := os.WriteFile(depStdlib, []byte("(ns doggo.sample)\n(def source :dependency)\n"), 0644); err != nil {
		t.Fatalf("write dependency stdlib file: %v", err)
	}

	exec := &capturingExecutor{}
	loader := NewLoader(projectRoot, []string{depRoot}, nil, exec)

	if err := loader.Require("doggo.sample"); err != nil {
		t.Fatalf("require failed: %v", err)
	}
	if got, want := exec.lastOrigin.File, "stdlib/doggo/sample.dog"; got != want {
		t.Fatalf("expected project stdlib origin %q, got %q", want, got)
	}
}

func TestNewRunnerWithProjectConfigLoadsProjectStdlibCore(t *testing.T) {
	projectRoot := t.TempDir()
	corePath := filepath.Join(projectRoot, "stdlib", "doggo", "core.dog")
	if err := os.MkdirAll(filepath.Dir(corePath), 0755); err != nil {
		t.Fatalf("mkdir core dir: %v", err)
	}
	if err := os.WriteFile(corePath, []byte("(ns doggo.core)\n(def project-core-marker :project)\n"), 0644); err != nil {
		t.Fatalf("write project core: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: true})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	value, err := runner.Run("project-core-marker")
	if err != nil {
		t.Fatalf("run project core marker: %v", err)
	}

	if got := value.String(); got != ":project" {
		t.Fatalf("unexpected project core marker: %s", got)
	}
}

func TestNewRunnerWithProjectConfigUsesProjectRootAsDefaultLoadPath(t *testing.T) {
	projectRoot := t.TempDir()
	libPath := filepath.Join(projectRoot, "app", "util.dog")
	if err := os.MkdirAll(filepath.Dir(libPath), 0755); err != nil {
		t.Fatalf("mkdir lib dir: %v", err)
	}
	if err := os.WriteFile(libPath, []byte("(ns app.util)\n(def answer 42)\n"), 0644); err != nil {
		t.Fatalf("write lib file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: true})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	if _, err := runner.Run("(require 'app.util)"); err != nil {
		t.Fatalf("require app.util: %v", err)
	}

	value, err := runner.Run("app.util/answer")
	if err != nil {
		t.Fatalf("resolve app.util/answer: %v", err)
	}
	if got := value.String(); got != "42" {
		t.Fatalf("unexpected value: %s", got)
	}
}

func TestNewRunnerWithProjectConfigReturnsCoreLoadError(t *testing.T) {
	projectRoot := t.TempDir()
	corePath := filepath.Join(projectRoot, "stdlib", "doggo", "core.dog")
	if err := os.MkdirAll(filepath.Dir(corePath), 0755); err != nil {
		t.Fatalf("mkdir core dir: %v", err)
	}
	if err := os.WriteFile(corePath, []byte("(ns doggo.core)\n(def broken\n"), 0644); err != nil {
		t.Fatalf("write broken core: %v", err)
	}

	_, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: true})
	if err == nil {
		t.Fatal("expected core load error, got nil")
	}
	if !strings.Contains(err.Error(), "stdlib/doggo/core.dog") {
		t.Fatalf("expected error to mention project core path, got: %v", err)
	}
}

func TestNewRunnerWithProjectConfigIgnoresDOGGOPath(t *testing.T) {
	projectRoot := t.TempDir()
	envPath := t.TempDir()

	t.Setenv("DOGGO_PATH", envPath)

	envLib := filepath.Join(envPath, "ambient", "lib.dog")
	if err := os.MkdirAll(filepath.Dir(envLib), 0755); err != nil {
		t.Fatalf("mkdir ambient dir: %v", err)
	}
	if err := os.WriteFile(envLib, []byte("(ns ambient.lib)\n(def leaked true)\n"), 0644); err != nil {
		t.Fatalf("write ambient lib: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: true})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	if _, err := runner.Run("(require 'ambient.lib)"); err == nil {
		t.Fatal("expected ambient DOGGO_PATH namespace to be ignored")
	}
}

func TestNewRunnerWithConfigResolvesRelativeLoadPathsFromProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	libPath := filepath.Join(projectRoot, "vendor", "lib", "math.dog")
	if err := os.MkdirAll(filepath.Dir(libPath), 0755); err != nil {
		t.Fatalf("mkdir lib dir: %v", err)
	}
	if err := os.WriteFile(libPath, []byte("(ns lib.math)\n(def value 7)\n"), 0644); err != nil {
		t.Fatalf("write lib file: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	outsideDir := t.TempDir()
	if err := os.Chdir(outsideDir); err != nil {
		t.Fatalf("chdir outside project: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{"vendor"},
		BootstrapCore: true,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	if _, err := runner.Run("(require 'lib.math)"); err != nil {
		t.Fatalf("require lib.math: %v", err)
	}

	value, err := runner.Run("lib.math/value")
	if err != nil {
		t.Fatalf("resolve lib.math/value: %v", err)
	}
	if got := value.String(); got != "7" {
		t.Fatalf("unexpected value: %s", got)
	}
}

func TestGenerationSourceRequireRejectsReplacedDependencyFile(t *testing.T) {
	_, session, dependencyRoot := openVMGenerationSession(t)
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:       session.Config.ProjectRoot,
		LoadPaths:         session.Config.LoadPaths,
		GenerationSources: session.Config.GenerationSources,
		Lock:              session.Config.Lock,
		BootstrapCore:     true,
	})
	if err != nil {
		t.Fatal(err)
	}

	original := filepath.Join(dependencyRoot, "dep", "core.dog")
	if err := os.Rename(original, original+".verified"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("(ns dep.core)\n(def replacement-executed true)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = runner.Run("(require 'dep.core)")
	if err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("require after replacing %s = %v, want integrity error", original, err)
	}
	if _, err := runner.Run("dep.core/replacement-executed"); err == nil || !strings.Contains(err.Error(), "undefined variable") {
		t.Fatalf("replacement definition lookup = %v, want undefined variable", err)
	}
}

func TestGenerationSourceDelayedRelativeLoadFileRetainsVerifiedOrigin(t *testing.T) {
	_, session, dependencyRoot := openVMGenerationSession(t)
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:       session.Config.ProjectRoot,
		LoadPaths:         session.Config.LoadPaths,
		GenerationSources: session.Config.GenerationSources,
		Lock:              session.Config.Lock,
		BootstrapCore:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run("(require 'dep.core)"); err != nil {
		t.Fatalf("require dependency: %v", err)
	}

	backup := dependencyRoot + ".verified"
	if err := os.Rename(dependencyRoot, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dependencyRoot, "dep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dependencyRoot, "dep", "value.dog"), []byte(":replacement\n"), 0644); err != nil {
		t.Fatal(err)
	}

	value, err := runner.Run("(dep.core/read-relative)")
	if err != nil {
		if !strings.Contains(err.Error(), "integrity") {
			t.Fatalf("delayed relative load = %v, want verified content or integrity error", err)
		}
		return
	}
	if got := value.String(); got != ":verified" {
		t.Fatalf("delayed relative load = %s, want :verified", got)
	}
}

func TestGenerationSourceOriginUsesCanonicalRelativePath(t *testing.T) {
	_, session, _ := openVMGenerationSession(t)
	loader := NewLoader(session.Config.ProjectRoot, nil, session.Config.GenerationSources, &capturingExecutor{})
	resolved, err := loader.resolveSource("dep//core.dog")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.Origin.RelativePath, "dep/core.dog"; got != want {
		t.Fatalf("relative origin = %q, want %q", got, want)
	}
	if got, want := resolved.Origin.File, session.Config.GenerationSources[0].DisplayPath("dep/core.dog"); got != want {
		t.Fatalf("display origin = %q, want %q", got, want)
	}
}

func TestRelativeLoadFileRejectsUnresolvedGenerationOrigin(t *testing.T) {
	projectRoot := t.TempDir()
	executor := &capturingExecutor{}
	loader := NewLoader(projectRoot, nil, nil, executor)
	origin := engine.SourceOrigin{File: "generation://missing/dep/core.dog"}

	_, err := loader.LoadFileFromContext(withSourceOrigin(context.Background(), origin), "value.dog")
	const want = `load-file: unresolved generation origin "generation://missing/dep/core.dog"`
	if err == nil || err.Error() != want {
		t.Fatalf("relative load from unresolved generation origin = %v, want %q", err, want)
	}
	if executor.lastSource != "" {
		t.Fatalf("executor received source %q", executor.lastSource)
	}
}

func openVMGenerationSession(t *testing.T) (string, *project.GenerationSession, string) {
	t.Helper()
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
		t.Fatal(err)
	}
	dependency := filepath.Join(root, "dep")
	for name, content := range map[string]string{
		"doggo.mod":     "{:module \"example/dep\" :deps []}\n",
		"dep/core.dog":  "(ns dep.core)\n(defn read-relative [] (load-file \"value.dog\"))\n",
		"dep/value.dog": ":verified\n",
	} {
		filename := filepath.Join(dependency, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	session, err := project.OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close generation session: %v", err)
		}
	})
	if len(session.Config.GenerationSources) != 1 || len(session.Config.Lock.Deps) != 1 {
		t.Fatalf("generation sources = %d, lock deps = %d", len(session.Config.GenerationSources), len(session.Config.Lock.Deps))
	}
	dependencyRoot := filepath.Join(root, ".doggo", "generations", session.Config.Lock.Generation, filepath.FromSlash(session.Config.Lock.Deps[0].InstallDir))
	return root, session, dependencyRoot
}

type capturingExecutor struct {
	lastSource string
	lastOrigin engine.SourceOrigin
}

func (e *capturingExecutor) RunWithOrigin(source string, origin engine.SourceOrigin) (types.Value, error) {
	e.lastSource = source
	e.lastOrigin = origin
	return nil, nil
}

func (e *capturingExecutor) CurrentNamespace() string {
	return "user"
}

func (e *capturingExecutor) SetCurrentNamespace(_ string) {}

func TestRuntimeErrorStackTraceHasFileLineColumn(t *testing.T) {
	runner := MustNewRunnerWithoutCore()
	_, err := runner.RunWithOrigin(`
(def boom (fn [] (/ 1 0)))
(boom)
`, engine.SourceOrigin{File: "app/trace.dog"})
	if err == nil {
		t.Fatal("expected runtime error, got nil")
	}

	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if len(rtErr.StackTrace) == 0 {
		t.Fatal("expected stack trace frames")
	}
	for i, frame := range rtErr.StackTrace {
		if frame.File == "" {
			t.Fatalf("frame %d has empty file", i)
		}
		if frame.Line <= 0 {
			t.Fatalf("frame %d has invalid line %d", i, frame.Line)
		}
		if frame.Column <= 0 {
			t.Fatalf("frame %d has invalid column %d", i, frame.Column)
		}
	}
}

var _ interface {
	RunWithOrigin(source string, origin engine.SourceOrigin) (types.Value, error)
	CurrentNamespace() string
	SetCurrentNamespace(ns string)
} = (*capturingExecutor)(nil)
