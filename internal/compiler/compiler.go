// Package compiler はDoggo言語のASTをバイトコードにコンパイルします。
package compiler

import (
	"doggo/internal/compiler/debugmeta"
	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/hir"
	"doggo/internal/compiler/opt"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/coll"
)

// Bytecode はコンパイル結果を表します。
type Bytecode struct {
	Instructions opcode.Instructions
	Constants    []types.Value
	BuiltinNames []string
	Lines        []int // 各命令の行番号（命令のオフセットに対応）
	Columns      []int // 各命令の列番号（命令のオフセットに対応）
	SourceFile   string
	Namespace    string
	NumLocals    int // トップレベルコードで使用されるローカル変数の数
}

type bytecodeMetadata struct {
	SourceFile string
	Lines      []int
	Columns    []int
}

func (c *Compiler) ResolveOptimizationSymbol(name string) (opt.Binding, bool) {
	sym, ok := c.symbolTable.Resolve(name)
	if !ok {
		return opt.Binding{}, false
	}
	return opt.Binding{Scope: string(sym.Scope)}, true
}

// LoopContext はループのコンパイル情報を保持します。
type LoopContext struct {
	StartPos         int      // ループ本体の開始位置
	Bindings         []Symbol // ループバインディングのシンボル
	TryDepth         int      // Number of protected regions surrounding the loop target.
	PendingRecurArgs int      // Pending arguments inherited by a loop inside finally.
}

// MacroEvaluator はマクロ関数を実行するための関数型です。
// コンパイラから VM を呼び出してマクロを評価します。
type MacroEvaluator func(fn *Closure, args []types.Value) (types.Value, error)

// Compiler はASTをバイトコードにコンパイルします。
type Compiler struct {
	debugMeta             *debugmeta.Owner
	constants             *ConstantStore
	symbolTable           *SymbolTable
	scopes                []CompilationScope
	scopeIndex            int
	loopStack             []LoopContext       // ループコンテキストのスタック
	macros                map[string]*Closure // マクロ名 → クロージャ
	macroMetadata         map[string]*coll.Map
	macroEvaluator        MacroEvaluator // マクロ実行用の関数
	sourceContentProvider func(file string) (string, bool)
	diagnosticOwner       *diagnosticOwner
	optConfig             *opt.Config // 最適化設定
	defInitStack          []string
	topLevelMaxLocals     int
	compileDepth          int
	allowPrivateBuiltins  bool
}

var (
	_ emit.SourceHooks     = (*Compiler)(nil)
	_ emit.DiagnosticHooks = (*Compiler)(nil)
)

// CompilationScope はコンパイル中のスコープを表します。
type CompilationScope struct {
	instructions          opcode.Instructions
	lastInstruction       EmittedInstruction
	previousInstruction   EmittedInstruction
	catchFinallyLoopDepth int // Recur must stay in loops created inside an active catch/finally.
	tryRegions            []tryRegion
	pendingRecurArgs      int
}

// EmittedInstruction は発行された命令の情報を保持します。
type EmittedInstruction struct {
	Opcode   opcode.Opcode
	Position int
}

// New は新しいCompilerを作成します。
func New() *Compiler {
	mainScope := CompilationScope{
		instructions:        opcode.Instructions{},
		lastInstruction:     EmittedInstruction{},
		previousInstruction: EmittedInstruction{},
	}
	owner := debugmeta.NewOwner()

	return &Compiler{
		debugMeta:       owner,
		constants:       NewConstantStore(nil),
		symbolTable:     NewSymbolTable(),
		scopes:          []CompilationScope{mainScope},
		scopeIndex:      0,
		macros:          make(map[string]*Closure),
		macroMetadata:   make(map[string]*coll.Map),
		diagnosticOwner: newDiagnosticOwner(owner, nil),
		optConfig:       opt.DefaultOptimizationConfig(),
	}
}

// SetOptimizationConfig は最適化設定を設定します。
func (c *Compiler) SetOptimizationConfig(config *opt.Config) {
	if config != nil {
		c.optConfig = config
	}
}

// OptimizationConfig は現在の最適化設定を返します。
func (c *Compiler) OptimizationConfig() *opt.Config {
	return c.optConfig
}

func (c *Compiler) SetAllowPrivateBuiltins(allow bool) {
	c.allowPrivateBuiltins = allow
}

func (c *Compiler) SetNamespace(namespace string) {
	c.symbolTable.SetCurrentNamespace(namespace)
}

func (c *Compiler) AddAlias(alias, namespace string) {
	c.symbolTable.AddAlias(alias, namespace)
}

func (c *Compiler) WithCompileEntry(compile func() error) error {
	return c.withCompileEntry(compile)
}

func (c *Compiler) NewCompileError(format string, args ...interface{}) error {
	return c.newCompileError(format, args...)
}

func (c *Compiler) SetFormSourceSpan(sp span.Span) {
	c.setSourceSpan(sp)
}

func (c *Compiler) EmitConstant(value types.Value) {
	c.emit(opcode.OpConst, c.addConstant(value))
}

func (c *Compiler) EmitBool(value bool) {
	if value {
		c.emit(opcode.OpTrue)
		return
	}
	c.emit(opcode.OpFalse)
}

func (c *Compiler) EmitNil() {
	c.emit(opcode.OpNil)
}

func (c *Compiler) CompileSymbolReference(name string) error {
	symbol, ok := c.symbolTable.Resolve(name)
	if !ok {
		return c.newCompileError("undefined variable: %s", name)
	}
	c.loadSymbol(symbol)
	return nil
}

func (c *Compiler) CompileResolvedFormSymbol(sym sexpr.ResolvedSymbol) error {
	return c.compileResolvedFormSymbol(sym)
}

func (c *Compiler) CompileVectorFormLiteral(node *sexpr.Vector) error {
	return c.compileVectorFormLiteral(node)
}

func (c *Compiler) CompileMapFormLiteral(node *sexpr.Map) error {
	return c.compileMapFormLiteral(node)
}

func (c *Compiler) CompileSetFormLiteral(node *sexpr.Set) error {
	return c.compileSetFormLiteral(node)
}

func (c *Compiler) ExpandMacroForm(list *sexpr.List) (sexpr.Form, error) {
	return c.expandMacroForm(list)
}

func (c *Compiler) EffectiveSourceFile() string {
	if c == nil || c.debugMeta == nil {
		return ""
	}
	return c.debugMeta.EffectiveFile()
}

func (c *Compiler) PushExpansionSource(span debugmeta.Span) {
	if c == nil || c.debugMeta == nil {
		return
	}
	c.debugMeta.PushExpansionSource(span, nil)
}

func (c *Compiler) PopExpansionSource() {
	if c == nil || c.debugMeta == nil {
		return
	}
	c.debugMeta.PopExpansionSource()
}

func (c *Compiler) CompileResolvedSpecialForm(name string, list *sexpr.List) (bool, error) {
	compiler, ok := getResolvedSpecialFormCompiler(name)
	if !ok {
		return false, nil
	}
	if isOptimizedBuiltinForm(name) {
		if c.symbolTable.IsCoreSymbolExcluded(name) {
			return false, nil
		}
		symbol, resolved := c.symbolTable.Resolve(name)
		if resolved && symbol.Scope != BuiltinScope {
			return false, nil
		}
	}
	return true, compiler(c, list)
}

func isOptimizedBuiltinForm(name string) bool {
	switch name {
	case "+", "-", "*", "/", "mod", "=", "not=", ">", ">=", "<", "<=", "not":
		return true
	default:
		return false
	}
}

func (c *Compiler) EmitCall(argc int) {
	c.emit(opcode.OpCall, argc)
}

// NewWithState は既存の状態を引き継いで新しいCompilerを作成します。
func NewWithState(s *SymbolTable, constants *ConstantStore) *Compiler {
	compiler := New()
	compiler.symbolTable = s
	compiler.constants = constants
	compiler.symbolTable.ResetTopLevelLocals()
	// optConfig は New() でデフォルト設定済み
	return compiler
}

// NewWithMacros はマクロ状態も引き継いで新しいCompilerを作成します。
func NewWithMacros(s *SymbolTable, constants *ConstantStore, macros map[string]*Closure, macroMetadata map[string]*coll.Map, evaluator MacroEvaluator) *Compiler {
	compiler := NewWithState(s, constants)
	if macros != nil {
		compiler.macros = cloneClosureMap(macros)
	}
	if macroMetadata != nil {
		compiler.macroMetadata = cloneMetadataMap(macroMetadata)
	}
	compiler.macroEvaluator = evaluator
	return compiler
}

// SetMacroEvaluator はマクロ評価関数を設定します。
func (c *Compiler) SetMacroEvaluator(eval MacroEvaluator) {
	c.macroEvaluator = eval
}

// Macros は登録されているマクロのマップを返します。
func (c *Compiler) Macros() map[string]*Closure {
	return c.macros
}

// MacroMetadata returns metadata for registered macros.
func (c *Compiler) MacroMetadata() map[string]*coll.Map {
	return cloneMetadataMap(c.macroMetadata)
}

func cloneClosureMap(in map[string]*Closure) map[string]*Closure {
	out := make(map[string]*Closure, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneMetadataMap(in map[string]*coll.Map) map[string]*coll.Map {
	out := make(map[string]*coll.Map, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func CompileSource(path, src string) (*Bytecode, error) {
	return NewSession(SessionConfig{}).CompileSource(path, src)
}

type emitBackend struct {
	comp *Compiler
}

func newEmitBackend(comp *Compiler) emitBackend {
	return emitBackend{comp: comp}
}

func (b emitBackend) ApplyScope(namespace string, aliases map[string]string) error {
	if b.comp == nil {
		return nil
	}
	if namespace != "" {
		b.comp.SetNamespace(namespace)
	}
	for alias, target := range aliases {
		b.comp.AddAlias(alias, target)
	}
	return nil
}

func (b emitBackend) EmitNode(node hir.Node) error {
	if b.comp == nil || node == nil {
		return nil
	}
	return b.comp.CompileForm(node.Form())
}

func (b emitBackend) Finish() (*Bytecode, error) {
	if b.comp == nil {
		return nil, nil
	}
	return b.comp.Bytecode(), nil
}

// Bytecode はコンパイル結果を返します。
func (c *Compiler) Bytecode() *Bytecode {
	numLocals := c.topLevelMaxLocals
	if current := c.symbolTable.NumLocals(); current > numLocals {
		numLocals = current
	}
	meta := c.debugMeta.InstructionTable()
	return &Bytecode{
		Instructions: c.currentInstructions(),
		Constants:    c.constants.Values(),
		BuiltinNames: c.symbolTable.BuiltinLayout(),
		Lines:        meta.Lines,
		Columns:      meta.Columns,
		SourceFile:   meta.SourceFile,
		Namespace:    c.symbolTable.CurrentNamespace(),
		NumLocals:    numLocals,
	}
}

func (c *Compiler) withCompileEntry(compile func() error) error {
	isTopLevelForm := c.scopeIndex == 0 && c.compileDepth == 0
	if isTopLevelForm {
		c.symbolTable.ResetTopLevelLocals()
	}

	restorePrivateBuiltins := false
	var previousAllowPrivateBuiltins bool
	if c.compileDepth == 0 {
		previousAllowPrivateBuiltins = c.symbolTable.AllowPrivateBuiltins()
		c.symbolTable.SetAllowPrivateBuiltins(c.allowPrivateBuiltins)
		restorePrivateBuiltins = true
	}

	c.compileDepth++
	defer func() {
		c.compileDepth--
		if restorePrivateBuiltins {
			c.symbolTable.SetAllowPrivateBuiltins(previousAllowPrivateBuiltins)
		}
	}()

	if err := compile(); err != nil {
		return err
	}

	if isTopLevelForm {
		if current := c.symbolTable.NumLocals(); current > c.topLevelMaxLocals {
			c.topLevelMaxLocals = current
		}
	}

	return nil
}
