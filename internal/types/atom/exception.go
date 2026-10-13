package atom

import (
	"fmt"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/util"
)

// Exception is Doggo's runtime error value.
type Exception struct {
	Message       string
	ExceptionType string // 例: "java.lang.Exception", "MyError" など
	Cause         types.Value
	StackTrace    []string
	CanonicalData *doggoerr.ExceptionData
}

// ExceptionInfoPayload preserves the original Doggo data map for ex-data.
type ExceptionInfoPayload struct {
	Value types.Value
}

func (e *Exception) Type() string {
	return "Exception"
}

func (e *Exception) Hash() uint32 {
	return util.HashString(e.String())
}

func (e *Exception) Equals(other types.Value) bool {
	if e == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	o, ok := other.(*Exception)
	if !ok {
		return false
	}
	return e.String() == o.String()
}

func (e *Exception) String() string {
	message := String(e.Message).String()
	if e.Cause != nil {
		return fmt.Sprintf("#error {:type %s :message %s :cause %s}", e.ExceptionType, message, e.Cause.String())
	}
	return fmt.Sprintf("#error {:type %s :message %s}", e.ExceptionType, message)
}

// NewException は新しいExceptionを作成します。
func NewException(exceptionType, message string, cause types.Value) *Exception {
	exc := &Exception{
		Message:       message,
		ExceptionType: exceptionType,
		Cause:         cause,
	}
	exc.CanonicalData = exc.buildCanonicalData()
	return exc
}

// NewExceptionInfo creates the internal value used by the public ex-info API.
func NewExceptionInfo(message string, data map[string]any, cause types.Value) *Exception {
	exc := NewException("ExceptionInfo", message, cause)
	exc.CanonicalData.Data = data
	return exc
}

// NewExceptionInfoWithPayload also preserves the original Doggo data value.
func NewExceptionInfoWithPayload(message string, data map[string]any, payload, cause types.Value) *Exception {
	exc := NewExceptionInfo(message, data, cause)
	exc.CanonicalData.Original = ExceptionInfoPayload{Value: payload}
	return exc
}

// CanonicalErrorData returns the canonical projection for a Doggo error value.
func CanonicalErrorData(value types.Value) *doggoerr.ExceptionData {
	if value == nil {
		return nil
	}
	if object, ok := value.(*GoObject); ok {
		if object == nil {
			return nil
		}
		return doggoerr.NormalizeExceptionData(object.Unwrap())
	}
	return doggoerr.NormalizeExceptionData(value)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (e *Exception) IsSelfEvaluating() bool {
	return true
}

// DoggoError は panic() に渡すためのラッパー構造体です。
// Go の通常の panic と区別するために使用します。
type DoggoError struct {
	Original types.Value // throw された値 (通常は *Exception)
}

// ExceptionData projects the legacy Exception compatibility type into the canonical model.
func (e *Exception) ExceptionData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}

	data := doggoerr.MergeLegacyExceptionData(
		e.CanonicalData,
		e.ExceptionType,
		e.Message,
		canonicalExceptionCause(e.Cause),
		e.legacyTraceFrames(),
	)
	e.CanonicalData = data
	return data.Clone()
}

func (e *Exception) buildCanonicalData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}
	return doggoerr.NewLegacyExceptionData(e.ExceptionType, e.Message, canonicalExceptionCause(e.Cause), e.legacyTraceFrames())
}

func canonicalExceptionCause(value types.Value) *doggoerr.ExceptionData {
	if value == nil {
		return nil
	}

	if exc, ok := value.(*Exception); ok {
		if exc.CanonicalData != nil {
			return exc.CanonicalData.Clone()
		}
		return exc.buildCanonicalData()
	}

	return doggoerr.NewThrownValueData(value.Type(), displayValue(value), value)
}

func displayValue(value types.Value) string {
	if value == nil {
		return ""
	}
	if d, ok := value.(types.Displayable); ok {
		return d.Display()
	}
	return value.String()
}

func (e *Exception) legacyTraceFrames() []doggoerr.ExceptionFrame {
	if e == nil || len(e.StackTrace) == 0 {
		return nil
	}
	trace := make([]doggoerr.ExceptionFrame, 0, len(e.StackTrace))
	for _, frame := range e.StackTrace {
		trace = append(trace, doggoerr.ExceptionFrame{Name: frame})
	}
	return trace
}
