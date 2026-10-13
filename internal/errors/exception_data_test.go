package errors

import (
	stderrors "errors"
	"testing"
)

func TestNormalizeExceptionDataDoggoAndHostErrors(t *testing.T) {
	t.Run("doggo error", func(t *testing.T) {
		view := NormalizeExceptionData(
			RuntimeError("boom").
				SetContext("range").
				SetFile("sample.dog").
				SetPosition(12, 4),
		)
		if view == nil {
			t.Fatal("expected canonical exception data")
		}
		if view.Kind != "runtime error" {
			t.Fatalf("unexpected kind: %q", view.Kind)
		}
		if view.Message != "boom" {
			t.Fatalf("unexpected message: %q", view.Message)
		}
		if view.Context["context"] != "range" {
			t.Fatalf("unexpected context: %#v", view.Context)
		}
		if view.At.File != "sample.dog" || view.At.Line != 12 || view.At.Column != 4 {
			t.Fatalf("unexpected position: %#v", view.At)
		}
	})

	t.Run("host error", func(t *testing.T) {
		view := NormalizeExceptionData(stderrors.New("host boom"))
		if view == nil {
			t.Fatal("expected canonical exception data")
		}
		if view.Kind != "host error" {
			t.Fatalf("unexpected kind: %q", view.Kind)
		}
		if view.Message != "host boom" {
			t.Fatalf("unexpected message: %q", view.Message)
		}
	})

	t.Run("wrapped host error preserves cause", func(t *testing.T) {
		root := stderrors.New("disk offline")
		wrapped := stderrors.New("host boom")
		err := wrappedHostError{msg: wrapped.Error(), cause: root}

		view := NormalizeExceptionData(err)
		if view == nil {
			t.Fatal("expected canonical exception data")
		}
		if view.Cause == nil {
			t.Fatal("expected wrapped host cause")
		}
		if view.Cause.Kind != "host error" {
			t.Fatalf("unexpected cause kind: %q", view.Cause.Kind)
		}
		if view.Cause.Message != "disk offline" {
			t.Fatalf("unexpected cause message: %q", view.Cause.Message)
		}
	})
}

func TestWithTraceReturnsClone(t *testing.T) {
	base := &ExceptionData{
		Message: "boom",
		Trace:   []ExceptionFrame{{File: "old.dog", Line: 1}},
	}

	withTrace := WithTrace(base, []ExceptionFrame{{File: "new.dog", Line: 2}})
	if withTrace == base {
		t.Fatal("WithTrace returned the original data")
	}
	if got := withTrace.Trace[0].File; got != "new.dog" {
		t.Fatalf("trace file = %q, want new.dog", got)
	}
	if got := base.Trace[0].File; got != "old.dog" {
		t.Fatalf("base trace was mutated: %q", got)
	}
}

func TestMergeLegacyExceptionDataPreservesCanonicalFields(t *testing.T) {
	base := &ExceptionData{
		Type:          "Canonical",
		Kind:          "canonical kind",
		Phase:         "analysis",
		Message:       "canonical message",
		CorrelationID: "request-1",
		Data:          map[string]any{"canonical": true},
	}

	merged := MergeLegacyExceptionData(base, "Legacy", "legacy message", nil, nil)
	if merged == base {
		t.Fatal("MergeLegacyExceptionData returned the original data")
	}
	if merged.Type != "Legacy" || merged.Message != "legacy message" {
		t.Fatalf("legacy fields were not merged: %#v", merged)
	}
	if merged.Kind != base.Kind || merged.Phase != base.Phase || merged.CorrelationID != base.CorrelationID {
		t.Fatalf("canonical fields were not preserved: %#v", merged)
	}
	merged.Data["canonical"] = false
	if got := base.Data["canonical"]; got != true {
		t.Fatalf("base data was mutated: %#v", got)
	}
	if base.Type != "Canonical" || base.Message != "canonical message" {
		t.Fatalf("base legacy fields were mutated: %#v", base)
	}
}

type wrappedHostError struct {
	msg   string
	cause error
}

func (e wrappedHostError) Error() string {
	return e.msg
}

func (e wrappedHostError) Unwrap() error {
	return e.cause
}
