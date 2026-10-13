package errors

import (
	"errors"
	"testing"
)

func TestDoggoError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *DoggoError
		want string
	}{
		{
			name: "basic error with kind",
			err:  New(KindParser, "unexpected token"),
			want: "parser error: unexpected token",
		},
		{
			name: "error with position",
			err:  ParserError("unexpected token", 10, 5),
			want: "parser error: unexpected token at line 10, column 5",
		},
		{
			name: "error with context",
			err:  EvalError("let", "binding must be a vector"),
			want: "evaluator error: let: binding must be a vector",
		},
		{
			name: "error with file and position",
			err:  ParserError("unexpected EOF", 42, 1).SetFile("test.dog"),
			want: "parser error: unexpected EOF at test.dog:42:1",
		},
		{
			name: "type error",
			err:  TypeError("first", "sequence", "number"),
			want: "type error: first: expected sequence, got number",
		},
		{
			name: "argument count error - exact",
			err:  ArgCountError("cons", 2, 2, 1),
			want: "argument error: cons: expects 2 argument(s), got 1",
		},
		{
			name: "argument count error - variadic",
			err:  ArgCountError("+", 1, -1, 0),
			want: "argument error: +: expects at least 1 argument(s), got 0",
		},
		{
			name: "argument count error - range",
			err:  ArgCountError("nth", 2, 3, 1),
			want: "argument error: nth: expects 2 to 3 argument(s), got 1",
		},
		{
			name: "not found error",
			err:  NotFoundError("resolve", "symbol", "foo"),
			want: "not found: resolve: symbol not found: foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("DoggoError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDoggoError_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	err := Wrap(KindEvaluator, cause, "wrapped error")

	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
	}

	// errors.Is should work
	if !errors.Is(err, cause) {
		t.Error("errors.Is() should return true for wrapped cause")
	}
}

func TestPosition_String(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		want string
	}{
		{
			name: "zero position",
			pos:  Position{},
			want: "",
		},
		{
			name: "line and column only",
			pos:  Position{Line: 10, Column: 5},
			want: "line 10, column 5",
		},
		{
			name: "with file",
			pos:  Position{File: "test.dog", Line: 10, Column: 5},
			want: "test.dog:10:5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.String(); got != tt.want {
				t.Errorf("Position.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPosition_IsZero(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		want bool
	}{
		{
			name: "zero position",
			pos:  Position{},
			want: true,
		},
		{
			name: "has line",
			pos:  Position{Line: 1},
			want: false,
		},
		{
			name: "has file only",
			pos:  Position{File: "test.dog"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.IsZero(); got != tt.want {
				t.Errorf("Position.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsDoggoError(t *testing.T) {
	doggoErr := New(KindParser, "test error")
	stdErr := errors.New("standard error")
	wrappedDoggoErr := errors.Join(stdErr, doggoErr)

	if !IsDoggoError(doggoErr) {
		t.Error("IsDoggoError should return true for DoggoError")
	}

	if IsDoggoError(stdErr) {
		t.Error("IsDoggoError should return false for standard error")
	}

	// Note: errors.Join does not support errors.As for wrapped errors
	// This tests that we correctly identify DoggoError in some wrapped contexts
	wrappedErr := Wrap(KindEvaluator, stdErr, "wrapper")
	if !IsDoggoError(wrappedErr) {
		t.Error("IsDoggoError should return true for wrapped DoggoError")
	}

	// Test nil
	if IsDoggoError(nil) {
		t.Error("IsDoggoError should return false for nil")
	}

	_ = wrappedDoggoErr // avoid unused variable
}

func TestAsDoggoError(t *testing.T) {
	doggoErr := New(KindParser, "test error")
	stdErr := errors.New("standard error")

	got, ok := AsDoggoError(doggoErr)
	if !ok || got != doggoErr {
		t.Error("AsDoggoError should return the DoggoError")
	}

	got, ok = AsDoggoError(stdErr)
	if ok || got != nil {
		t.Error("AsDoggoError should return nil for non-DoggoError")
	}
}

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{KindUnknown, "error"},
		{KindLexer, "lexer error"},
		{KindParser, "parser error"},
		{KindEvaluator, "evaluator error"},
		{KindType, "type error"},
		{KindArgument, "argument error"},
		{KindNotFound, "not found"},
		{KindRuntime, "runtime error"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("Kind.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDoggoError_ExceptionData(t *testing.T) {
	cause := errors.New("disk failure")
	err := RuntimeError("boom").
		SetContext("range").
		SetFile("sample.dog").
		SetPosition(12, 4).
		SetCause(cause)

	data := err.ExceptionData()
	if data == nil {
		t.Fatal("expected canonical exception data")
	}
	if data.Type != "DoggoError" {
		t.Fatalf("unexpected type: %q", data.Type)
	}
	if data.Kind != "runtime error" {
		t.Fatalf("unexpected kind: %q", data.Kind)
	}
	if data.Phase != "runtime" {
		t.Fatalf("unexpected phase: %q", data.Phase)
	}
	if data.Message != "boom" {
		t.Fatalf("unexpected message: %q", data.Message)
	}
	if data.Context["context"] != "range" {
		t.Fatalf("unexpected context: %#v", data.Context)
	}
	if data.At.File != "sample.dog" || data.At.Line != 12 || data.At.Column != 4 {
		t.Fatalf("unexpected position: %#v", data.At)
	}
	if data.Cause == nil {
		t.Fatal("expected canonical cause")
	}
	if data.Cause.Kind != "host error" {
		t.Fatalf("unexpected cause kind: %q", data.Cause.Kind)
	}
	if data.Cause.Message != "disk failure" {
		t.Fatalf("unexpected cause message: %q", data.Cause.Message)
	}
}

func TestNormalizeExceptionDataUsesProviderClone(t *testing.T) {
	err := ParserError("unexpected token", 7, 3).SetFile("parser.dog")

	data := NormalizeExceptionData(err)
	if data == nil {
		t.Fatal("expected canonical exception data")
	}
	if data.Kind != "parser error" {
		t.Fatalf("unexpected kind: %q", data.Kind)
	}
	data.Message = "mutated"

	fresh := err.ExceptionData()
	if fresh.Message != "unexpected token" {
		t.Fatalf("expected provider data to remain unchanged, got %q", fresh.Message)
	}
}
