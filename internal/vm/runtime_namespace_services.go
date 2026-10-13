package vm

import (
	"context"
	"fmt"

	"doggo/internal/compiler"
	"doggo/internal/project"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/vm/engine"
)

type runnerNamespaceServices struct {
	engine        *engine.NamespaceServices
	symbolTable   *compiler.SymbolTable
	macroExpander *MacroExpander
}

func newRunnerNamespaceServices(runner *Runner, cfg RunnerConfig) *runnerNamespaceServices {
	loader := NewLoader(cfg.ProjectRoot, cfg.LoadPaths, cfg.GenerationSources, runner)
	return &runnerNamespaceServices{
		engine: engine.NewNamespaceServices(engine.NamespaceConfig{
			ProjectRoot:       cfg.ProjectRoot,
			LoadPaths:         append([]string{}, cfg.LoadPaths...),
			GenerationSources: append([]*project.GenerationSource(nil), cfg.GenerationSources...),
			Runtime: engine.RuntimeConfig{
				ProjectRoot:       cfg.ProjectRoot,
				LoadPaths:         append([]string{}, cfg.LoadPaths...),
				GenerationSources: append([]*project.GenerationSource(nil), cfg.GenerationSources...),
				Lock:              cfg.Lock,
				BootstrapCore:     cfg.BootstrapCore,
			},
			Loader: &runnerNamespaceLoader{loader: loader},
			State:  &runnerNamespaceState{runner: runner},
		}),
		symbolTable:   runner.symbolTable,
		macroExpander: runner.macroExpander,
	}
}

func (services *runnerNamespaceServices) LoadFile(path string) (types.Value, error) {
	return services.engine.LoadFile(path)
}

func (services *runnerNamespaceServices) LoadFileFromContext(ctx context.Context, path string) (types.Value, error) {
	if services == nil {
		return nil, nil
	}
	loader, _ := services.engine.Loader().(*runnerNamespaceLoader)
	if loader == nil || loader.loader == nil {
		return nil, nil
	}
	return loader.loader.LoadFileFromContext(ctx, path)
}

func (services *runnerNamespaceServices) Require(nsName string) error {
	return services.engine.Require(nsName)
}

func (services *runnerNamespaceServices) RequireFromContext(ctx context.Context, nsName string) error {
	loader, _ := services.engine.Loader().(*runnerNamespaceLoader)
	if loader == nil || loader.loader == nil {
		return nil
	}
	return loader.loader.RequireFromContext(ctx, nsName)
}

func (services *runnerNamespaceServices) ResolveResourcePath(name string) (string, bool) {
	return services.engine.ResolveResourcePath(name)
}

func (services *runnerNamespaceServices) CurrentNamespace() string {
	if services == nil {
		return ""
	}
	return services.engine.CurrentNamespace()
}

func (services *runnerNamespaceServices) SetCurrentNamespace(ns string) {
	if services == nil {
		return
	}
	services.engine.SetCurrentNamespace(ns)
}

func (services *runnerNamespaceServices) Loader() engine.NamespaceLoader {
	if services == nil || services.engine == nil {
		return nil
	}
	return services.engine.Loader()
}

func (services *runnerNamespaceServices) RequireValue(arg types.Value) error {
	return services.RequireValueFromContext(context.Background(), arg)
}

func (services *runnerNamespaceServices) RequireValueFromContext(ctx context.Context, arg types.Value) error {
	loader, _ := services.engine.Loader().(*runnerNamespaceLoader)
	if loader == nil || loader.loader == nil {
		return nil
	}

	switch v := arg.(type) {
	case types.Symbol:
		return services.RequireFromContext(ctx, string(v))
	case atom.Keyword:
		return services.RequireFromContext(ctx, string(v))
	case *coll.Vector:
		return services.requireVector(ctx, v)
	default:
		return fmt.Errorf("require: argument must be a symbol, keyword, or vector, got %s", arg.Type())
	}
}

func (services *runnerNamespaceServices) requireVector(ctx context.Context, vec *coll.Vector) error {
	if len(vec.Items) == 0 {
		return fmt.Errorf("require: empty vector")
	}

	spec, err := parseRequireSpec(vec)
	if err != nil {
		return fmt.Errorf("require: %w", err)
	}

	if err := services.RequireFromContext(ctx, spec.namespace); err != nil {
		return err
	}

	loader, _ := services.engine.Loader().(*runnerNamespaceLoader)
	ctx, leave := loader.loader.enter(ctx)
	defer leave()
	if namespace := namespaceFromContext(ctx); namespace != "" {
		services.symbolTable.SetCurrentNamespace(namespace)
	}

	if spec.alias != "" {
		services.symbolTable.AddAlias(spec.alias, spec.namespace)
	}

	if spec.referAll {
		for _, sym := range services.symbolTable.GetPublicSymbolsInNamespace(spec.namespace) {
			localName := sym
			if newName, ok := spec.rename[sym]; ok {
				localName = newName
			}
			if err := services.symbolTable.AddRefer(spec.namespace, sym, localName); err != nil {
				return err
			}
		}
		if services.macroExpander != nil {
			for _, macroName := range services.macroExpander.GetPublicMacrosInNamespace(spec.namespace) {
				localName := macroName
				if newName, ok := spec.rename[macroName]; ok {
					localName = newName
				}
				if err := services.symbolTable.AddMacroRefer(spec.namespace, macroName, localName); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, sym := range spec.refer {
		localName := sym
		if newName, ok := spec.rename[sym]; ok {
			localName = newName
		}

		err := services.symbolTable.AddRefer(spec.namespace, sym, localName)
		if err != nil {
			if services.macroExpander == nil {
				return err
			}
			if services.macroExpander.HasPublicMacro(spec.namespace, sym) {
				if err := services.symbolTable.AddMacroRefer(spec.namespace, sym, localName); err != nil {
					return err
				}
			} else if services.macroExpander.HasMacro(spec.namespace, sym) {
				return fmt.Errorf("require: macro '%s/%s' is not public", spec.namespace, sym)
			} else {
				return err
			}
		}

		if services.macroExpander != nil && services.macroExpander.HasPublicMacro(spec.namespace, sym) {
			if err := services.symbolTable.AddMacroRefer(spec.namespace, sym, localName); err != nil {
				return err
			}
		}
	}

	return nil
}

type requireSpec struct {
	namespace string
	alias     string
	refer     []string
	referAll  bool
	rename    map[string]string
}

func parseRequireSpec(vec *coll.Vector) (*requireSpec, error) {
	if len(vec.Items) == 0 {
		return nil, fmt.Errorf("empty require spec")
	}

	nsName, err := extractNamespaceName(vec.Items[0])
	if err != nil {
		return nil, err
	}

	spec := &requireSpec{
		namespace: nsName,
		rename:    make(map[string]string),
	}

	i := 1
	for i < len(vec.Items) {
		kw, ok := vec.Items[i].(atom.Keyword)
		if !ok {
			return nil, fmt.Errorf("expected keyword, got %s", vec.Items[i].Type())
		}

		switch kwStr := string(kw); kwStr {
		case "as":
			i++
			if i >= len(vec.Items) {
				return nil, fmt.Errorf(":as requires an argument")
			}
			alias, ok := vec.Items[i].(types.Symbol)
			if !ok {
				return nil, fmt.Errorf(":as argument must be a symbol, got %s", vec.Items[i].Type())
			}
			spec.alias = string(alias)

		case "refer":
			i++
			if i >= len(vec.Items) {
				return nil, fmt.Errorf(":refer requires an argument")
			}
			switch rv := vec.Items[i].(type) {
			case atom.Keyword:
				if string(rv) == "all" {
					spec.referAll = true
				} else {
					return nil, fmt.Errorf(":refer argument must be :all or a vector, got %s", rv)
				}
			case *coll.Vector:
				spec.refer = make([]string, 0, len(rv.Items))
				for _, item := range rv.Items {
					sym, ok := item.(types.Symbol)
					if !ok {
						return nil, fmt.Errorf(":refer vector must contain symbols, got %s", item.Type())
					}
					spec.refer = append(spec.refer, string(sym))
				}
			default:
				return nil, fmt.Errorf(":refer argument must be :all or a vector, got %s", vec.Items[i].Type())
			}

		case "rename":
			i++
			if i >= len(vec.Items) {
				return nil, fmt.Errorf(":rename requires an argument")
			}
			renameMap, ok := vec.Items[i].(*coll.Map)
			if !ok {
				return nil, fmt.Errorf(":rename argument must be a map, got %s", vec.Items[i].Type())
			}
			for _, entry := range renameMap.AllEntries() {
				keySym, ok := entry.Key.(types.Symbol)
				if !ok {
					return nil, fmt.Errorf(":rename key must be a symbol, got %s", entry.Key.Type())
				}
				valSym, ok := entry.Value.(types.Symbol)
				if !ok {
					return nil, fmt.Errorf(":rename value must be a symbol, got %s", entry.Value.Type())
				}
				spec.rename[string(keySym)] = string(valSym)
			}

		default:
			return nil, fmt.Errorf("unknown require option: :%s", kwStr)
		}
		i++
	}

	return spec, nil
}

func extractNamespaceName(v types.Value) (string, error) {
	switch ns := v.(type) {
	case types.Symbol:
		return string(ns), nil
	case atom.Keyword:
		return string(ns), nil
	default:
		return "", fmt.Errorf("namespace must be a symbol or keyword, got %s", v.Type())
	}
}

type runnerNamespaceLoader struct {
	loader *Loader
}

func (l *runnerNamespaceLoader) LoadFile(path string) (types.Value, error) {
	if l == nil || l.loader == nil {
		return nil, nil
	}
	return l.loader.LoadFile(path)
}

func (l *runnerNamespaceLoader) Require(ns string) error {
	if l == nil || l.loader == nil {
		return nil
	}
	return l.loader.Require(ns)
}

func (l *runnerNamespaceLoader) ResolveResourcePath(name string) (string, bool) {
	if l == nil || l.loader == nil {
		return "", false
	}
	return l.loader.ResolveResourcePath(name)
}

func (l *runnerNamespaceLoader) GetLoadPaths() []string {
	if l == nil || l.loader == nil {
		return nil
	}
	return append([]string{}, l.loader.GetLoadPaths()...)
}

func (l *runnerNamespaceLoader) ProjectRoot() string {
	if l == nil || l.loader == nil {
		return ""
	}
	return l.loader.projectRoot
}

type runnerNamespaceState struct {
	runner *Runner
}

func (s *runnerNamespaceState) CurrentNamespace() string {
	if s == nil || s.runner == nil {
		return ""
	}
	return s.runner.CurrentNamespace()
}

func (s *runnerNamespaceState) SetCurrentNamespace(ns string) {
	if s == nil || s.runner == nil {
		return
	}
	s.runner.SetCurrentNamespace(ns)
}
