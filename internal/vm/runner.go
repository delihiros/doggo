package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"doggo/internal/compiler"
	compileropt "doggo/internal/compiler/opt"
	doggoerr "doggo/internal/errors"
	"doggo/internal/project"
	_ "doggo/internal/reify" // reifyファクトリの登録
	"doggo/internal/stm"
	"doggo/internal/syntax/form"
	"doggo/internal/syntax/frontend"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/vm/engine"
)

// Runner はVMを使用してDoggoコードを実行するためのヘルパーです。
type Runner struct {
	session         *executionSession
	builtinCatalog  *BuiltinCatalog
	symbolTable     *compiler.SymbolTable
	macroExpander   *MacroExpander
	optConfig       *compileropt.Config
	replSeq         int
	compileSeq      int
	lastException   types.Value
	runtimeServices *runnerServicesBundle
	commandLineArgs []string
	entryPointFile  string
}

type RunnerConfig struct {
	ProjectRoot       string
	LoadPaths         []string
	GenerationSources []*project.GenerationSource
	Lock              *project.LockFile
	BootstrapCore     bool
	RuntimeProfile    *RuntimeProfile
}

func resolveRunnerRuntimeProfile(cfg RunnerConfig) *RuntimeProfile {
	if cfg.RuntimeProfile != nil {
		profile := cfg.RuntimeProfile.clone()
		if profile.builtinCatalog == nil {
			profile.builtinCatalog = MustBuiltinCatalog()
		}
		return profile
	}

	return DefaultRuntimeProfile()
}

// NewRunner は新しいRunnerを作成します。
func NewRunner() (*Runner, error) {
	return NewRunnerWithConfig(RunnerConfig{
		BootstrapCore: true,
	})
}

// NewRunnerWithoutCore はコアライブラリをロードせずに新しいRunnerを作成します。
// テスト用途など、軽量な Runner が必要な場合に使用します。
func NewRunnerWithoutCore() (*Runner, error) {
	return NewRunnerWithConfig(RunnerConfig{
		BootstrapCore: false,
	})
}

// MustNewRunner は NewRunner が失敗した場合に panic します。
func MustNewRunner() *Runner {
	runner, err := NewRunner()
	if err != nil {
		panic(err)
	}
	return runner
}

// MustNewRunnerWithoutCore は NewRunnerWithoutCore が失敗した場合に panic します。
func MustNewRunnerWithoutCore() *Runner {
	runner, err := NewRunnerWithoutCore()
	if err != nil {
		panic(err)
	}
	return runner
}

// NewRunnerWithConfig は初期ロード設定を明示して新しい Runner を作成します。
func NewRunnerWithConfig(cfg RunnerConfig) (*Runner, error) {
	profile := resolveRunnerRuntimeProfile(cfg)

	if strings.TrimSpace(cfg.ProjectRoot) == "" {
		if wd, err := os.Getwd(); err == nil {
			cfg.ProjectRoot = wd
		}
	}
	if cfg.LoadPaths == nil {
		if strings.TrimSpace(cfg.ProjectRoot) == "" {
			cfg.LoadPaths = []string{"."}
		} else {
			cfg.LoadPaths = []string{cfg.ProjectRoot}
		}
	}

	runner := newRunnerBase(runnerBaseConfig{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         append([]string(nil), cfg.LoadPaths...),
		GenerationSources: append([]*project.GenerationSource(nil), cfg.GenerationSources...),
		Lock:              cfg.Lock,
		BootstrapCore:     cfg.BootstrapCore,
		Profile:           profile,
	})

	if err := runner.initializeRuntimeProfile(profile, cfg.BootstrapCore); err != nil {
		return nil, err
	}
	return runner, nil
}

type runnerBaseConfig struct {
	ProjectRoot       string
	LoadPaths         []string
	GenerationSources []*project.GenerationSource
	Lock              *project.LockFile
	BootstrapCore     bool
	Profile           *RuntimeProfile
}

func newRunnerBase(cfg runnerBaseConfig) *Runner {
	symbolTable := compiler.NewSymbolTable()
	builtinDefinitions := cfg.Profile.BuiltinCatalog().Definitions()

	// 組み込み関数をシンボルテーブルに登録
	for i, b := range builtinDefinitions {
		if b.IsPublic() {
			symbolTable.DefineBuiltin(i, b.Name)
			continue
		}
		symbolTable.DefinePrivateBuiltin(i, b.Name)
	}

	stmStore := stm.NewStore()
	globals := make([]types.Value, GlobalsSize)
	constantStore := compiler.NewConstantStore(nil)
	constants := constantStore.Values()
	session := newExecutionSession()
	session.globals = globals
	session.constants = constants
	session.constantStore = constantStore
	session.stmStore = stmStore

	macroExpander := NewMacroExpander()
	macroExpander.SetSymbolTable(symbolTable)

	r := &Runner{
		session:        session,
		builtinCatalog: cfg.Profile.BuiltinCatalog(),
		symbolTable:    symbolTable,
		macroExpander:  macroExpander,
	}
	session.runner = r
	session.observability = NewObservabilityState()
	session.callableRuntime = newRunnerExactCallableRuntime(r)

	r.runtimeServices = newRunnerServices(r, RunnerConfig{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         append([]string{}, cfg.LoadPaths...),
		GenerationSources: append([]*project.GenerationSource(nil), cfg.GenerationSources...),
		Lock:              cfg.Lock,
		BootstrapCore:     cfg.BootstrapCore,
		RuntimeProfile:    cfg.Profile,
	})
	r.macroExpander.SetClosureExecutor(r.runtimeServices.execution.macroExecutor())

	return r
}

// BootstrapCore initializes the core runtime contract and loads doggo.core once.
func (r *Runner) BootstrapCore() error {
	if r.runtimeServices == nil || r.runtimeServices.core == nil {
		return nil
	}
	return r.runtimeServices.core.BootstrapOnce()
}

func (r *Runner) initializeCoreGlobalIfMissing(name string, value types.Value) {
	session := r.getOrCreateExecutionSession()
	qualified := compiler.QualifySymbol(compiler.CoreNamespace, name)
	if sym, ok := r.symbolTable.Resolve(qualified); ok {
		if sym.Scope == compiler.GlobalScope || sym.Scope == compiler.ProtectedGlobalScope {
			if sym.Index >= 0 && sym.Index < len(session.globals) && session.globals[sym.Index] != nil {
				return
			}
		}
	}
	sym := r.symbolTable.DefineGlobalInNamespace(compiler.CoreNamespace, name)
	session.globals[sym.Index] = value
}

func (r *Runner) runtimeLoader() *Loader {
	if r == nil || r.runtimeServices == nil {
		return nil
	}
	if r.runtimeServices.namespaces == nil {
		return nil
	}
	loader, _ := r.runtimeServices.namespaces.Loader().(*runnerNamespaceLoader)
	if loader == nil {
		return nil
	}
	return loader.loader
}

func (r *Runner) runtimeNamespaces() *runnerNamespaceServices {
	if r == nil || r.runtimeServices == nil {
		return nil
	}
	return r.runtimeServices.namespaces
}

func (r *Runner) runtimeSources() *runnerSourceRegistryServices {
	if r == nil || r.runtimeServices == nil {
		return nil
	}
	return r.runtimeServices.sources
}

func (r *Runner) executionServices() *runnerExecutionServices {
	if r == nil || r.runtimeServices == nil {
		return nil
	}
	return r.runtimeServices.execution
}

func (r *Runner) runtimeCompile() *runnerCompileServices {
	if r == nil || r.runtimeServices == nil {
		return nil
	}
	return r.runtimeServices.compile
}

func (r *Runner) InstallRuntimeFeatures(features []RuntimeFeature) error {
	if r == nil {
		return nil
	}
	return installRuntimeFeatures(r, features)
}

func (r *Runner) InstallRuntimeModules(modules []RunnerModule) error {
	if r == nil {
		return nil
	}
	return installRunnerModules(r, modules)
}

// SetContext は実行コンテキストを設定します。
func (r *Runner) SetContext(ctx context.Context) {
	r.getOrCreateExecutionSession().ctx = ctx
}

// SetProfiler はプロファイラを設定します。
func (r *Runner) SetProfiler(p *Profiler) {
	r.getOrCreateExecutionSession().profiler = p
}

// SetCommandLineArgs stores the script arguments visible to doggo.cli.
func (r *Runner) SetCommandLineArgs(args []string) {
	if args == nil {
		r.commandLineArgs = nil
		r.syncCommandLineArgsRootVar()
		return
	}
	r.commandLineArgs = append(make([]string, 0, len(args)), args...)
	r.syncCommandLineArgsRootVar()
}

// SetEntryPointFile stores the file that was launched as the current program.
func (r *Runner) SetEntryPointFile(path string) {
	r.entryPointFile = r.normalizeEntryPointFile(path)
}

func (r *Runner) isEntryPointOrigin(origin engine.SourceOrigin) bool {
	if strings.TrimSpace(r.entryPointFile) == "" {
		return false
	}
	return r.normalizeEntryPointFile(origin.FileOrUnknown()) == r.entryPointFile
}

func (r *Runner) normalizeEntryPointFile(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "<") || strings.HasPrefix(trimmed, "stdlib://") {
		return filepath.ToSlash(trimmed)
	}

	nativePath := filepath.FromSlash(trimmed)
	loader := r.runtimeLoader()
	if loader != nil {
		canonical := nativePath
		if filepath.IsAbs(nativePath) {
			canonical = loader.resolvePathBestEffort(nativePath)
		}
		if filepath.IsAbs(canonical) && loader.projectRoot != "" {
			if rel, err := filepath.Rel(loader.projectRoot, canonical); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return filepath.ToSlash(rel)
			}
		}
		if filepath.IsAbs(canonical) {
			return filepath.ToSlash(canonical)
		}
		return filepath.ToSlash(filepath.Clean(canonical))
	}

	if filepath.IsAbs(nativePath) {
		if abs, err := filepath.Abs(nativePath); err == nil {
			return filepath.ToSlash(abs)
		}
	}
	return filepath.ToSlash(filepath.Clean(nativePath))
}

func (r *Runner) syncCommandLineArgsRootVar() {
	session := r.getOrCreateExecutionSession()
	saved := r.symbolTable.CurrentNamespace()
	r.symbolTable.SetCurrentNamespace("doggo.cli")
	defer r.symbolTable.SetCurrentNamespace(saved)

	sym, ok := r.symbolTable.Resolve("doggo.cli/*command-line-args*")
	if !ok || sym.Scope != compiler.GlobalScope || sym.Index < 0 || sym.Index >= len(session.globals) {
		return
	}
	varObj, ok := session.globals[sym.Index].(*atom.Var)
	if !ok {
		return
	}
	varObj.Value = stringsToArgsVector(r.commandLineArgs)
}

// GetProfiler はプロファイラを取得します。
func (r *Runner) GetProfiler() *Profiler {
	return r.getOrCreateExecutionSession().profiler
}

// SetOptimizationConfig は最適化設定を設定します。
func (r *Runner) SetOptimizationConfig(config *compileropt.Config) {
	r.optConfig = config
}

// GetOptimizationConfig は最適化設定を取得します。
func (r *Runner) GetOptimizationConfig() *compileropt.Config {
	return r.optConfig
}

// DisableLoopFusion はコンパイラのループフュージョンを無効にします。
func (r *Runner) DisableLoopFusion() {
	r.optConfig = compileropt.NoLoopFusionConfig()
}

// Run はDoggoのソースコードを解析、コンパイル、実行します。
// 複数のトップレベル式がある場合は、最後の式の結果を返します。
func (r *Runner) Run(source string) (types.Value, error) {
	return r.RunWithOrigin(source, r.nextREPLOrigin())
}

// RunWithOrigin はソースコードの由来情報を付与して実行します。
func (r *Runner) RunWithOrigin(source string, origin engine.SourceOrigin) (types.Value, error) {
	result, err := r.runSource(source, origin, false, r.contextForExecution())
	if err != nil {
		r.rememberLastError(err)
	}
	return result, err
}

func (r *Runner) runWithOriginTrusted(source string, origin engine.SourceOrigin) (types.Value, error) {
	return r.runSource(source, origin, true, r.contextForExecution())
}

// runSource はソースコードをパースして実行します。
func (r *Runner) runSource(source string, origin engine.SourceOrigin, allowPrivateBuiltins bool, execCtx context.Context) (types.Value, error) {
	origin.File = origin.FileOrUnknown()
	r.registerSource(origin, source)
	if execCtx == nil {
		execCtx = r.contextForExecution()
	}

	reader, err := frontend.NewReader(origin.File, source)
	if err != nil {
		return nil, r.wrapSourceError(origin, err)
	}

	var result types.Value

	for {
		node, err := reader.ReadSyntaxNext()
		if err != nil {
			return nil, r.wrapSourceError(origin, err)
		}
		if node == nil {
			// EOF
			break
		}

		result, err = r.runSyntaxWithContext(node, origin, execCtx, allowPrivateBuiltins)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (r *Runner) runSyntaxWithContext(node *form.Node, origin engine.SourceOrigin, execCtx context.Context, allowPrivateBuiltins bool) (types.Value, error) {
	origin.File = origin.FileOrUnknown()
	if execCtx == nil {
		execCtx = r.contextForExecution()
	}
	execCtx = withSourceOrigin(execCtx, origin)

	compileCtx, leave := r.runtimeLoader().enter(execCtx)
	if namespace := namespaceFromContext(compileCtx); namespace != "" {
		r.symbolTable.SetCurrentNamespace(namespace)
	} else {
		setNamespaceInContext(compileCtx, r.symbolTable.CurrentNamespace())
	}
	program, err := r.runtimeCompile().compileSyntaxNode(node, origin, allowPrivateBuiltins)
	namespace := r.symbolTable.CurrentNamespace()
	setNamespaceInContext(compileCtx, namespace)
	var namespaceVar *atom.Var
	if symbol, ok := r.symbolTable.Resolve(compiler.QualifySymbol(compiler.StdlibCoreNamespace, "*ns*")); ok {
		session := r.getOrCreateExecutionSession()
		if symbol.Index >= 0 && symbol.Index < len(session.globals) {
			namespaceVar, _ = session.globals[symbol.Index].(*atom.Var)
		}
	}
	leave()
	if err != nil {
		return nil, err
	}

	// VM実行
	machine, err := r.executionServices().newMachine(program.bytecode, compileCtx, false)
	if err != nil {
		return nil, err
	}
	if namespaceVar != nil {
		PushDynamicBindings(compileCtx, map[*atom.Var]types.Value{
			namespaceVar: types.NewSymbol(namespace),
		})
		defer PopDynamicBindings(compileCtx)
	}

	if err := machine.Run(); err != nil {
		return nil, err
	}

	return machine.StackTop(), nil
}

// RunMultiple は複数の式を順番に実行します。
func (r *Runner) RunMultiple(sources []string) (types.Value, error) {
	var result types.Value
	for _, source := range sources {
		var err error
		result, err = r.Run(source)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Compile は匿名ソースコードをコンパイルして、コンパイル済みプログラムを返します。
// 実行は行いません。compile-once/run-many ベンチマーク用です。
func (r *Runner) Compile(source string) (*engine.Program, error) {
	return r.compileWithOrigin(source, r.nextCompileOrigin(), false)
}

// CompileWithOrigin はソースコードの由来情報を明示してコンパイルします。
// origin は診断や source introspection にのみ使われ、private builtin の可視性は変更しません。
func (r *Runner) CompileWithOrigin(source string, origin engine.SourceOrigin) (*engine.Program, error) {
	return r.compileWithOrigin(source, origin, false)
}

func (r *Runner) compileWithOriginTrusted(source string, origin engine.SourceOrigin) (*engine.Program, error) {
	return r.compileWithOrigin(source, origin, true)
}

func (r *Runner) compileWithOrigin(source string, origin engine.SourceOrigin, allowPrivateBuiltins bool) (*engine.Program, error) {
	origin.File = origin.FileOrUnknown()
	if origin.File == "" {
		origin = r.nextCompileOrigin()
	}
	ctx, leave := r.runtimeLoader().enter(r.contextForExecution())
	if namespace := namespaceFromContext(ctx); namespace != "" {
		r.symbolTable.SetCurrentNamespace(namespace)
	} else {
		setNamespaceInContext(ctx, r.symbolTable.CurrentNamespace())
	}
	code, err := r.runtimeCompile().compileSource(source, origin, allowPrivateBuiltins)
	setNamespaceInContext(ctx, r.symbolTable.CurrentNamespace())
	leave()
	if err != nil {
		r.rememberLastError(err)
		return nil, err
	}
	return code, nil
}

// RunPrecompiled はコンパイル済みコードを実行します。
// compile-once/run-many ベンチマーク用です。
func (r *Runner) RunPrecompiled(code *engine.Program) (types.Value, error) {
	r.registerPrecompiledSource(code)
	machine, err := r.executionServices().newMachine(code.Bytecode, r.contextForExecution(), false)
	if err != nil {
		return nil, err
	}

	if err := machine.Run(); err != nil {
		r.rememberLastError(err)
		return nil, err
	}

	return machine.StackTop(), nil
}

// RunPrecompiledWithStats はコンパイル済みコードを実行し、実行統計も返します。
func (r *Runner) RunPrecompiledWithStats(code *engine.Program) (types.Value, *ExecStats, error) {
	r.registerPrecompiledSource(code)
	machine, err := r.executionServices().newMachine(code.Bytecode, r.contextForExecution(), true)
	if err != nil {
		return nil, nil, err
	}

	if err := machine.Run(); err != nil {
		r.rememberLastError(err)
		return nil, nil, err
	}

	return machine.StackTop(), machine.GetExecStats(), nil
}

// LoadFile はファイルを読み込み、現在の環境で実行します。
func (r *Runner) LoadFile(path string) (types.Value, error) {
	return r.runtimeNamespaces().LoadFile(path)
}

// Require は名前空間名からファイルを検索してロードします。
// 既にロード済みの場合は何もしません。
func (r *Runner) Require(nsName string) error {
	return r.runtimeNamespaces().RequireFromContext(r.contextForExecution(), nsName)
}

// CurrentNamespace は現在の名前空間を返します。
func (r *Runner) CurrentNamespace() string {
	return r.symbolTable.CurrentNamespace()
}

// SetCurrentNamespace は現在の名前空間を設定します。
func (r *Runner) SetCurrentNamespace(ns string) {
	setNamespaceInContext(r.contextForExecution(), ns)
	r.setCurrentNamespaceState(ns)
}

func (r *Runner) setCurrentNamespaceState(ns string) {
	r.symbolTable.SetCurrentNamespace(ns)
	r.syncNamespaceRootVar(ns)
}

func (r *Runner) syncNamespaceRootVar(ns string) {
	session := r.getOrCreateExecutionSession()
	if strings.TrimSpace(ns) == "" {
		return
	}
	sym := r.symbolTable.DefineGlobalInNamespace(compiler.StdlibCoreNamespace, "*ns*")
	value := types.NewSymbol(ns)
	if variable, ok := session.globals[sym.Index].(*atom.Var); ok {
		variable.Value = value
		return
	}
	session.globals[sym.Index] = value
}

func (r *Runner) nextREPLOrigin() engine.SourceOrigin {
	r.replSeq++
	return engine.SourceOrigin{File: fmt.Sprintf("<repl>#%d", r.replSeq)}
}

func (r *Runner) nextCompileOrigin() engine.SourceOrigin {
	r.compileSeq++
	return engine.SourceOrigin{File: fmt.Sprintf("<compiled>#%d", r.compileSeq)}
}

func (r *Runner) registerSource(origin engine.SourceOrigin, source string) {
	r.runtimeSources().registerOrigin(origin, source)
}

func (r *Runner) resolveSourceOrigin(file string) (engine.SourceOrigin, bool) {
	return r.runtimeSources().resolveOrigin(file)
}

func (r *Runner) registerPrecompiledSource(code *engine.Program) {
	r.runtimeSources().registerPrecompiled(code)
}

func (r *Runner) rememberLastError(err error) {
	if err == nil {
		return
	}
	r.lastException = r.exceptionFromError(err)
}

func (r *Runner) lastExceptionValue() types.Value {
	if r.lastException == nil {
		return atom.NilValue
	}
	return r.lastException
}

func (r *Runner) exceptionFromError(err error) types.Value {
	var compileErr *compiler.CompileError
	if errors.As(err, &compileErr) {
		return exceptionFromCanonicalData(canonicalCompileDiagnosticData(
			compileErr.File,
			compileErr.Line,
			compileErr.Column,
			compileErr.Message,
			compileErr.SnippetLine,
			compileErr.SnippetCol,
		))
	}

	var sourceErr *SourceDiagnosticError
	if errors.As(err, &sourceErr) {
		return exceptionFromCanonicalData(canonicalSourceDiagnosticData(
			"SourceDiagnosticError",
			"source diagnostic error",
			"read",
			sourceErr.Message,
			sourceErr.File,
			sourceErr.Line,
			sourceErr.Column,
			sourceErr.Snippet,
			sourceErr.SnippetCol,
		))
	}

	return normalizeError(err)
}

func canonicalCompileDiagnosticData(file string, line, column int, message, snippet string, snippetCol int) *doggoerr.ExceptionData {
	return canonicalSourceDiagnosticData(
		"CompileError",
		"compile error",
		"compile",
		message,
		file,
		line,
		column,
		snippet,
		snippetCol,
	)
}

func canonicalSourceDiagnosticData(typeName, kind, phase, message, file string, line, column int, snippet string, snippetCol int) *doggoerr.ExceptionData {
	data := map[string]any(nil)
	context := map[string]any(nil)
	if snippet != "" || snippetCol > 0 {
		data = map[string]any{
			"snippet":        snippet,
			"snippet_column": snippetCol,
		}
		if snippet != "" {
			caretCol := snippetCol
			if caretCol <= 0 {
				caretCol = column
			}
			if caretCol <= 0 {
				caretCol = 1
			}
			data["formatted_snippet"] = formatDiagnosticSnippet(line, snippet, caretCol)
		}
		context = map[string]any{
			"snippet":        snippet,
			"snippet_column": snippetCol,
		}
	}

	trace := []doggoerr.ExceptionFrame(nil)
	if line > 0 {
		trace = []doggoerr.ExceptionFrame{{
			File:   file,
			Line:   line,
			Column: column,
		}}
	}

	return &doggoerr.ExceptionData{
		Type:    typeName,
		Kind:    kind,
		Phase:   phase,
		Message: message,
		Data:    data,
		Context: context,
		At: doggoerr.Position{
			File:   file,
			Line:   line,
			Column: column,
		},
		Trace: trace,
	}
}

func formatDiagnosticSnippet(lineNumber int, lineText string, caretCol int) string {
	if caretCol <= 0 {
		caretCol = 1
	}
	runes := []rune(lineText)
	if len(runes) == 0 {
		caretCol = 1
	} else if caretCol > len(runes) {
		caretCol = len(runes)
	}

	lineNum := fmt.Sprintf("%d", lineNumber)
	var sb strings.Builder
	sb.WriteString(lineNum)
	sb.WriteString(" | ")
	sb.WriteString(lineText)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat(" ", len(lineNum)))
	sb.WriteString(" | ")
	sb.WriteString(strings.Repeat(" ", caretCol-1))
	sb.WriteString("^")
	return sb.String()
}

func (r *Runner) lookupSourceLine(file string, line int) (string, bool) {
	return r.runtimeSources().lookupLine(file, line)
}

func (r *Runner) wrapSourceError(origin engine.SourceOrigin, err error) error {
	var doggoErr *doggoerr.DoggoError
	if !errors.As(err, &doggoErr) {
		return err
	}

	line := doggoErr.Position.Line
	column := doggoErr.Position.Column
	if column <= 0 {
		column = 1
	}

	file := origin.FileOrUnknown()
	snippet, _ := r.lookupSourceLine(file, line)
	return &SourceDiagnosticError{
		File:       file,
		Line:       line,
		Column:     column,
		Message:    doggoErr.Message,
		Snippet:    snippet,
		SnippetCol: column,
	}
}
