package compiler

import (
	"fmt"
	"strings"

	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/opt"
	"doggo/internal/compiler/resolve"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/syntax/form"
	"doggo/internal/syntax/frontend"
	"doggo/internal/types"
	"doggo/internal/types/coll"
)

type SessionConfig struct {
	SymbolTable           *SymbolTable
	ConstantStore         *ConstantStore
	Macros                map[string]*Closure
	MacroMetadata         map[string]*coll.Map
	SourceLineProvider    func(file string, line int) (string, bool)
	SourceContentProvider func(file string) (string, bool)
	OptimizationConfig    *opt.Config
	AllowPrivateBuiltins  bool
	MacroEvaluator
}

type Session struct {
	symbolTable    *SymbolTable
	constantStore  *ConstantStore
	macros         map[string]*Closure
	macroMetadata  map[string]*coll.Map
	macroEvaluator MacroEvaluator
	sourceLine     func(file string, line int) (string, bool)
	sourceContent  func(file string) (string, bool)
	optConfig      *opt.Config
	allowPrivate   bool
}

func NewSession(cfg SessionConfig) *Session {
	base := New()

	symbolTable := cfg.SymbolTable
	if symbolTable == nil {
		symbolTable = base.symbolTable
	}
	constantStore := cfg.ConstantStore
	if constantStore == nil {
		constantStore = base.constants
	}

	return &Session{
		symbolTable:    symbolTable,
		constantStore:  constantStore,
		macros:         cloneClosureMap(cfg.Macros),
		macroMetadata:  cloneMetadataMap(cfg.MacroMetadata),
		macroEvaluator: cfg.MacroEvaluator,
		sourceLine:     cfg.SourceLineProvider,
		sourceContent:  cfg.SourceContentProvider,
		optConfig:      cfg.OptimizationConfig,
		allowPrivate:   cfg.AllowPrivateBuiltins,
	}
}

func (s *Session) CompileSource(path, src string) (*Bytecode, error) {
	parsed := frontend.Parse(path, src)
	if len(parsed.Diagnostics) > 0 {
		return nil, fmt.Errorf("frontend diagnostics: %s", parsed.Diagnostics[0].Message)
	}

	file, err := sexpr.LowerFile(parsed.Forms)
	if err != nil {
		return nil, err
	}
	lineProvider, contentProvider := sourceProvidersForText(path, src, s.sourceLine, s.sourceContent)
	return s.compileFileWithProviders(file, true, lineProvider, contentProvider)
}

func (s *Session) CompileFile(file *sexpr.File) (*Bytecode, error) {
	return s.compileFileWithProviders(file, true, s.sourceLine, s.sourceContent)
}

func (s *Session) CompileSourceForRuntime(path, src string) (*Bytecode, error) {
	reader, err := frontend.NewReader(path, src)
	if err != nil {
		return nil, err
	}
	nodes, err := reader.ReadSyntaxAll()
	if err != nil {
		return nil, err
	}
	return s.compileSyntaxNodesForRuntime(nodes)
}

func (s *Session) CompileSyntaxNodeForRuntime(node *form.Node) (*Bytecode, error) {
	if node == nil {
		return s.compileSyntaxNodesForRuntime(nil)
	}
	return s.compileSyntaxNodesForRuntime([]*form.Node{node})
}

func (s *Session) compileFile(file *sexpr.File, scoped bool) (*Bytecode, error) {
	return s.compileFileWithProviders(file, scoped, s.sourceLine, s.sourceContent)
}

func (s *Session) compileFileWithProviders(file *sexpr.File, scoped bool, sourceLine func(file string, line int) (string, bool), sourceContent func(file string) (string, bool)) (*Bytecode, error) {
	prog, err := resolve.File(resolve.Config{}, file)
	if err != nil {
		return nil, err
	}

	symbolSnapshot := s.symbolTable.Snapshot()
	constantBase := s.constantStore.Len()
	comp := NewWithMacros(s.symbolTable, s.constantStore, s.macros, s.macroMetadata, s.macroEvaluator)
	comp.SetSourceFile(file.Path)
	comp.SetSourceLineProvider(sourceLine)
	comp.SetSourceContentProvider(sourceContent)
	comp.SetAllowPrivateBuiltins(s.allowPrivate)
	if s.optConfig != nil {
		comp.SetOptimizationConfig(s.optConfig)
	}
	bytecode, err := emit.Program(prog, emit.Config[*Bytecode]{
		Backend: newEmitBackend(comp),
	})
	if err != nil {
		s.symbolTable.Restore(symbolSnapshot)
		s.constantStore.Truncate(constantBase)
		return nil, err
	}
	if scoped {
		bytecode, err = scopedBytecode(bytecode, constantBase)
		if err != nil {
			s.symbolTable.Restore(symbolSnapshot)
			s.constantStore.Truncate(constantBase)
			return nil, err
		}
	}

	s.macros = comp.Macros()
	s.macroMetadata = comp.MacroMetadata()
	return bytecode, nil
}

func (s *Session) compileSyntaxNodesForRuntime(nodes []*form.Node) (*Bytecode, error) {
	file, err := sexpr.LowerFile(nodes)
	if err != nil {
		return nil, err
	}
	return s.compileFile(file, false)
}

func sourceProvidersForText(path, src string, fallbackLine func(file string, line int) (string, bool), fallbackContent func(file string) (string, bool)) (func(file string, line int) (string, bool), func(file string) (string, bool)) {
	lineProvider := fallbackLine
	if lineProvider == nil {
		lines := strings.Split(src, "\n")
		lineProvider = func(file string, line int) (string, bool) {
			if file != path || line <= 0 || line > len(lines) {
				return "", false
			}
			return lines[line-1], true
		}
	}

	contentProvider := fallbackContent
	if contentProvider == nil {
		contentProvider = func(file string) (string, bool) {
			if file != path {
				return "", false
			}
			return src, true
		}
	}

	return lineProvider, contentProvider
}

func (s *Session) Macros() map[string]*Closure {
	return cloneClosureMap(s.macros)
}

func (s *Session) MacroMetadata() map[string]*coll.Map {
	return cloneMetadataMap(s.macroMetadata)
}

func scopedBytecode(bytecode *Bytecode, constantBase int) (*Bytecode, error) {
	if bytecode == nil {
		return nil, nil
	}
	if constantBase < 0 || constantBase > len(bytecode.Constants) {
		return nil, fmt.Errorf("invalid constant base %d", constantBase)
	}

	instructions, err := rebaseInstructions(bytecode.Instructions, constantBase)
	if err != nil {
		return nil, err
	}
	constants, err := rebaseConstants(bytecode.Constants[constantBase:], constantBase)
	if err != nil {
		return nil, err
	}

	return &Bytecode{
		Instructions: instructions,
		Constants:    constants,
		BuiltinNames: append([]string(nil), bytecode.BuiltinNames...),
		Lines:        append([]int(nil), bytecode.Lines...),
		Columns:      append([]int(nil), bytecode.Columns...),
		SourceFile:   bytecode.SourceFile,
		Namespace:    bytecode.Namespace,
		NumLocals:    bytecode.NumLocals,
	}, nil
}

func rebaseConstants(constants []types.Value, constantBase int) ([]types.Value, error) {
	rebased := make([]types.Value, 0, len(constants))
	for _, value := range constants {
		next, err := rebaseConstantValue(value, constantBase)
		if err != nil {
			return nil, err
		}
		rebased = append(rebased, next)
	}
	return rebased, nil
}

func rebaseConstantValue(value types.Value, constantBase int) (types.Value, error) {
	switch current := value.(type) {
	case *CompiledFunction:
		instructions, err := rebaseInstructions(current.Instructions, constantBase)
		if err != nil {
			return nil, err
		}
		clone := *current
		clone.Instructions = instructions
		clone.Lines = append([]int(nil), current.Lines...)
		clone.Columns = append([]int(nil), current.Columns...)
		clone.Arities = append([]ArityInfo(nil), current.Arities...)
		return &clone, nil
	default:
		return value, nil
	}
}

func rebaseInstructions(instructions opcode.Instructions, constantBase int) (opcode.Instructions, error) {
	rebased := append(opcode.Instructions(nil), instructions...)
	for offset := 0; offset < len(rebased); {
		op := opcode.Opcode(rebased[offset])
		def, err := opcode.Lookup(op)
		if err != nil {
			return nil, err
		}
		operands, read := opcode.ReadOperands(def, rebased[offset+1:])
		switch op {
		case opcode.OpConst, opcode.OpClosure, opcode.OpImport:
			operands[0], err = rebaseConstantOperand(operands[0], constantBase)
		case opcode.OpDot, opcode.OpNew:
			operands[0], err = rebaseConstantOperand(operands[0], constantBase)
		case opcode.OpDefVar:
			if operands[1] > 0 {
				operands[1], err = rebaseConstantOperand(operands[1], constantBase)
			}
		}
		if err != nil {
			return nil, err
		}
		rewritten := opcode.EncodeInstruction(op, operands...)
		copy(rebased[offset:offset+len(rewritten)], rewritten)
		offset += 1 + read
	}
	return rebased, nil
}

func rebaseConstantOperand(index, constantBase int) (int, error) {
	rebased := index - constantBase
	if rebased < 0 {
		return 0, fmt.Errorf("bytecode references prior-session constant %d before base %d", index, constantBase)
	}
	return rebased, nil
}
