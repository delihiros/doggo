package vm

import (
	"errors"

	"doggo/internal/compiler"
	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func normalizeThrownValue(value types.Value, trace []StackFrame) *atom.Exception {
	if exc, ok := value.(*atom.Exception); ok {
		if len(exc.StackTrace) == 0 && len(trace) > 0 {
			exc.StackTrace = formatExceptionStackTrace(trace)
		}
		refreshExceptionCanonicalData(exc, trace)
		return exc
	}

	message := valueDisplayString(value)
	exc := atom.NewException("Exception", message, value)
	exc.CanonicalData = doggoerr.NewCanonicalExceptionData(
		message,
		value,
		doggoerr.NewThrownValueData(value.Type(), message, value),
	)
	if len(trace) > 0 {
		exc.StackTrace = formatExceptionStackTrace(trace)
	}
	refreshExceptionCanonicalData(exc, trace)
	return exc
}

func normalizeError(err error) *atom.Exception {
	if err == nil {
		return nil
	}

	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) {
		runtimeData := runtimeErr.ExceptionData()
		if runtimeErr.Value != nil {
			if exc, ok := runtimeErr.Value.(*atom.Exception); ok {
				overlayRuntimeCanonicalData(exc, runtimeData)
				return exc
			}
			exc := normalizeThrownValue(runtimeErr.Value, runtimeErr.StackTrace)
			overlayRuntimeCanonicalData(exc, runtimeData)
			return exc
		}
		return exceptionFromCanonicalData(runtimeData)
	}

	var compileErr *compiler.CompileError
	if errors.As(err, &compileErr) {
		return exceptionFromCanonicalData(compileErr.ExceptionData())
	}

	var doggoError *doggoerr.DoggoError
	if errors.As(err, &doggoError) {
		return exceptionFromCanonicalData(doggoError.ExceptionData())
	}

	if data := doggoerr.NormalizeExceptionData(err); data != nil {
		return exceptionFromCanonicalData(data)
	}

	return atom.NewException("RuntimeError", err.Error(), nil)
}

func exceptionFromCanonicalData(data *doggoerr.ExceptionData) *atom.Exception {
	if data == nil {
		return atom.NewException("RuntimeError", "", nil)
	}

	exceptionType := data.Type
	if exceptionType == "" {
		exceptionType = "Exception"
	}

	var cause types.Value
	if data.Cause != nil {
		cause = canonicalCauseValue(data.Cause)
	}

	exc := atom.NewException(exceptionType, data.Message, cause)
	exc.CanonicalData = data.Clone()

	if len(data.Trace) > 0 {
		exc.StackTrace = formatExceptionFrames(data.Trace)
	} else if !data.At.IsZero() {
		exc.StackTrace = []string{formatStackFrame(StackFrame{
			File:   data.At.File,
			Line:   data.At.Line,
			Column: data.At.Column,
		})}
	}
	refreshExceptionCanonicalData(exc, nil)
	return exc
}

func canonicalCauseValue(data *doggoerr.ExceptionData) types.Value {
	if data == nil {
		return nil
	}
	if value, ok := data.Original.(types.Value); ok {
		return value
	}
	return exceptionFromCanonicalData(data)
}

func formatExceptionFrames(frames []doggoerr.ExceptionFrame) []string {
	if len(frames) == 0 {
		return nil
	}
	formatted := make([]string, len(frames))
	for i, frame := range frames {
		formatted[i] = formatStackFrame(StackFrame{
			File:   frame.File,
			Line:   frame.Line,
			Column: frame.Column,
			Name:   frame.Name,
		})
	}
	return formatted
}

func refreshExceptionCanonicalData(exc *atom.Exception, trace []StackFrame) {
	if exc == nil {
		return
	}
	data := exc.ExceptionData()
	if data == nil {
		return
	}

	if len(trace) > 0 {
		frames := make([]doggoerr.ExceptionFrame, 0, len(trace))
		for _, frame := range trace {
			frames = append(frames, doggoerr.ExceptionFrame{
				File:   frame.File,
				Line:   frame.Line,
				Column: frame.Column,
				Name:   frame.Name,
			})
		}
		data = doggoerr.WithTrace(data, frames)
	}
	exc.CanonicalData = data
}

func overlayRuntimeCanonicalData(exc *atom.Exception, runtimeData *doggoerr.ExceptionData) {
	if exc == nil || runtimeData == nil {
		return
	}
	refreshExceptionCanonicalData(exc, nil)
	if exc.CanonicalData == nil {
		exc.CanonicalData = runtimeData.Clone()
	} else {
		exc.CanonicalData = doggoerr.OverlayRuntimeData(exc.CanonicalData, runtimeData)
	}
	if len(runtimeData.Trace) > 0 {
		exc.StackTrace = formatExceptionFrames(runtimeData.Trace)
	} else if len(exc.StackTrace) == 0 && !runtimeData.At.IsZero() {
		exc.StackTrace = []string{formatStackFrame(StackFrame{
			File:   runtimeData.At.File,
			Line:   runtimeData.At.Line,
			Column: runtimeData.At.Column,
		})}
	}
}

func valueDisplayString(value types.Value) string {
	if value == nil {
		return ""
	}
	if d, ok := value.(types.Displayable); ok {
		return d.Display()
	}
	return value.String()
}

func (vm *VM) normalizeException(err error, trace []StackFrame) types.Value {
	if vmErr, ok := err.(*VMError); ok {
		return normalizeThrownValue(vmErr.Value, trace)
	}

	exc := normalizeError(err)
	if len(exc.StackTrace) == 0 && len(trace) > 0 {
		exc.StackTrace = formatExceptionStackTrace(trace)
	}
	refreshExceptionCanonicalData(exc, trace)
	return exc
}
