package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type observabilityContextKey string

const (
	correlationIDContextKey observabilityContextKey = "doggo-correlation-id"
	traceContextKey         observabilityContextKey = "doggo-trace-context"
)

// TraceContext は実行中のトレース情報を表します。
type TraceContext struct {
	TraceID string
	SpanID  string
	Span    string
}

// HistogramSnapshot はヒストグラムの集計結果です。
type HistogramSnapshot struct {
	Count int64
	Sum   float64
	Min   float64
	Max   float64
}

// MetricsSnapshot は現在のメトリクス値です。
type MetricsSnapshot struct {
	Counters   map[string]float64
	Gauges     map[string]float64
	Histograms map[string]HistogramSnapshot
}

type histogramState struct {
	Count int64
	Sum   float64
	Min   float64
	Max   float64
	init  bool
}

// ObservabilityState は log/metrics/trace の共有状態を保持します。
type ObservabilityState struct {
	mu         sync.RWMutex
	logWriter  io.Writer
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string]*histogramState
	idSeq      uint64
}

// NewObservabilityState は新しい observability 状態を作成します。
func NewObservabilityState() *ObservabilityState {
	return &ObservabilityState{
		logWriter:  os.Stderr,
		counters:   make(map[string]float64),
		gauges:     make(map[string]float64),
		histograms: make(map[string]*histogramState),
	}
}

func withCorrelationID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, correlationIDContextKey, id)
}

func correlationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(correlationIDContextKey).(string)
	if !ok {
		return ""
	}
	return v
}

func withTraceContext(ctx context.Context, traceCtx TraceContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, traceContextKey, traceCtx)
}

func traceContextFromContext(ctx context.Context) (TraceContext, bool) {
	if ctx == nil {
		return TraceContext{}, false
	}
	v, ok := ctx.Value(traceContextKey).(TraceContext)
	return v, ok
}

func (s *ObservabilityState) nextID(prefix string) string {
	n := atomic.AddUint64(&s.idSeq, 1)
	return fmt.Sprintf("%s-%d", prefix, n)
}

// NewCorrelationID は新しい相関 ID を生成します。
func (s *ObservabilityState) NewCorrelationID() string {
	return s.nextID("req")
}

// NewTraceID は新しいトレース ID を生成します。
func (s *ObservabilityState) NewTraceID() string {
	return s.nextID("trace")
}

// NewSpanID は新しい span ID を生成します。
func (s *ObservabilityState) NewSpanID() string {
	return s.nextID("span")
}

// SetLogWriter は構造化ログの出力先を変更します。
func (s *ObservabilityState) SetLogWriter(w io.Writer) {
	if w == nil {
		w = io.Discard
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logWriter = w
}

// Log は JSON line 形式で構造化ログを出力します。
func (s *ObservabilityState) Log(ctx context.Context, level, message string, fields map[string]any) error {
	entry := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     strings.ToLower(level),
		"message":   message,
	}

	if traceCtx, ok := traceContextFromContext(ctx); ok {
		if traceCtx.TraceID != "" {
			entry["trace_id"] = traceCtx.TraceID
		}
		if traceCtx.SpanID != "" {
			entry["span_id"] = traceCtx.SpanID
		}
		if traceCtx.Span != "" {
			entry["span"] = traceCtx.Span
		}
	}

	if fields != nil {
		normalizedFields := make(map[string]any, len(fields))
		for k, v := range fields {
			normalizedFields[normalizeLogFieldKey(k)] = v
		}
		if _, ok := normalizedFields["correlation_id"]; !ok {
			if cid := correlationIDFromContext(ctx); cid != "" {
				entry["correlation_id"] = cid
			}
		}
		for k, v := range normalizedFields {
			entry[k] = v
		}
	} else if cid := correlationIDFromContext(ctx); cid != "" {
		entry["correlation_id"] = cid
	}

	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	s.mu.RLock()
	writer := s.logWriter
	s.mu.RUnlock()
	if writer == nil {
		return nil
	}

	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}

func normalizeLogFieldKey(key string) string {
	return strings.ReplaceAll(key, "-", "_")
}

// IncCounter は counter を加算します。
func (s *ObservabilityState) IncCounter(name string, delta float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[name] += delta
}

// SetGauge は gauge を設定します。
func (s *ObservabilityState) SetGauge(name string, value float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gauges[name] = value
}

// ObserveHistogram は histogram に値を観測します。
func (s *ObservabilityState) ObserveHistogram(name string, value float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hist, ok := s.histograms[name]
	if !ok {
		hist = &histogramState{}
		s.histograms[name] = hist
	}

	hist.Count++
	hist.Sum += value
	if !hist.init {
		hist.Min = value
		hist.Max = value
		hist.init = true
		return
	}
	if value < hist.Min {
		hist.Min = value
	}
	if value > hist.Max {
		hist.Max = value
	}
}

// Snapshot はメトリクスのスナップショットを返します。
func (s *ObservabilityState) Snapshot() MetricsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counters := make(map[string]float64, len(s.counters))
	for k, v := range s.counters {
		counters[k] = v
	}

	gauges := make(map[string]float64, len(s.gauges))
	for k, v := range s.gauges {
		gauges[k] = v
	}

	histograms := make(map[string]HistogramSnapshot, len(s.histograms))
	for k, v := range s.histograms {
		histograms[k] = HistogramSnapshot{
			Count: v.Count,
			Sum:   v.Sum,
			Min:   v.Min,
			Max:   v.Max,
		}
	}

	return MetricsSnapshot{
		Counters:   counters,
		Gauges:     gauges,
		Histograms: histograms,
	}
}

func (vm *VM) SetObservability(state *ObservabilityState) {
	vm.observability = state
	vm.callableRuntime = nil
	if vm.session != nil {
		vm.session.observability = state
	}
}

func (vm *VM) getOrCreateObservabilityState() *ObservabilityState {
	if vm.observability == nil {
		vm.observability = NewObservabilityState()
	}
	return vm.observability
}

func (r *Runner) contextForExecution() context.Context {
	return r.getOrCreateExecutionSession().contextForExecution()
}

// SetLogOutput は構造化ログの出力先を設定します。
func (r *Runner) SetLogOutput(w io.Writer) {
	r.getOrCreateExecutionSession().getOrCreateObservabilityState().SetLogWriter(w)
}
