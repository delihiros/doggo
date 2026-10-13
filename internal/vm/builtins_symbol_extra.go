package vm

import (
	"fmt"
	"sync/atomic"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

var gensymCounter uint64

var builtinsSymbolExtra = []BuiltinDefinition{
	{
		Name: "gensym",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if len(args) > 1 {
				return newArgumentError("gensym expects 0 or 1 arguments, got %d", len(args))
			}

			prefix := "G__"
			if len(args) == 1 {
				switch value := args[0].(type) {
				case atom.String:
					prefix = string(value)
				case types.Symbol:
					prefix = string(value)
				case *types.SymbolWithMeta:
					prefix = value.Name
				case *types.ResolvedSymbol:
					prefix = value.Name
				default:
					return newArgumentError("gensym expects a string or symbol prefix, got %s", args[0].Type())
				}
			}

			id := atomic.AddUint64(&gensymCounter, 1)
			return types.NewSymbol(fmt.Sprintf("%s%d", prefix, id))
		}},
	},
}
