package vm

import (
	"fmt"
	"sort"

	"doggo/internal/compiler"
	"doggo/internal/types"
)

type runtimeBindingGroup string

const (
	runtimeBindingGroupMacroExpand       runtimeBindingGroup = "macro-expand"
	runtimeBindingGroupFileLoad          runtimeBindingGroup = "file-load"
	runtimeBindingGroupREPLIntrospection runtimeBindingGroup = "repl-introspection"
	runtimeBindingGroupLazySeq           runtimeBindingGroup = "lazy-seq"
	runtimeBindingGroupObservability     runtimeBindingGroup = "observability"
	runtimeBindingGroupBasis             runtimeBindingGroup = "basis"
	runtimeBindingGroupCLI               runtimeBindingGroup = "cli"
)

type runtimeBootstrapPhase string

const (
	runtimeBootstrapPhaseCoreContract runtimeBootstrapPhase = "core-contract"
)

type runtimeBindingInstallMode string

const (
	runtimeBindingInstallReplace  runtimeBindingInstallMode = "replace"
	runtimeBindingInstallIfAbsent runtimeBindingInstallMode = "if-absent"
)

type runtimeBindingSpec struct {
	group           runtimeBindingGroup
	namespace       string
	name            string
	private         bool
	bootstrapPhases []runtimeBootstrapPhase
	build           func(*Runner) types.Value
}

type runnerRuntimeBindingCatalog struct {
	specs []runtimeBindingSpec
}

func (catalog runnerRuntimeBindingCatalog) clone() runnerRuntimeBindingCatalog {
	if len(catalog.specs) == 0 {
		return runnerRuntimeBindingCatalog{}
	}
	cloned := make([]runtimeBindingSpec, len(catalog.specs))
	copy(cloned, catalog.specs)
	return runnerRuntimeBindingCatalog{specs: cloned}
}

func defaultRunnerRuntimeBindingCatalog() runnerRuntimeBindingCatalog {
	specs := []runtimeBindingSpec{
		{
			group:     runtimeBindingGroupMacroExpand,
			namespace: compiler.CoreNamespace,
			name:      "macroexpand-1",
			build: func(r *Runner) types.Value {
				return &macroExpandFn{expander: r.macroExpander, expandAll: false}
			},
		},
		{
			group:     runtimeBindingGroupMacroExpand,
			namespace: compiler.CoreNamespace,
			name:      "macroexpand",
			build: func(r *Runner) types.Value {
				return &macroExpandFn{expander: r.macroExpander, expandAll: true}
			},
		},
		{
			group:           runtimeBindingGroupFileLoad,
			namespace:       compiler.CoreNamespace,
			name:            "load-file",
			bootstrapPhases: []runtimeBootstrapPhase{runtimeBootstrapPhaseCoreContract},
			build: func(r *Runner) types.Value {
				return &loadFileFn{namespaces: r.runtimeNamespaces()}
			},
		},
		{
			group:           runtimeBindingGroupFileLoad,
			namespace:       compiler.CoreNamespace,
			name:            "require",
			bootstrapPhases: []runtimeBootstrapPhase{runtimeBootstrapPhaseCoreContract},
			build: func(r *Runner) types.Value {
				return &requireFn{namespaces: r.runtimeNamespaces()}
			},
		},
		{
			group:           runtimeBindingGroupFileLoad,
			namespace:       compiler.CoreNamespace,
			name:            "resource-path*",
			bootstrapPhases: []runtimeBootstrapPhase{runtimeBootstrapPhaseCoreContract},
			build: func(r *Runner) types.Value {
				return &resourcePathFn{namespaces: r.runtimeNamespaces()}
			},
		},
		{
			group:     runtimeBindingGroupREPLIntrospection,
			namespace: compiler.CoreNamespace,
			name:      "doc*",
			build: func(r *Runner) types.Value {
				return &docIntrospectionFn{runner: r}
			},
		},
		{
			group:     runtimeBindingGroupREPLIntrospection,
			namespace: compiler.CoreNamespace,
			name:      "source-str*",
			build: func(r *Runner) types.Value {
				return &sourceIntrospectionFn{runner: r}
			},
		},
		{
			group:     runtimeBindingGroupREPLIntrospection,
			namespace: compiler.CoreNamespace,
			name:      "dir*",
			build: func(r *Runner) types.Value {
				return &dirIntrospectionFn{runner: r}
			},
		},
		{
			group:     runtimeBindingGroupREPLIntrospection,
			namespace: compiler.CoreNamespace,
			name:      "apropos*",
			build: func(r *Runner) types.Value {
				return &aproposIntrospectionFn{runner: r}
			},
		},
		{
			group:     runtimeBindingGroupREPLIntrospection,
			namespace: compiler.CoreNamespace,
			name:      "find-doc*",
			build: func(r *Runner) types.Value {
				return &findDocIntrospectionFn{runner: r}
			},
		},
		{
			group:     runtimeBindingGroupREPLIntrospection,
			namespace: compiler.CoreNamespace,
			name:      "last-exception*",
			build: func(r *Runner) types.Value {
				return &lastExceptionIntrospectionFn{runner: r}
			},
		},
		{
			group:           runtimeBindingGroupLazySeq,
			namespace:       compiler.CoreNamespace,
			name:            "new-lazy-seq",
			bootstrapPhases: []runtimeBootstrapPhase{runtimeBootstrapPhaseCoreContract},
			build: func(r *Runner) types.Value {
				return &newLazySeqFn{runtime: r.resolveLazySeqRuntime()}
			},
		},
		{
			group:     runtimeBindingGroupBasis,
			namespace: "doggo.basis",
			name:      "basis-current*",
			private:   true,
			build: func(r *Runner) types.Value {
				return &basisSnapshotFn{runner: r, current: true}
			},
		},
		{
			group:     runtimeBindingGroupBasis,
			namespace: "doggo.basis",
			name:      "basis-initial*",
			private:   true,
			build: func(r *Runner) types.Value {
				return &basisSnapshotFn{runner: r, current: false}
			},
		},
		{
			group:     runtimeBindingGroupCLI,
			namespace: "doggo.cli",
			name:      "command-line-args*",
			private:   true,
			build: func(r *Runner) types.Value {
				return &commandLineArgsFn{runner: r}
			},
		},
		{
			group:     runtimeBindingGroupCLI,
			namespace: "doggo.cli",
			name:      "entrypoint?*",
			private:   true,
			build: func(r *Runner) types.Value {
				return &entryPointFn{runner: r}
			},
		},
	}

	for _, def := range observabilityBuiltinDefs {
		def := def
		def.ErrorFunction = "doggo.observability/" + def.Name
		specs = append(specs, runtimeBindingSpec{
			group:     runtimeBindingGroupObservability,
			namespace: "doggo.observability",
			name:      def.Name,
			build: func(*Runner) types.Value {
				builtin := cloneBuiltin(def.Builtin, def.Name)
				builtin.ErrorFunction = def.ErrorFunction
				return builtin
			},
		})
	}

	return runnerRuntimeBindingCatalog{specs: specs}
}

func (catalog runnerRuntimeBindingCatalog) installGroup(r *Runner, group runtimeBindingGroup) error {
	specs := make([]runtimeBindingSpec, 0)
	for _, spec := range catalog.specs {
		if spec.group == group {
			specs = append(specs, spec)
		}
	}
	if len(specs) == 0 {
		return fmt.Errorf("runtime binding group %q: no bindings declared", group)
	}
	return catalog.installSpecs(r, specs, runtimeBindingInstallReplace)
}

func (catalog runnerRuntimeBindingCatalog) installBootstrapPhase(r *Runner, phase runtimeBootstrapPhase) error {
	specs := make([]runtimeBindingSpec, 0)
	for _, spec := range catalog.specs {
		if spec.hasBootstrapPhase(phase) {
			specs = append(specs, spec)
		}
	}
	if len(specs) == 0 {
		return fmt.Errorf("runtime bootstrap phase %q: no bindings declared", phase)
	}
	return catalog.installSpecs(r, specs, runtimeBindingInstallIfAbsent)
}

func (catalog runnerRuntimeBindingCatalog) installSpecs(r *Runner, specs []runtimeBindingSpec, mode runtimeBindingInstallMode) error {
	for _, spec := range specs {
		if err := r.installRuntimeBinding(spec, mode); err != nil {
			return err
		}
	}
	return nil
}

func (catalog runnerRuntimeBindingCatalog) bindingNamesForBootstrapPhase(phase runtimeBootstrapPhase) []string {
	names := make([]string, 0)
	for _, spec := range catalog.specs {
		if spec.hasBootstrapPhase(phase) {
			names = append(names, spec.name)
		}
	}
	sort.Strings(names)
	return names
}

func (spec runtimeBindingSpec) hasBootstrapPhase(phase runtimeBootstrapPhase) bool {
	for _, candidate := range spec.bootstrapPhases {
		if candidate == phase {
			return true
		}
	}
	return false
}

func (r *Runner) installRuntimeBinding(spec runtimeBindingSpec, mode runtimeBindingInstallMode) error {
	if mode == runtimeBindingInstallIfAbsent && r.hasRuntimeBinding(spec.namespace, spec.name) {
		return nil
	}
	if spec.build == nil {
		return fmt.Errorf("runtime binding %s/%s: missing builder", spec.namespace, spec.name)
	}
	value := spec.build(r)
	if value == nil {
		return fmt.Errorf("runtime binding %s/%s: builder returned nil", spec.namespace, spec.name)
	}
	sym := r.symbolTable.DefineGlobalInNamespace(spec.namespace, spec.name)
	r.symbolTable.SetGlobalPrivateInNamespace(spec.namespace, spec.name, spec.private)
	r.getOrCreateExecutionSession().globals[sym.Index] = value
	return nil
}

func (r *Runner) hasRuntimeBinding(namespace, name string) bool {
	qualified := compiler.QualifySymbol(namespace, name)
	sym, ok := r.symbolTable.Resolve(qualified)
	if !ok {
		return false
	}
	if sym.Scope != compiler.GlobalScope && sym.Scope != compiler.ProtectedGlobalScope {
		return false
	}
	session := r.getOrCreateExecutionSession()
	if sym.Index < 0 || sym.Index >= len(session.globals) {
		return false
	}
	return session.globals[sym.Index] != nil
}

func (r *Runner) installRuntimeBindingGroup(group runtimeBindingGroup) error {
	if r == nil || r.runtimeServices == nil {
		return fmt.Errorf("runtime binding group %q: runner runtime services are not available", group)
	}
	return r.runtimeServices.bindings.installGroup(r, group)
}

func (r *Runner) installRuntimeBootstrapPhase(phase runtimeBootstrapPhase) error {
	if r == nil || r.runtimeServices == nil {
		return fmt.Errorf("runtime bootstrap phase %q: runner runtime services are not available", phase)
	}
	return r.runtimeServices.bindings.installBootstrapPhase(r, phase)
}
