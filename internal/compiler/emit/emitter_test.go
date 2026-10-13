package emit_test

import (
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/hir"
	"doggo/internal/compiler/resolve"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/frontend"
	"doggo/internal/syntax/span"
)

func TestEmitterUsesProgramAliases(t *testing.T) {
	prog := &hir.Program{
		Namespace: "foo.bar",
		Aliases: map[string]string{
			"fb": "foo.bar",
		},
		Forms: []hir.Node{
			hir.FormNode{
				Value: sexpr.NewList([]sexpr.Form{
					sexpr.NewSymbol("def", span.Unknown("<test>"), nil),
					sexpr.NewSymbol("x", span.Unknown("<test>"), nil),
					sexpr.NewNumber("1", span.Unknown("<test>"), nil),
				}, span.Unknown("<test>"), nil),
			},
			hir.FormNode{
				Value: sexpr.NewSymbol("fb/x", span.Unknown("<test>"), nil),
			},
		},
	}

	bc, err := emit.Program(prog, newCompilerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(bc.Instructions) == 0 {
		t.Fatal("expected bytecode")
	}
	if bc.Namespace != "foo.bar" {
		t.Fatalf("unexpected namespace: %s", bc.Namespace)
	}
}

func newCompilerConfig() emit.Config[*compiler.Bytecode] {
	return emit.Config[*compiler.Bytecode]{
		Backend: compilerBackend{comp: compiler.New()},
	}
}

type compilerBackend struct {
	comp *compiler.Compiler
}

func (b compilerBackend) ApplyScope(namespace string, aliases map[string]string) error {
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

func (b compilerBackend) EmitNode(node hir.Node) error {
	if b.comp == nil || node == nil {
		return nil
	}
	return b.comp.CompileForm(node.Form())
}

func (b compilerBackend) Finish() (*compiler.Bytecode, error) {
	if b.comp == nil {
		return nil, nil
	}
	return b.comp.Bytecode(), nil
}

func mustResolveProgram(t *testing.T, source string) *hir.Program {
	t.Helper()

	reader, err := frontend.NewReader("<test>", source)
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}
	nodes, err := reader.ReadSyntaxAll()
	if err != nil {
		t.Fatalf("ReadSyntaxAll failed: %v", err)
	}

	file, err := sexpr.LowerFile(nodes)
	if err != nil {
		t.Fatalf("LowerFile failed: %v", err)
	}

	prog, err := resolve.File(resolve.Config{}, file)
	if err != nil {
		t.Fatalf("resolve.File failed: %v", err)
	}

	return prog
}
