package compiler

import (
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestRuntimeMacroExpanderExpandsRuntimeMacroThroughCompilerEvaluator(t *testing.T) {
	closure := &Closure{}

	expander := runtimeMacroExpander{
		evaluator: func(fn *Closure, args []types.Value) (types.Value, error) {
			if fn != closure {
				t.Fatalf("unexpected closure: got %p want %p", fn, closure)
			}
			if len(args) != 2 {
				t.Fatalf("unexpected arg count: %d", len(args))
			}
			if sym, ok := args[0].(types.Symbol); !ok || string(sym) != "ok?" {
				t.Fatalf("unexpected first arg: %#v", args[0])
			}
			if keyword, ok := args[1].(atom.Keyword); !ok || string(keyword) != "done" {
				t.Fatalf("unexpected second arg: %#v", args[1])
			}
			items := append([]types.Value{types.Symbol("expanded")}, args...)
			return coll.NewList(items...), nil
		},
		lookup: func(sym sexpr.Symbol) (*Closure, bool) {
			if sym.Name != "runtime-macro" {
				return nil, false
			}
			return closure, true
		},
	}

	got, err := expander.ExpandRuntimeMacro(sexpr.NewSymbol("runtime-macro", span.Unknown(""), nil), []sexpr.Form{
		sexpr.NewSymbol("ok?", span.Unknown(""), nil),
		sexpr.NewKeyword("done", span.Unknown(""), nil),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.String() != "(expanded ok? :done)" {
		t.Fatalf("unexpected expansion: %s", got)
	}
}
