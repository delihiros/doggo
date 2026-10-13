package vm

import (
	"testing"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types/atom"
)

func TestCanonicalExceptionDataNormalizesDoggoAndRuntimeErrors(t *testing.T) {
	view := doggoerr.NormalizeExceptionData(
		doggoerr.RuntimeError("boom").
			SetContext("range").
			SetFile("example.dog").
			SetPosition(3, 7),
	)
	if view == nil {
		t.Fatal("expected canonical exception data")
	}
	if view.Message != "boom" {
		t.Fatalf("unexpected message: %q", view.Message)
	}
	if view.Kind != "runtime error" {
		t.Fatalf("unexpected kind: %q", view.Kind)
	}

	runtimeView := doggoerr.NormalizeExceptionData(&RuntimeError{
		Message:       "kaboom",
		Value:         atom.NewNumberInt(42),
		File:          "runtime.dog",
		Line:          9,
		Column:        2,
		CorrelationID: "corr-123",
		Snippet:       "(throw 42)",
		SnippetCol:    8,
		StackTrace: []StackFrame{{
			File:   "runtime.dog",
			Line:   9,
			Column: 2,
			Name:   "boom",
		}},
	})
	if runtimeView == nil {
		t.Fatal("expected runtime canonical exception data")
	}
	if runtimeView.Message != "kaboom" {
		t.Fatalf("unexpected runtime message: %q", runtimeView.Message)
	}
	if runtimeView.CorrelationID != "corr-123" {
		t.Fatalf("unexpected correlation ID: %q", runtimeView.CorrelationID)
	}
	if runtimeView.Original == nil {
		t.Fatal("expected original thrown value to be preserved")
	}
	if got := runtimeView.Context["snippet"]; got != "(throw 42)" {
		t.Fatalf("unexpected runtime snippet: %#v", runtimeView.Context)
	}
	if len(runtimeView.Trace) != 1 || runtimeView.Trace[0].Name != "boom" {
		t.Fatalf("unexpected runtime trace: %#v", runtimeView.Trace)
	}
}

func TestVMHandleErrorRejectsNonErrorThrowWithCanonicalData(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(try
		  (throw 42)
		  (catch [e]
		    [(ex-message e) (:kind (ex-data e))]))`)
	if err != nil {
		t.Fatal(err)
	}
	if result.String() != `["throw expects a canonical error value" :invalid-argument]` {
		t.Fatalf("unexpected result: %s", result.String())
	}
}

func TestExceptionCompatibilityConstructorProducesCanonicalException(t *testing.T) {
	exc := atom.NewException("LegacyError", "boom", atom.NewString("tail"))
	if exc.CanonicalData == nil {
		t.Fatal("expected canonical data on compatibility exception")
	}
	if exc.CanonicalData.Type != "LegacyError" {
		t.Fatalf("unexpected type: %q", exc.CanonicalData.Type)
	}
	if exc.CanonicalData.Message != "boom" {
		t.Fatalf("unexpected message: %q", exc.CanonicalData.Message)
	}
	if exc.CanonicalData.Cause == nil {
		t.Fatal("expected canonical cause")
	}
	if exc.CanonicalData.Cause.Message != "tail" {
		t.Fatalf("unexpected cause message: %q", exc.CanonicalData.Cause.Message)
	}
}

func TestNormalizeErrorRuntimeErrorOverlaysCanonicalExceptionData(t *testing.T) {
	original := atom.NewException("LegacyError", "original", atom.NewString("tail"))
	original.CanonicalData = &doggoerr.ExceptionData{
		Type:    "LegacyError",
		Kind:    "exception",
		Phase:   "runtime",
		Message: "original enriched",
		Context: map[string]any{
			"user": "kept",
		},
		Data: map[string]any{
			"code": "E-1",
		},
		Original: "semantic payload",
	}
	err := &RuntimeError{
		Message:       "runtime boom",
		Value:         original,
		File:          "runtime.dog",
		Line:          9,
		Column:        2,
		CorrelationID: "corr-123",
		Snippet:       "(boom)",
		SnippetCol:    3,
		StackTrace: []StackFrame{{
			File:   "runtime.dog",
			Line:   9,
			Column: 2,
			Name:   "boom",
		}},
	}

	exc := normalizeError(err)
	if exc != original {
		t.Fatal("expected runtime normalization to reuse original exception")
	}
	if exc.CanonicalData == nil {
		t.Fatal("expected canonical data")
	}
	if exc.CanonicalData.Message != "original" {
		t.Fatalf("unexpected message: %q", exc.CanonicalData.Message)
	}
	if exc.CanonicalData.CorrelationID != "corr-123" {
		t.Fatalf("unexpected correlation ID: %q", exc.CanonicalData.CorrelationID)
	}
	if got := exc.CanonicalData.Context["snippet"]; got != "(boom)" {
		t.Fatalf("unexpected context: %#v", exc.CanonicalData.Context)
	}
	if got := exc.CanonicalData.Context["user"]; got != "kept" {
		t.Fatalf("expected existing context to survive, got %#v", exc.CanonicalData.Context)
	}
	if got := exc.CanonicalData.Data["code"]; got != "E-1" {
		t.Fatalf("expected existing data to survive, got %#v", exc.CanonicalData.Data)
	}
	if exc.CanonicalData.At.File != "runtime.dog" || exc.CanonicalData.At.Line != 9 || exc.CanonicalData.At.Column != 2 {
		t.Fatalf("unexpected position: %#v", exc.CanonicalData.At)
	}
	if exc.CanonicalData.Original != "semantic payload" {
		t.Fatal("expected semantic payload to be retained")
	}
	if len(exc.CanonicalData.Trace) != 1 || exc.CanonicalData.Trace[0].Name != "boom" {
		t.Fatalf("unexpected trace: %#v", exc.CanonicalData.Trace)
	}
}

func TestExceptionDataPreservesStoredCanonicalData(t *testing.T) {
	exc := atom.NewException("LegacyError", "legacy", atom.NewString("tail"))
	exc.CanonicalData = &doggoerr.ExceptionData{
		Type:    "LegacyError",
		Kind:    "runtime error",
		Phase:   "runtime",
		Message: "enriched",
		Context: map[string]any{
			"snippet": "(boom)",
		},
		Data: map[string]any{
			"code": "E-2",
		},
	}
	exc.Message = "updated"
	exc.ExceptionType = "UpdatedError"
	exc.StackTrace = []string{"frame-1"}

	data := exc.ExceptionData()
	if data.Message != "updated" {
		t.Fatalf("unexpected message: %q", data.Message)
	}
	if data.Type != "UpdatedError" {
		t.Fatalf("unexpected type: %q", data.Type)
	}
	if data.Context["snippet"] != "(boom)" {
		t.Fatalf("unexpected context: %#v", data.Context)
	}
	if data.Data["code"] != "E-2" {
		t.Fatalf("unexpected data: %#v", data.Data)
	}
	if len(data.Trace) != 1 || data.Trace[0].Name != "frame-1" {
		t.Fatalf("unexpected trace: %#v", data.Trace)
	}

	data.Message = "mutated"
	if exc.CanonicalData.Message != "updated" {
		t.Fatalf("expected canonical data clone, got %q", exc.CanonicalData.Message)
	}
}

func TestRunnerExceptionFromErrorPreservesSourceDiagnosticContext(t *testing.T) {
	runner := MustNewRunner()
	value := runner.exceptionFromError(&SourceDiagnosticError{
		File:       "source.dog",
		Line:       4,
		Column:     2,
		Message:    "bad source",
		Snippet:    "(oops)",
		SnippetCol: 3,
	})

	exc, ok := value.(*atom.Exception)
	if !ok {
		t.Fatalf("expected Exception, got %T", value)
	}
	if exc.ExceptionType != "SourceDiagnosticError" {
		t.Fatalf("unexpected exception type: %q", exc.ExceptionType)
	}
	if exc.Message != "bad source" {
		t.Fatalf("unexpected message: %q", exc.Message)
	}
	if exc.CanonicalData == nil {
		t.Fatal("expected canonical data")
	}
	if exc.CanonicalData.Kind != "source diagnostic error" {
		t.Fatalf("unexpected canonical kind: %q", exc.CanonicalData.Kind)
	}
	if exc.CanonicalData.Phase != "read" {
		t.Fatalf("unexpected canonical phase: %q", exc.CanonicalData.Phase)
	}
	if exc.CanonicalData.At.File != "source.dog" || exc.CanonicalData.At.Line != 4 || exc.CanonicalData.At.Column != 2 {
		t.Fatalf("unexpected canonical position: %#v", exc.CanonicalData.At)
	}
	if got := exc.CanonicalData.Context["snippet"]; got != "(oops)" {
		t.Fatalf("unexpected canonical context: %#v", exc.CanonicalData.Context)
	}
	if len(exc.StackTrace) != 1 || exc.StackTrace[0] != "source.dog:4:2" {
		t.Fatalf("unexpected stack trace: %#v", exc.StackTrace)
	}
}

func TestRefreshExceptionCanonicalDataSyncsLegacyMutationAndPreservesRichFields(t *testing.T) {
	exc := atom.NewException("LegacyError", "legacy", atom.NewString("tail"))
	exc.CanonicalData = &doggoerr.ExceptionData{
		Type:    "LegacyError",
		Kind:    "runtime error",
		Phase:   "runtime",
		Message: "enriched",
		Context: map[string]any{
			"user": "kept",
		},
		Data: map[string]any{
			"code": "E-3",
		},
	}
	exc.Message = "mutated in vm"
	exc.StackTrace = []string{"legacy-frame"}

	refreshExceptionCanonicalData(exc, []StackFrame{{
		File:   "vm.dog",
		Line:   8,
		Column: 2,
		Name:   "boom",
	}})

	if exc.CanonicalData == nil {
		t.Fatal("expected canonical data")
	}
	if exc.CanonicalData.Message != "mutated in vm" {
		t.Fatalf("unexpected message: %q", exc.CanonicalData.Message)
	}
	if exc.CanonicalData.Kind != "runtime error" {
		t.Fatalf("unexpected kind: %q", exc.CanonicalData.Kind)
	}
	if exc.CanonicalData.Context["user"] != "kept" {
		t.Fatalf("unexpected context: %#v", exc.CanonicalData.Context)
	}
	if exc.CanonicalData.Data["code"] != "E-3" {
		t.Fatalf("unexpected data: %#v", exc.CanonicalData.Data)
	}
	if len(exc.CanonicalData.Trace) != 1 || exc.CanonicalData.Trace[0].Name != "boom" {
		t.Fatalf("unexpected trace: %#v", exc.CanonicalData.Trace)
	}
}
