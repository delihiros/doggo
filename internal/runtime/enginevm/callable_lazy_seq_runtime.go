package enginevm

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/vm"
	"doggo/internal/vm/engine"
)

type callableLazySeqRuntime struct {
	runner    *vm.Runner
	bindingVM *vm.VM
}

func newCallableLazySeqRuntime(runner *vm.Runner) *callableLazySeqRuntime {
	return &callableLazySeqRuntime{runner: runner}
}

func (s callableLazySeqRuntime) Bind(vmref *vm.VM) vm.CallableRuntime {
	s.bindingVM = vmref
	return s
}

func (s callableLazySeqRuntime) SnapshotCallableRuntime() vm.CallableRuntime {
	if s.runner == nil {
		return exactCallableRuntimeAdapter{}
	}
	return newExactCallableRuntimeAdapter(s.runner)
}

func (s callableLazySeqRuntime) Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("callable runtime is not initialized")
	}
	return s.runner.SnapshotExactCallableRuntime().Apply(ctx, s.bindingVM, fn, args)
}

func (s callableLazySeqRuntime) ApplyFast(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("callable runtime is not initialized")
	}
	return s.runner.SnapshotExactCallableRuntime().ApplyFast(ctx, s.bindingVM, fn, args)
}

func (s callableLazySeqRuntime) InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("callable runtime is not initialized")
	}
	return s.runner.SnapshotExactCallableRuntime().InvokeClosure(ctx, cl, args)
}

func (s callableLazySeqRuntime) NewLazySeq(ctx context.Context, thunk types.Value) *vm.VMLazySeq {
	if s.runner == nil {
		return nil
	}
	return vm.NewVMLazySeq(thunk, func(thunkValue types.Value) (types.Value, error) {
		if fn, ok := thunkValue.(types.Fn); ok {
			return fn.Invoke(ctx)
		}

		closure, ok := thunkValue.(*compiler.Closure)
		if !ok {
			return nil, &vm.RuntimeError{Message: "lazy-seq: thunk must be a function"}
		}
		if closure.Fn.NumParameters != 0 {
			return nil, &vm.RuntimeError{Message: "lazy-seq: thunk must take no arguments"}
		}
		return s.InvokeClosure(ctx, closure, nil)
	})
}

func newRuntimeExecutionFromExactExecution(exec ExactExecution) *RuntimeExecution {
	runtimeExec := &RuntimeExecution{
		runner:         exec.Runner,
		initialization: exec.Initialization,
		namespaces:     exec.Namespaces,
		sources:        exec.Sources,
		basis:          exec.Basis,
	}
	if runtimeExec.runner != nil {
		runtimeExec.services = newCallableLazySeqRuntime(runtimeExec.runner)
		runtimeExec.runner.SetCallableRuntime(runtimeExec.services)
		runtimeExec.runner.SetLazySeqRuntime(runtimeExec.services)
	}
	return runtimeExec
}

type exactNamespaceLoader struct {
	runner      *vm.Runner
	projectRoot string
	mirror      *vm.Loader
}

type exactNamespaceState struct {
	runner *vm.Runner
}

func newExactGraphHandles(runner *vm.Runner, graph GraphHandles, sources *engine.SourceRegistry) GraphHandles {
	projectRoot := graph.Engine.ProjectRoot
	loadPaths := append([]string(nil), graph.Engine.LoadPaths...)
	if graph.Namespaces != nil && graph.Namespaces.Loader() != nil {
		projectRoot = graph.Namespaces.Loader().ProjectRoot()
		loadPaths = graph.Namespaces.Loader().GetLoadPaths()
	}

	loader := &exactNamespaceLoader{
		runner:      runner,
		projectRoot: resolveProjectRootBestEffort(projectRoot),
		mirror:      vm.NewLoader(projectRoot, loadPaths, graph.Engine.GenerationSources, runner),
	}
	namespaces := engine.NewNamespaceServices(engine.NamespaceConfig{
		ProjectRoot: loader.projectRoot,
		LoadPaths:   loader.GetLoadPaths(),
		Runtime:     graph.Engine,
		Loader:      loader,
		State:       exactNamespaceState{runner: runner},
	})

	if sources == nil {
		sources = engine.NewSourceRegistry(namespaces.Loader())
	}

	return GraphHandles{
		Engine:     graph.Engine,
		Namespaces: namespaces,
		Sources:    sources,
		Basis:      engine.NewBasisServices(namespaces.Loader(), graph.Engine.Lock),
	}
}

func (l *exactNamespaceLoader) LoadFile(path string) (types.Value, error) {
	if l == nil || l.runner == nil {
		return nil, nil
	}
	return l.runner.LoadFile(path)
}

func (l *exactNamespaceLoader) Require(ns string) error {
	if l == nil || l.runner == nil {
		return nil
	}
	return l.runner.Require(ns)
}

func (l *exactNamespaceLoader) ResolveResourcePath(name string) (string, bool) {
	if l == nil || l.mirror == nil {
		return "", false
	}
	return l.mirror.ResolveResourcePath(name)
}

func (l *exactNamespaceLoader) GetLoadPaths() []string {
	if l == nil || l.mirror == nil {
		return nil
	}
	return append([]string(nil), l.mirror.GetLoadPaths()...)
}

func (l *exactNamespaceLoader) ProjectRoot() string {
	if l == nil {
		return ""
	}
	return l.projectRoot
}

func (s exactNamespaceState) CurrentNamespace() string {
	if s.runner == nil {
		return ""
	}
	return s.runner.CurrentNamespace()
}

func (s exactNamespaceState) SetCurrentNamespace(ns string) {
	if s.runner == nil {
		return
	}
	s.runner.SetCurrentNamespace(ns)
}

func resolveProjectRootBestEffort(projectRoot string) string {
	trimmed := strings.TrimSpace(projectRoot)
	if trimmed == "" {
		return ""
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return trimmed
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved
	}
	return abs
}
