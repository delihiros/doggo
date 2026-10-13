package emit_test

import (
	"fmt"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/hir"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/syntax/span"
)

type recordingBackend struct {
	events    []string
	finished  int
	namespace string
	aliases   map[string]string
}

func (b *recordingBackend) ApplyScope(namespace string, aliases map[string]string) error {
	b.events = append(b.events, fmt.Sprintf("scope:%s", namespace))
	b.namespace = namespace
	b.aliases = make(map[string]string, len(aliases))
	for alias, target := range aliases {
		b.aliases[alias] = target
	}
	return nil
}

func (b *recordingBackend) EmitNode(node hir.Node) error {
	b.events = append(b.events, fmt.Sprintf("form:%T", node))
	return nil
}

func (b *recordingBackend) Finish() ([]string, error) {
	b.finished++
	b.events = append(b.events, "finish")
	return append([]string(nil), b.events...), nil
}

func TestProgramAppliesScopeBeforeEmittingForms(t *testing.T) {
	backend := &recordingBackend{}
	prog := &hir.Program{
		Namespace: "demo.core",
		Aliases: map[string]string{
			"d": "demo.core",
		},
		Forms: []hir.Node{
			hir.FormNode{Value: sexpr.NewSymbol("x", span.Unknown("<test>"), nil)},
			hir.FormNode{Value: sexpr.NewSymbol("y", span.Unknown("<test>"), nil)},
		},
	}

	events, err := emit.Program(prog, emit.Config[[]string]{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"scope:demo.core", "form:hir.FormNode", "form:hir.FormNode", "finish"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if backend.namespace != "demo.core" {
		t.Fatalf("namespace = %q, want demo.core", backend.namespace)
	}
	if got := backend.aliases["d"]; got != "demo.core" {
		t.Fatalf("alias d = %q, want demo.core", got)
	}
}

func TestProgramSkipsNilFormsAndFinishesOnce(t *testing.T) {
	backend := &recordingBackend{}
	prog := &hir.Program{
		Forms: []hir.Node{
			nil,
			hir.FormNode{Value: sexpr.NewSymbol("x", span.Unknown("<test>"), nil)},
			nil,
		},
	}

	events, err := emit.Program(prog, emit.Config[[]string]{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"scope:", "form:hir.FormNode", "finish"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if backend.finished != 1 {
		t.Fatalf("finish count = %d, want 1", backend.finished)
	}
}

func TestProgramIntegrationEmitsClosureAndConditionalBytecode(t *testing.T) {
	prog := mustResolveProgram(t, "(do (fn [] 1) (if true 1 2))")

	bc, err := emit.Program(prog, newCompilerConfig())
	if err != nil {
		t.Fatal(err)
	}

	compiledFns := 0
	for _, constant := range bc.Constants {
		if _, ok := constant.(*compiler.CompiledFunction); ok {
			compiledFns++
		}
	}
	if compiledFns == 0 {
		t.Fatal("expected nested compiled function constant")
	}

	jumpCount := 0
	for _, ins := range bc.Instructions {
		if opcode.Opcode(ins) == opcode.OpJumpNotTruthy {
			jumpCount++
		}
	}
	if jumpCount == 0 {
		t.Fatal("expected emitted jump instruction")
	}
}
