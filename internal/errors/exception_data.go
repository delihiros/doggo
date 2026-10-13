package errors

import stderrors "errors"

// ExceptionFrame is one canonical exception trace frame.
type ExceptionFrame struct {
	File   string
	Line   int
	Column int
	Name   string
}

// ExceptionData is the canonical Go-native exception model.
type ExceptionData struct {
	Type          string
	Kind          string
	Phase         string
	Message       string
	Original      any
	CorrelationID string
	Data          map[string]any
	Context       map[string]any
	At            Position
	Trace         []ExceptionFrame
	Cause         *ExceptionData
}

type exceptionDataProvider interface {
	ExceptionData() *ExceptionData
}

// NewLegacyExceptionData builds canonical data for a compatibility Exception value.
func NewLegacyExceptionData(exceptionType, message string, cause *ExceptionData, trace []ExceptionFrame) *ExceptionData {
	return &ExceptionData{
		Type:    exceptionType,
		Kind:    "exception",
		Phase:   "runtime",
		Message: message,
		Cause:   cause,
		Trace:   appendExceptionFrames(trace),
	}
}

// MergeLegacyExceptionData overlays current legacy Exception fields onto an
// existing canonical model while preserving richer canonical-only fields.
func MergeLegacyExceptionData(base *ExceptionData, exceptionType, message string, cause *ExceptionData, trace []ExceptionFrame) *ExceptionData {
	legacy := NewLegacyExceptionData(exceptionType, message, cause, trace)
	if base == nil {
		return legacy
	}

	synced := base.Clone()
	synced.Type = legacy.Type
	synced.Message = legacy.Message
	synced.Cause = legacy.Cause
	synced.Trace = appendExceptionFrames(legacy.Trace)
	return synced
}

// NewThrownValueData builds canonical data for an arbitrary thrown value.
func NewThrownValueData(typeName, message string, original any) *ExceptionData {
	return &ExceptionData{
		Type:     typeName,
		Kind:     "thrown value",
		Phase:    "runtime",
		Message:  message,
		Original: original,
	}
}

// NewCanonicalExceptionData builds canonical data for a normalized thrown Exception wrapper.
func NewCanonicalExceptionData(message string, original any, cause *ExceptionData) *ExceptionData {
	return &ExceptionData{
		Type:     "Exception",
		Kind:     "exception",
		Phase:    "runtime",
		Message:  message,
		Original: original,
		Cause:    cause,
	}
}

// WithTrace replaces canonical trace frames while preserving the rest of the model.
func WithTrace(data *ExceptionData, trace []ExceptionFrame) *ExceptionData {
	if data == nil {
		return nil
	}
	cloned := data.Clone()
	cloned.Trace = appendExceptionFrames(trace)
	return cloned
}

// OverlayRuntimeData merges runtime projection fields into an existing canonical model.
func OverlayRuntimeData(base, runtime *ExceptionData) *ExceptionData {
	if runtime == nil {
		return base.Clone()
	}
	if base == nil {
		return runtime.Clone()
	}

	merged := base.Clone()
	merged.Phase = runtime.Phase
	merged.CorrelationID = runtime.CorrelationID
	merged.At = runtime.At
	merged.Trace = appendExceptionFrames(runtime.Trace)
	if len(runtime.Context) > 0 {
		if merged.Context == nil {
			merged.Context = map[string]any{}
		}
		for key, value := range runtime.Context {
			merged.Context[key] = value
		}
	}
	return merged
}

// Clone returns a copy of the exception data tree, including cloned top-level
// maps and nested cause/trace structures.
func (e *ExceptionData) Clone() *ExceptionData {
	if e == nil {
		return nil
	}

	cloned := *e
	cloned.Data = cloneMap(e.Data)
	cloned.Context = cloneMap(e.Context)
	if len(e.Trace) > 0 {
		cloned.Trace = append([]ExceptionFrame(nil), e.Trace...)
	}
	cloned.Cause = e.Cause.Clone()
	return &cloned
}

// NormalizeExceptionData projects supported exception-like values into the
// canonical model.
func NormalizeExceptionData(value any) *ExceptionData {
	if value == nil {
		return nil
	}

	if provider, ok := value.(exceptionDataProvider); ok {
		if data := provider.ExceptionData(); data != nil {
			return data.Clone()
		}
	}

	err, ok := value.(error)
	if !ok {
		return nil
	}

	var provider exceptionDataProvider
	if stderrors.As(err, &provider) {
		if data := provider.ExceptionData(); data != nil {
			return data.Clone()
		}
	}

	return &ExceptionData{
		Type:    "HostError",
		Kind:    "host error",
		Phase:   "host",
		Message: err.Error(),
		Cause:   NormalizeExceptionData(stderrors.Unwrap(err)),
	}
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func appendExceptionFrames(trace []ExceptionFrame) []ExceptionFrame {
	if len(trace) == 0 {
		return nil
	}
	return append([]ExceptionFrame(nil), trace...)
}
