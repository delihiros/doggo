// builtins_exception.go は例外操作の組み込み関数を定義します。
//
// 例外は Clojure の try/catch/throw に相当する例外処理を提供します。
// 内部のエラー値はメッセージ、データ、原因、トレースを保持します。
//
// 提供する関数:
//   - 情報取得: ex-data, ex-message, ex-cause, ex->map
package vm

import (
	"context"
	"strings"
	"unicode/utf8"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var builtinsException = []BuiltinDefinition{
	{
		Name: "ex-info",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRange("ex-info", args, 2, 3); err != nil {
				return nil, invalidArgumentBuiltinError("doggo.core/ex-info", err.(*Error))
			}

			message, ok := args[0].(atom.String)
			if !ok {
				return nil, invalidArgumentBuiltinError("doggo.core/ex-info", newArgumentError("ex-info: first argument must be string, got %s", args[0].Type()))
			}

			data, ok := args[1].(types.Map)
			if !ok {
				return nil, invalidArgumentBuiltinError("doggo.core/ex-info", newArgumentError("ex-info: second argument must be map, got %s", args[1].Type()))
			}

			var cause types.Value
			if len(args) == 3 {
				cause = args[2]
				if !isNilArg(cause) && canonicalExceptionDataFromValue(cause) == nil {
					return nil, invalidArgumentBuiltinError("doggo.core/ex-info", newArgumentError("ex-info: third argument must be an error or nil, got %s", cause.Type()))
				}
			}

			exc := newExceptionInfo(string(message), data, cause)
			if vm := GetVM(ctx); vm != nil {
				trace := vm.captureStackTrace()
				if len(trace) > 0 {
					exc.StackTrace = formatExceptionStackTrace(trace)
				}
				refreshExceptionCanonicalData(exc, trace)
			}
			return exc, nil
		}},
	},
	{
		Name: "ex-data",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("ex-data", args, 1); err != nil {
				return err
			}

			data := canonicalExceptionDataFromValue(args[0])
			if data == nil {
				return atom.NilValue
			}
			return canonicalExceptionPayloadDisplayValue(data)
		}},
	},
	{
		Name: "ex-message",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("ex-message", args, 1); err != nil {
				return err
			}
			data := canonicalExceptionDataFromValue(args[0])
			if data == nil {
				return atom.NilValue
			}
			return atom.NewString(data.Message)
		}},
	},
	{
		Name: "ex-cause",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("ex-cause", args, 1); err != nil {
				return err
			}
			data := canonicalExceptionDataFromValue(args[0])
			if data == nil || data.Cause == nil || isNilThrownExceptionData(data.Cause) {
				return atom.NilValue
			}
			return canonicalCauseValue(data.Cause)
		}},
	},
	{
		Name: "ex->map",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("ex->map", args, 1); err != nil {
				return err
			}
			data := canonicalExceptionDataFromValue(args[0])
			if data == nil {
				return atom.NilValue
			}
			return exceptionMapValue(data)
		}},
	},
}

func newExceptionInfo(message string, data types.Map, cause types.Value) *atom.Exception {
	return atom.NewExceptionInfoWithPayload(message, doggoMapToCanonicalData(data), data, cause)
}

func invalidArgumentVMError(function, message string) error {
	data := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("kind"), atom.NewKeyword("invalid-argument"),
		atom.NewKeyword("function"), types.Symbol(function),
	})
	return &VMError{Value: newExceptionInfo(message, data, nil)}
}

func argumentVMError(function, argument, expected string, actual types.Value, message string) error {
	if !utf8.ValidString(message) {
		message = strings.ToValidUTF8(message, "\uFFFD")
	}
	actual = safeArgumentActual(actual)
	data := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("kind"), atom.NewKeyword("invalid-argument"),
		atom.NewKeyword("function"), types.Symbol(function),
		atom.NewKeyword("argument"), atom.NewKeyword(argument),
		atom.NewKeyword("expected"), atom.NewString(expected),
		atom.NewKeyword("actual"), actual,
	})
	return &VMError{Value: newExceptionInfo(message, data, nil)}
}

func safeArgumentActual(value types.Value) types.Value {
	budget := 256
	validUTF8 := true
	// Keep scanning after invalid bytes so unsafe or unbounded values take priority.
	if !plainArgumentActualWithin(value, &budget, &validUTF8) {
		return atom.NewString("<unsafe value>")
	}
	if !validUTF8 {
		return atom.NewString("<invalid UTF-8>")
	}
	if value == nil {
		return atom.NilValue
	}
	return value
}

func plainArgumentActualWithin(value types.Value, budget *int, validUTF8 *bool) bool {
	if *budget == 0 {
		return false
	}
	*budget--
	switch value := value.(type) {
	case nil, types.Nil, atom.Boolean, atom.Character, coll.RangeInt:
		return true
	case atom.String:
		*validUTF8 = *validUTF8 && utf8.ValidString(string(value))
		return true
	case atom.Keyword:
		*validUTF8 = *validUTF8 && utf8.ValidString(string(value))
		return true
	case types.Symbol:
		*validUTF8 = *validUTF8 && utf8.ValidString(string(value))
		return true
	case *atom.Number:
		return value != nil
	case *atom.UUID, *atom.Instant:
		return true
	case *coll.Map:
		if value == nil {
			return true
		}
		if !plainArgumentActualWithin(value.Metadata, budget, validUTF8) || !plainArgumentValues(value.Order, budget, validUTF8) {
			return false
		}
		for _, bucket := range value.Buckets {
			for _, entry := range bucket {
				if entry == nil || !plainArgumentActualWithin(entry.Key, budget, validUTF8) || !plainArgumentActualWithin(entry.Value, budget, validUTF8) {
					return false
				}
			}
		}
		return true
	case *coll.Vector:
		return value == nil || plainArgumentActualWithin(value.Metadata, budget, validUTF8) && plainArgumentValues(value.Items, budget, validUTF8)
	case *coll.List:
		return value == nil || plainArgumentActualWithin(value.Metadata, budget, validUTF8) && plainArgumentValues(value.Items, budget, validUTF8)
	case *coll.Set:
		if value == nil {
			return true
		}
		if !plainArgumentActualWithin(value.Metadata, budget, validUTF8) || !plainArgumentValues(value.Order, budget, validUTF8) {
			return false
		}
		for _, bucket := range value.Buckets {
			if !plainArgumentValues(bucket, budget, validUTF8) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func plainArgumentValues(values []types.Value, budget *int, validUTF8 *bool) bool {
	for _, value := range values {
		if !plainArgumentActualWithin(value, budget, validUTF8) {
			return false
		}
	}
	return true
}

func invalidArgumentBuiltinError(name string, builtinErr *Error) error {
	pairs := []types.Value{
		atom.NewKeyword("kind"), atom.NewKeyword("invalid-argument"),
		atom.NewKeyword("function"), types.Symbol(name),
	}
	if builtinErr.Expected != "" {
		pairs = append(pairs, atom.NewKeyword("expected"), atom.NewString(builtinErr.Expected))
	}
	if builtinErr.Got != "" {
		pairs = append(pairs, atom.NewKeyword("actual"), atom.NewString(builtinErr.Got))
	}
	return &VMError{Value: newExceptionInfo(builtinErr.Message, coll.NewMapFromPairs(pairs), nil)}
}

func runtimeBuiltinError(name, message string) error {
	return runtimeVMError(name, message)
}

func runtimeVMError(function, message string) error {
	data := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("kind"), atom.NewKeyword("runtime-failure"),
		atom.NewKeyword("function"), types.Symbol(function),
	})
	return &VMError{Value: newExceptionInfo(message, data, nil)}
}

func exceptionMapValue(data *doggoerr.ExceptionData) types.Value {
	if data == nil {
		return atom.NilValue
	}
	cause := types.Value(atom.NilValue)
	if data.Cause != nil && !isNilThrownExceptionData(data.Cause) {
		cause = canonicalCauseValue(data.Cause)
	}
	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("message"), atom.NewString(data.Message),
		atom.NewKeyword("data"), canonicalExceptionPayloadDisplayValue(data),
		atom.NewKeyword("cause"), cause,
		atom.NewKeyword("trace"), formattedExceptionTraceValue(data.Trace),
	})
}

func formattedExceptionTraceValue(trace []doggoerr.ExceptionFrame) types.Value {
	formatted := formatExceptionFrames(trace)
	items := make([]types.Value, len(formatted))
	for i, frame := range formatted {
		items[i] = atom.NewString(frame)
	}
	return coll.NewVector(items...)
}

func invalidThrowValue(value types.Value) *atom.Exception {
	actual := "nil"
	if value != nil {
		actual = value.Type()
	}
	data := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("kind"), atom.NewKeyword("invalid-argument"),
		atom.NewKeyword("function"), types.Symbol("doggo.core/throw"),
		atom.NewKeyword("argument"), atom.NewKeyword("value"),
		atom.NewKeyword("expected"), atom.NewString("error"),
		atom.NewKeyword("actual"), atom.NewString(actual),
	})
	return newExceptionInfo("throw expects a canonical error value", data, nil)
}

func canonicalDataPayloadValue(data map[string]any) types.Value {
	if len(data) == 0 {
		return coll.NewMap(nil)
	}
	entries := make(map[types.Value]types.Value, len(data))
	for key, value := range data {
		entries[atom.NewKeyword(key)] = canonicalAnyValue(value)
	}
	return coll.NewMap(entries)
}

func canonicalExceptionDataFromValue(value types.Value) *doggoerr.ExceptionData {
	return atom.CanonicalErrorData(value)
}

func canonicalExceptionDataValue(data *doggoerr.ExceptionData) types.Value {
	if data == nil {
		return atom.NilValue
	}

	payload := canonicalExceptionPayloadDisplayValue(data)

	pairs := []types.Value{
		atom.NewKeyword("type"), atom.NewString(data.Type),
		atom.NewKeyword("kind"), atom.NewString(data.Kind),
		atom.NewKeyword("phase"), atom.NewString(data.Phase),
		atom.NewKeyword("message"), atom.NewString(data.Message),
		atom.NewKeyword("data"), payload,
		atom.NewKeyword("context"), canonicalAnyValue(data.Context),
		atom.NewKeyword("at"), canonicalPositionValue(data.At),
		atom.NewKeyword("trace"), canonicalTraceValue(data.Trace),
	}
	if data.Cause != nil && !isNilThrownExceptionData(data.Cause) {
		pairs = append(pairs, atom.NewKeyword("cause"), canonicalExceptionDataValue(data.Cause))
	} else {
		pairs = append(pairs, atom.NewKeyword("cause"), atom.NilValue)
	}
	return coll.NewMapFromPairs(pairs)
}

func canonicalExceptionMapValue(data *doggoerr.ExceptionData) types.Value {
	if data == nil {
		return atom.NilValue
	}

	payload := canonicalExceptionPayloadDisplayValue(data)

	pairs := []types.Value{
		atom.NewKeyword("type"), atom.NewString(data.Type),
		atom.NewKeyword("kind"), atom.NewString(data.Kind),
		atom.NewKeyword("phase"), atom.NewString(data.Phase),
		atom.NewKeyword("message"), atom.NewString(data.Message),
		atom.NewKeyword("data"), payload,
		atom.NewKeyword("context"), canonicalAnyValue(data.Context),
		atom.NewKeyword("at"), canonicalPositionValue(data.At),
		atom.NewKeyword("cause"), rootCauseValue(data),
		atom.NewKeyword("trace"), canonicalTraceValue(data.Trace),
		atom.NewKeyword("via"), exceptionViaValue(data),
	}
	return coll.NewMapFromPairs(pairs)
}

func exceptionViaValue(data *doggoerr.ExceptionData) types.Value {
	if data == nil {
		return coll.NewVector()
	}

	items := make([]types.Value, 0, 4)
	for current := data; current != nil; current = nextExceptionCause(current) {
		items = append(items, exceptionViaEntryValue(current))
	}
	return coll.NewVector(items...)
}

func exceptionViaEntryValue(data *doggoerr.ExceptionData) types.Value {
	if data == nil {
		return atom.NilValue
	}
	payload := canonicalExceptionPayloadDisplayValue(data)
	pairs := []types.Value{
		atom.NewKeyword("type"), atom.NewString(data.Type),
		atom.NewKeyword("kind"), atom.NewString(data.Kind),
		atom.NewKeyword("phase"), atom.NewString(data.Phase),
		atom.NewKeyword("message"), atom.NewString(data.Message),
		atom.NewKeyword("data"), payload,
		atom.NewKeyword("context"), canonicalAnyValue(data.Context),
		atom.NewKeyword("at"), canonicalPositionValue(data.At),
	}
	return coll.NewMapFromPairs(pairs)
}

func canonicalExceptionPayloadValue(data *doggoerr.ExceptionData) (types.Value, bool) {
	if data == nil || data.Type != "ExceptionInfo" {
		return nil, false
	}
	switch payload := data.Original.(type) {
	case atom.ExceptionInfoPayload:
		if payload.Value == nil {
			return coll.NewMap(nil), true
		}
		return payload.Value, true
	default:
		return nil, false
	}
}

func canonicalExceptionPayloadDisplayValue(data *doggoerr.ExceptionData) types.Value {
	if payload, ok := canonicalExceptionPayloadValue(data); ok {
		return payload
	}
	return canonicalDataPayloadValue(data.Data)
}

func canonicalTraceValue(trace []doggoerr.ExceptionFrame) types.Value {
	if len(trace) == 0 {
		return coll.NewVector()
	}
	items := make([]types.Value, len(trace))
	for i, frame := range trace {
		items[i] = canonicalFrameValue(frame)
	}
	return coll.NewVector(items...)
}

func canonicalFrameValue(frame doggoerr.ExceptionFrame) types.Value {
	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("file"), atom.NewString(frame.File),
		atom.NewKeyword("line"), atom.NewNumberInt(int64(frame.Line)),
		atom.NewKeyword("column"), atom.NewNumberInt(int64(frame.Column)),
		atom.NewKeyword("name"), atom.NewString(frame.Name),
	})
}

func canonicalPositionValue(pos doggoerr.Position) types.Value {
	if pos.IsZero() {
		return atom.NilValue
	}
	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("file"), atom.NewString(pos.File),
		atom.NewKeyword("line"), atom.NewNumberInt(int64(pos.Line)),
		atom.NewKeyword("column"), atom.NewNumberInt(int64(pos.Column)),
	})
}

func canonicalAnyValue(value any) types.Value {
	switch v := value.(type) {
	case nil:
		return atom.NilValue
	case string:
		return atom.NewString(v)
	case bool:
		return atom.NewBoolean(v)
	case int:
		return atom.NewNumberInt(int64(v))
	case int8:
		return atom.NewNumberInt(int64(v))
	case int16:
		return atom.NewNumberInt(int64(v))
	case int32:
		return atom.NewNumberInt(int64(v))
	case int64:
		return atom.NewNumberInt(v)
	case float32:
		return atom.NewNumberFloat(float64(v))
	case float64:
		return atom.NewNumberFloat(v)
	case doggoerr.Position:
		return canonicalPositionValue(v)
	case doggoerr.ExceptionFrame:
		return canonicalFrameValue(v)
	case []doggoerr.ExceptionFrame:
		return canonicalTraceValue(v)
	case *doggoerr.ExceptionData:
		return canonicalExceptionDataValue(v)
	case *atom.Exception:
		if v == nil {
			return atom.NilValue
		}
		return canonicalExceptionDataValue(v.ExceptionData())
	case map[string]any:
		return canonicalDataPayloadValue(v)
	case []any:
		items := make([]types.Value, len(v))
		for i, item := range v {
			items[i] = canonicalAnyValue(item)
		}
		return coll.NewVector(items...)
	case types.Value:
		return v
	default:
		return atom.NewGoObject(v)
	}
}

func doggoMapToCanonicalData(value types.Map) map[string]any {
	if value == nil || value.Count() == 0 {
		return nil
	}

	data := make(map[string]any, value.Count())
	keys := value.Keys()
	if keys == nil {
		return nil
	}
	seq, _ := keys.Seq().(types.Seq)
	for seq != nil {
		key := seq.First()
		if key == nil {
			seq = seq.Next()
			continue
		}
		keyString := canonicalMapKeyString(key)
		data[keyString] = canonicalAnyFromDoggoValue(value.Get(key))
		seq = seq.Next()
	}
	return data
}

func canonicalAnyFromDoggoValue(value types.Value) any {
	switch v := value.(type) {
	case nil:
		return nil
	case types.Nil:
		return nil
	case atom.Boolean:
		return bool(v)
	case atom.String:
		return string(v)
	case atom.Keyword:
		return v
	case types.Number:
		if intValue, ok := v.(types.Int64Valuer); ok {
			if n, ok := intValue.Int64Value(); ok {
				return n
			}
		}
		return v.ToFloat64()
	case *coll.Map:
		return doggoMapToCanonicalData(v)
	case *coll.Vector:
		return canonicalSliceFromDoggoValues(v.Items)
	case *coll.List:
		return canonicalSliceFromDoggoValues(v.Items)
	case *coll.Set:
		return canonicalSliceFromDoggoValues(v.AllElements())
	case *atom.Exception:
		if v == nil {
			return nil
		}
		return canonicalExceptionDataValue(v.ExceptionData())
	default:
		if name, ok := types.SymbolName(value); ok {
			return types.Symbol(name)
		}
		return value
	}
}

func canonicalSliceFromDoggoValues(values []types.Value) []any {
	if len(values) == 0 {
		return nil
	}
	items := make([]any, len(values))
	for i, value := range values {
		items[i] = canonicalAnyFromDoggoValue(value)
	}
	return items
}

func canonicalMapKeyString(value types.Value) string {
	switch v := value.(type) {
	case atom.String:
		return string(v)
	case atom.Keyword:
		return string(v)
	default:
		if name, ok := types.SymbolName(value); ok {
			return name
		}
		return value.String()
	}
}

func rootCauseValue(data *doggoerr.ExceptionData) types.Value {
	if data == nil || data.Cause == nil {
		return atom.NilValue
	}
	current := data.Cause
	for next := nextExceptionCause(current); next != nil; next = nextExceptionCause(current) {
		current = next
	}
	return atom.NewString(current.Message)
}

func nextExceptionCause(data *doggoerr.ExceptionData) *doggoerr.ExceptionData {
	if data == nil || data.Cause == nil || isNilThrownExceptionData(data.Cause) {
		return nil
	}
	return data.Cause
}

func isNilThrownExceptionData(data *doggoerr.ExceptionData) bool {
	if data == nil {
		return false
	}
	return data.Type == "nil" && data.Kind == "thrown value"
}
