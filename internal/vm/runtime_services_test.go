package vm

import (
	"path/filepath"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/testutil"
	"doggo/internal/types"
)

func TestNewRunnerServicesCreatesLoaderFromConfig(t *testing.T) {
	projectRoot := testutil.CanonicalTempDir(t)

	session := newExecutionSession()
	session.globals = make([]types.Value, GlobalsSize)
	session.observability = NewObservabilityState()
	runner := &Runner{
		session:     session,
		symbolTable: compiler.NewSymbolTable(),
	}
	session.runner = runner
	services := newRunnerServices(runner, RunnerConfig{
		ProjectRoot: projectRoot,
		LoadPaths:   []string{"lib", filepath.Join(projectRoot, "vendor")},
	})

	loader, ok := services.namespaces.Loader().(*runnerNamespaceLoader)
	if !ok {
		t.Fatal("expected runner namespace loader")
	}
	loadPaths := loader.GetLoadPaths()
	if len(loadPaths) != 2 {
		t.Fatalf("len(loadPaths) = %d, want 2", len(loadPaths))
	}
	if loadPaths[0] != filepath.Join(projectRoot, "lib") {
		t.Fatalf("loadPaths[0] = %q", loadPaths[0])
	}
	if loadPaths[1] != filepath.Join(projectRoot, "vendor") {
		t.Fatalf("loadPaths[1] = %q", loadPaths[1])
	}
	if loader.ProjectRoot() != resolveProjectRootBestEffort(projectRoot) {
		t.Fatalf("loader.projectRoot = %q, want %q", loader.ProjectRoot(), resolveProjectRootBestEffort(projectRoot))
	}
}

func TestRunnerCoreBootstrapperLoadIsIdempotent(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: testExactDefaultRuntimeProfile(),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	if err := runner.runtimeServices.core.BootstrapOnce(); err != nil {
		t.Fatalf("first core load: %v", err)
	}
	if err := runner.runtimeServices.core.BootstrapOnce(); err != nil {
		t.Fatalf("second core load: %v", err)
	}

	value, err := runner.Run(`(take 2 (repeat 9))`)
	if err != nil {
		t.Fatalf("repeat after core bootstrap: %v", err)
	}
	if value.String() != "(9 9)" {
		t.Fatalf("unexpected repeat result: %s", value.String())
	}
}
