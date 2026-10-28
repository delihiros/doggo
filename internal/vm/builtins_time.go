package vm

import (
	"context"
	"fmt"
	"math"
	stdtime "time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var timeBuiltinNow = stdtime.Now
var timeBuiltinSleep = func(ctx context.Context, duration stdtime.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if duration <= 0 {
		return nil
	}
	timer := stdtime.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

const (
	timeMaxInt64         = int64(^uint64(0) >> 1)
	timeMinInt64         = -timeMaxInt64 - 1
	timeMaxDurationMilli = timeMaxInt64 / int64(stdtime.Millisecond)
)

var (
	timeMaxInt64Float  = float64(timeMaxInt64)
	timeMinInt64Float  = float64(timeMinInt64)
	timeKeywordInstant = atom.NewKeyword("instant")
	timeKeywordEpoch   = atom.NewKeyword("epoch-millis")
	timeKeywordOffset  = atom.NewKeyword("offset-seconds")
)

func timeInvalidInstant(function, field string, got types.Value) error {
	return invalidArgumentVMError(function, fmt.Sprintf("doggo.time instant must have %s, got %v", field, got))
}

func timeInvalidArg(function, format string, args ...any) error {
	return invalidArgumentVMError(function, fmt.Sprintf("%s: %s", function, fmt.Sprintf(format, args...)))
}

func timeExactFloatArg(value float64, function string) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, timeInvalidArg(function, "expected finite milliseconds, got %v", value)
	}
	if math.Trunc(value) != value {
		return 0, timeInvalidArg(function, "expected integer milliseconds, got %v", value)
	}
	switch {
	case value == timeMaxInt64Float:
		return timeMaxInt64, nil
	case value == timeMinInt64Float:
		return timeMinInt64, nil
	case value > timeMaxInt64Float || value < timeMinInt64Float:
		return 0, timeInvalidArg(function, "milliseconds out of int64 range, got %v", value)
	default:
		return int64(value), nil
	}
}

func timeExactIntArg(arg types.Value, function string) (int64, error) {
	switch value := arg.(type) {
	case *atom.Number:
		if value.IntVal != nil {
			return *value.IntVal, nil
		}
		return timeExactFloatArg(value.ToFloat64(), function)
	case coll.RangeInt:
		return int64(value), nil
	case types.Number:
		return timeExactFloatArg(value.ToFloat64(), function)
	default:
		return 0, timeInvalidArg(function, "expected number, got %s", arg.Type())
	}
}

func timeInstantMapFromEpochMillis(epochMillis int64, offsetSeconds int64) types.Map {
	instantTime := stdtime.UnixMilli(epochMillis).UTC()
	return coll.NewMapFromPairs([]types.Value{
		timeKeywordInstant, atom.NewString(instantTime.Format(stdtime.RFC3339Nano)),
		timeKeywordEpoch, atom.NewNumberInt(epochMillis),
		timeKeywordOffset, atom.NewNumberInt(offsetSeconds),
	})
}

func timeValidateInstantArg(function string, arg types.Value) (types.Map, error) {
	if instant, ok := arg.(*atom.Instant); ok {
		return timeInstantMapFromEpochMillis(instant.EpochMillis(), 0), nil
	}

	value, ok := arg.(types.Map)
	if !ok {
		return nil, timeInvalidInstant(function, ":instant, :epoch-millis, and :offset-seconds", arg)
	}

	instantValue := value.Get(timeKeywordInstant)
	instantString, ok := instantValue.(atom.String)
	if !ok {
		return nil, timeInvalidInstant(function, ":instant", arg)
	}
	parsedInstant, _, parseErr := parseInstantTimestampValue(string(instantString))
	if parseErr != nil {
		return nil, timeInvalidInstant(function, ":instant", arg)
	}
	canonicalInstant := parsedInstant.UTC().Format(stdtime.RFC3339Nano)
	if canonicalInstant != string(instantString) {
		return nil, timeInvalidInstant(function, ":instant", arg)
	}

	epochValue := value.Get(timeKeywordEpoch)
	if epochValue == nil {
		return nil, timeInvalidInstant(function, ":epoch-millis", arg)
	}
	epochMillis, errValue := timeExactIntArg(epochValue, function)
	if errValue != nil {
		return nil, timeInvalidInstant(function, ":epoch-millis", arg)
	}
	// UnixMilli wraps outside the int64 range; compare the actual times instead.
	if !parsedInstant.UTC().Truncate(stdtime.Millisecond).Equal(stdtime.UnixMilli(epochMillis)) {
		return nil, timeInvalidInstant(function, ":epoch-millis", arg)
	}

	offsetValue := value.Get(timeKeywordOffset)
	if offsetValue == nil {
		return nil, timeInvalidInstant(function, ":offset-seconds", arg)
	}
	offsetSeconds, errValue := timeExactIntArg(offsetValue, function)
	if errValue != nil {
		return nil, timeInvalidInstant(function, ":offset-seconds", arg)
	}

	return value.
		Assoc(timeKeywordInstant, instantString).
		Assoc(timeKeywordEpoch, atom.NewNumberInt(epochMillis)).
		Assoc(timeKeywordOffset, atom.NewNumberInt(offsetSeconds)), nil
}

func timeFunctionArg(arg types.Value) (string, error) {
	function, ok := types.SymbolName(arg)
	if !ok || function == "" {
		return "", invalidArgumentVMError("doggo.time/operation", "doggo.time internal operation requires a function symbol")
	}
	return function, nil
}

var builtinsTime = []BuiltinDefinition{
	{
		Name:       "time-now*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("now", args, 0); err != nil {
				return err
			}
			return instantResultFromTime(timeBuiltinNow().UTC(), 0)
		}},
	},
	{
		Name:       "time-from-epoch-millis*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("from-epoch-millis", args, 2); err != nil {
				return nil, err
			}
			function, err := timeFunctionArg(args[0])
			if err != nil {
				return nil, err
			}
			value, errValue := timeExactIntArg(args[1], function)
			if errValue != nil {
				return nil, errValue
			}
			return instantResultFromTime(stdtime.UnixMilli(value).UTC(), 0), nil
		}},
	},
	{
		Name:       "time-instant*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("time-instant*", args, 2); err != nil {
				return nil, err
			}
			function, err := timeFunctionArg(args[0])
			if err != nil {
				return nil, err
			}
			value, errValue := timeValidateInstantArg(function, args[1])
			if errValue != nil {
				return nil, errValue
			}
			return value, nil
		}},
	},
	{
		Name:       "time-sleep*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("sleep", args, 2); err != nil {
				return nil, err
			}
			function, err := timeFunctionArg(args[0])
			if err != nil {
				return nil, err
			}
			value, errValue := timeExactIntArg(args[1], function)
			if errValue != nil {
				return nil, errValue
			}
			if value < 0 {
				return nil, timeInvalidArg(function, "expected non-negative milliseconds, got %d", value)
			}
			if value > timeMaxDurationMilli {
				return nil, timeInvalidArg(function, "milliseconds out of supported range, got %d", value)
			}
			if err := timeBuiltinSleep(ctx, stdtime.Duration(value)*stdtime.Millisecond); err != nil {
				return nil, RuntimeErrf("time-sleep*", "%v", err)
			}
			return atom.NilValue, nil
		}},
	},
}
