package vm

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

const (
	uuidParseFunction     = "doggo.uuid/parse"
	uuidRandomFunction    = "doggo.uuid/random"
	uuidPredicateFunction = "doggo.uuid/uuid?"
)

var builtinsUUID = []BuiltinDefinition{
	{
		Name:       "uuid-parse*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("uuid-parse*", args, 1); err != nil {
				return nil, err
			}
			text, ok := args[0].(atom.String)
			if !ok || !utf8.ValidString(string(text)) {
				return nil, argumentVMError(uuidParseFunction, "value", "valid UTF-8 UUID string", args[0], uuidParseFunction+": value must be a valid UTF-8 UUID string")
			}
			value, err := atom.NewUUID(string(text))
			if err != nil {
				return nil, argumentVMError(uuidParseFunction, "value", "canonical UUID string", args[0], uuidParseFunction+": invalid UUID")
			}
			return value, nil
		}},
	},
	{
		Name:       "uuid-random*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("uuid-random*", args, 0); err != nil {
				return nil, err
			}
			return uuidRandomFrom(rand.Reader)
		}},
	},
	{
		Name:       "uuid-predicate*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("uuid-predicate*", args, 1); err != nil {
				return nil, err
			}
			_, ok := args[0].(*atom.UUID)
			return atom.Boolean(ok), nil
		}},
	},
}

func uuidRandomFrom(reader io.Reader) (*atom.UUID, error) {
	bytes := make([]byte, 16)
	if _, err := io.ReadFull(reader, bytes); err != nil {
		return nil, runtimeVMError(uuidRandomFunction, uuidRandomFunction+": secure random source failed")
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	value, err := atom.NewUUID(fmt.Sprintf("%x-%x-%x-%x-%x", bytes[:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:]))
	if err != nil {
		return nil, runtimeVMError(uuidRandomFunction, uuidRandomFunction+": failed to format UUID")
	}
	return value, nil
}
