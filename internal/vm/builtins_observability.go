package vm

import (
	"context"
	"math"
	"time"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var observabilityBuiltinDefs = []BuiltinDefinition{
	{
		Name: "log!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("log!", args, 2, 3); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, doggoerr.EvalErrorf("log!", "vm context is not available")
			}
			state := vm.getOrCreateObservabilityState()

			level, err := observabilityString(args[0])
			if err != nil {
				return nil, NewTypeErrorAt("log!", 0, "string/symbol/keyword", args[0].Type())
			}
			message, err := observabilityString(args[1])
			if err != nil {
				return nil, NewTypeErrorAt("log!", 1, "string/symbol/keyword", args[1].Type())
			}

			var fields map[string]any
			if len(args) == 3 {
				contextMap, ok := args[2].(*coll.Map)
				if !ok {
					return nil, NewTypeErrorAt("log!", 2, "map", args[2].Type())
				}
				fields = observabilityFields(contextMap)
			}

			if err := state.Log(ctx, level, message, fields); err != nil {
				return nil, doggoerr.EvalErrorf("log!", "%v", err)
			}
			return atom.NilValue, nil
		}},
	},
	{
		Name: "correlation-id",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("correlation-id", args, 0); err != nil {
				return nil, err
			}
			correlationID := correlationIDFromContext(ctx)
			if correlationID == "" {
				return atom.NilValue, nil
			}
			return atom.NewString(correlationID), nil
		}},
	},
	{
		Name: "trace-id",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("trace-id", args, 0); err != nil {
				return nil, err
			}

			if traceCtx, ok := traceContextFromContext(ctx); ok && traceCtx.TraceID != "" {
				return atom.NewString(traceCtx.TraceID), nil
			}
			return atom.NilValue, nil
		}},
	},
	{
		Name: "with-span",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("with-span", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, doggoerr.EvalErrorf("with-span", "vm context is not available")
			}
			state := vm.getOrCreateObservabilityState()

			spanName, err := observabilityString(args[0])
			if err != nil {
				return nil, NewTypeErrorAt("with-span", 0, "string/symbol/keyword", args[0].Type())
			}

			traceCtx, ok := traceContextFromContext(ctx)
			if !ok || traceCtx.TraceID == "" {
				traceCtx = TraceContext{TraceID: state.NewTraceID()}
			}
			spanCtx := TraceContext{
				TraceID: traceCtx.TraceID,
				SpanID:  state.NewSpanID(),
				Span:    spanName,
			}
			runCtx := withTraceContext(ctx, spanCtx)

			start := time.Now()
			result, callErr := vm.Apply(runCtx, args[1], nil)
			durationMs := float64(time.Since(start)) / float64(time.Millisecond)

			fields := map[string]any{
				"event":       "span",
				"duration_ms": durationMs,
				"span":        spanName,
			}
			if callErr != nil {
				fields["status"] = "error"
				fields["error"] = callErr.Error()
				_ = state.Log(runCtx, "error", "span failed", fields)
				return nil, callErr
			}
			fields["status"] = "ok"
			_ = state.Log(runCtx, "info", "span completed", fields)
			return result, nil
		}},
	},
	{
		Name: "metric-inc!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("metric-inc!", args, 1, 2); err != nil {
				return nil, err
			}
			vm := GetVM(ctx)
			if vm == nil {
				return nil, doggoerr.EvalErrorf("metric-inc!", "vm context is not available")
			}

			name, err := observabilityString(args[0])
			if err != nil {
				return nil, NewTypeErrorAt("metric-inc!", 0, "string/symbol/keyword", args[0].Type())
			}
			delta := 1.0
			if len(args) == 2 {
				n, ok := toNumber(args[1])
				if !ok {
					return nil, NewTypeErrorAt("metric-inc!", 1, "number", args[1].Type())
				}
				delta = n
			}
			vm.getOrCreateObservabilityState().IncCounter(name, delta)
			return atom.NilValue, nil
		}},
	},
	{
		Name:    "metric-set-gauge!",
		Builtin: &Builtin{CtxFn: metricValueCtxFn("metric-set-gauge!", (*ObservabilityState).SetGauge)},
	},
	{
		Name:    "metric-observe!",
		Builtin: &Builtin{CtxFn: metricValueCtxFn("metric-observe!", (*ObservabilityState).ObserveHistogram)},
	},
	{
		Name: "metrics-snapshot",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("metrics-snapshot", args, 0); err != nil {
				return nil, err
			}
			vm := GetVM(ctx)
			if vm == nil {
				return nil, doggoerr.EvalErrorf("metrics-snapshot", "vm context is not available")
			}
			snapshot := vm.getOrCreateObservabilityState().Snapshot()
			return snapshotToValue(snapshot), nil
		}},
	},
}

func metricValueCtxFn(fnName string, update func(*ObservabilityState, string, float64)) func(context.Context, ...types.Value) (types.Value, error) {
	return func(ctx context.Context, args ...types.Value) (types.Value, error) {
		if err := CheckArgCountCtx(fnName, args, 2); err != nil {
			return nil, err
		}
		vm := GetVM(ctx)
		if vm == nil {
			return nil, doggoerr.EvalErrorf(fnName, "vm context is not available")
		}

		name, err := observabilityString(args[0])
		if err != nil {
			return nil, NewTypeErrorAt(fnName, 0, "string/symbol/keyword", args[0].Type())
		}
		value, ok := toNumber(args[1])
		if !ok {
			return nil, NewTypeErrorAt(fnName, 1, "number", args[1].Type())
		}
		update(vm.getOrCreateObservabilityState(), name, value)
		return atom.NilValue, nil
	}
}

func observabilityString(v types.Value) (string, error) {
	switch value := v.(type) {
	case atom.String:
		return string(value), nil
	case atom.Keyword:
		return string(value), nil
	case types.Symbol:
		return string(value), nil
	default:
		return "", doggoerr.TypeError("observability", "string/symbol/keyword", v.Type())
	}
}

func observabilityFields(m *coll.Map) map[string]any {
	fields := make(map[string]any, m.Count())
	m.Range(func(key, value types.Value) bool {
		fields[observabilityFieldKey(key)] = observabilityValue(value)
		return true
	})
	return fields
}

func observabilityFieldKey(v types.Value) string {
	switch key := v.(type) {
	case atom.Keyword:
		return normalizeLogFieldKey(string(key))
	case atom.String:
		return normalizeLogFieldKey(string(key))
	case types.Symbol:
		return normalizeLogFieldKey(string(key))
	default:
		return normalizeLogFieldKey(key.String())
	}
}

func observabilityValue(v types.Value) any {
	if v == nil {
		return nil
	}

	switch value := v.(type) {
	case types.Nil:
		return nil
	case atom.String:
		return string(value)
	case atom.Boolean:
		return bool(value)
	case atom.Keyword:
		return ":" + string(value)
	case types.Symbol:
		return string(value)
	case *atom.Number:
		if value.IsInt() && value.IntVal != nil {
			return *value.IntVal
		}
		return value.ToFloat64()
	case types.Number:
		return value.ToFloat64()
	case *coll.Vector:
		items := make([]any, 0, len(value.Items))
		for _, item := range value.Items {
			items = append(items, observabilityValue(item))
		}
		return items
	case *coll.List:
		items := make([]any, 0, len(value.Items))
		for _, item := range value.Items {
			items = append(items, observabilityValue(item))
		}
		return items
	case *coll.Set:
		elements := value.AllElements()
		items := make([]any, 0, len(elements))
		for _, item := range elements {
			items = append(items, observabilityValue(item))
		}
		return items
	case *coll.Map:
		entries := make(map[string]any, value.Count())
		value.Range(func(key, mapValue types.Value) bool {
			entries[observabilityFieldKey(key)] = observabilityValue(mapValue)
			return true
		})
		return entries
	default:
		if displayable, ok := value.(types.Displayable); ok {
			return displayable.Display()
		}
		return value.String()
	}
}

func snapshotToValue(snapshot MetricsSnapshot) types.Value {
	counterEntries := make(map[types.Value]types.Value, len(snapshot.Counters))
	for name, value := range snapshot.Counters {
		counterEntries[atom.NewString(name)] = floatToDoggoNumber(value)
	}

	gaugeEntries := make(map[types.Value]types.Value, len(snapshot.Gauges))
	for name, value := range snapshot.Gauges {
		gaugeEntries[atom.NewString(name)] = floatToDoggoNumber(value)
	}

	histogramEntries := make(map[types.Value]types.Value, len(snapshot.Histograms))
	for name, stats := range snapshot.Histograms {
		histogramEntries[atom.NewString(name)] = coll.NewMap(map[types.Value]types.Value{
			atom.NewKeyword("count"): atom.NewNumberInt(stats.Count),
			atom.NewKeyword("sum"):   floatToDoggoNumber(stats.Sum),
			atom.NewKeyword("min"):   floatToDoggoNumber(stats.Min),
			atom.NewKeyword("max"):   floatToDoggoNumber(stats.Max),
		})
	}

	return coll.NewMap(map[types.Value]types.Value{
		atom.NewKeyword("counters"):   coll.NewMap(counterEntries),
		atom.NewKeyword("gauges"):     coll.NewMap(gaugeEntries),
		atom.NewKeyword("histograms"): coll.NewMap(histogramEntries),
	})
}

func floatToDoggoNumber(v float64) types.Value {
	if math.Trunc(v) == v && v >= math.MinInt64 && v <= math.MaxInt64 {
		return atom.NewNumberInt(int64(v))
	}
	return atom.NewNumberFloat(v)
}
