package vm

import (
	"errors"

	"doggo/internal/compiler"
	"doggo/internal/syntax/form"
	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/internal/vm/engine"
)

type runnerCompileServices struct {
	runner  *Runner
	sources *runnerSourceRegistryServices
}

type runnerCompiledProgram struct {
	bytecode      *compiler.Bytecode
	macros        map[string]*compiler.Closure
	macroMetadata map[string]*coll.Map
}

func newRunnerCompileServices(runner *Runner, sources *runnerSourceRegistryServices) *runnerCompileServices {
	return &runnerCompileServices{
		runner:  runner,
		sources: sources,
	}
}

func (services *runnerCompileServices) compileSource(source string, origin engine.SourceOrigin, allowPrivateBuiltins bool) (*engine.Program, error) {
	origin.File = origin.FileOrUnknown()
	services.runner.registerSource(origin, source)

	session := services.newSession(allowPrivateBuiltins)
	bytecode, err := session.CompileSourceForRuntime(origin.File, source)
	if err != nil {
		return nil, services.enrichCompileError(origin, err)
	}

	program := &runnerCompiledProgram{
		bytecode:      bytecode,
		macros:        session.Macros(),
		macroMetadata: session.MacroMetadata(),
	}
	services.apply(program)
	return &engine.Program{
		Bytecode:   bytecode,
		Constants:  bytecode.Constants,
		Macros:     program.macros,
		SourceFile: origin.File,
		Source:     source,
	}, nil
}

func (services *runnerCompileServices) compileSyntaxNode(node *form.Node, origin engine.SourceOrigin, allowPrivateBuiltins bool) (*runnerCompiledProgram, error) {
	origin.File = origin.FileOrUnknown()
	session := services.newSession(allowPrivateBuiltins)
	bytecode, err := session.CompileSyntaxNodeForRuntime(node)
	if err != nil {
		return nil, services.enrichCompileError(origin, err)
	}
	program := &runnerCompiledProgram{
		bytecode:      bytecode,
		macros:        session.Macros(),
		macroMetadata: session.MacroMetadata(),
	}
	services.apply(program)
	return program, nil
}

func (services *runnerCompileServices) newSession(allowPrivateBuiltins bool) *compiler.Session {
	session := services.runner.getOrCreateExecutionSession()
	return compiler.NewSession(compiler.SessionConfig{
		SymbolTable:           services.runner.symbolTable,
		ConstantStore:         session.constantStore,
		Macros:                services.runner.macroExpander.Macros(),
		MacroMetadata:         services.runner.macroExpander.MacroMetadata(),
		SourceLineProvider:    services.sources.lookupLine,
		SourceContentProvider: services.sources.lookupContent,
		OptimizationConfig:    services.runner.optConfig,
		AllowPrivateBuiltins:  allowPrivateBuiltins,
		MacroEvaluator:        services.macroEvaluator(),
	})
}

func (services *runnerCompileServices) macroEvaluator() compiler.MacroEvaluator {
	return func(fn *compiler.Closure, args []types.Value) (types.Value, error) {
		return services.runner.macroExpander.EvaluateMacro(fn, args)
	}
}

func (services *runnerCompileServices) apply(program *runnerCompiledProgram) {
	if program == nil {
		return
	}
	if session := services.runner.getOrCreateExecutionSession(); session != nil {
		session.constants = session.constantStore.Values()
	}
	services.runner.macroExpander.ReplaceMacros(program.macros, program.macroMetadata)
}

func (services *runnerCompileServices) enrichCompileError(origin engine.SourceOrigin, err error) error {
	var compileErr *compiler.CompileError
	if !errors.As(err, &compileErr) {
		return err
	}
	if compileErr.File == "" {
		compileErr.File = origin.File
	}
	if compileErr.SnippetLine != "" || compileErr.Line <= 0 {
		return err
	}
	line, ok := services.sources.lookupLine(compileErr.File, compileErr.Line)
	if !ok {
		return err
	}
	col := compileErr.Column
	if col <= 0 {
		col = 1
	}
	compileErr.SetSnippet(line, col)
	return err
}
