package gohelpers

import (
	"errors"
	"reflect"
	"testing"

	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestConvertResults_Empty(t *testing.T) {
	result, err := ConvertResults([]reflect.Value{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != atom.NilValue {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestConvertResults_SingleValue(t *testing.T) {
	val := reflect.ValueOf(42)
	result, err := ConvertResults([]reflect.Value{val})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	num, ok := result.(*atom.Number)
	if !ok {
		t.Fatalf("expected Number, got %T", result)
	}
	if *num.IntVal != 42 {
		t.Errorf("expected 42, got %d", *num.IntVal)
	}
}

func TestConvertResults_WithError(t *testing.T) {
	val := reflect.ValueOf(42)
	errVal := reflect.ValueOf(errors.New("test error"))

	_, err := ConvertResults([]reflect.Value{val, errVal})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errors.Unwrap(err)) {
		// エラーがラップされていることを確認
	}
}

func TestConvertResults_WithNilError(t *testing.T) {
	val := reflect.ValueOf("hello")
	var errVal error = nil
	errReflect := reflect.ValueOf(&errVal).Elem()

	result, err := ConvertResults([]reflect.Value{val, errReflect})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// nilエラーの場合、最初の値が返される
	str, ok := result.(atom.String)
	if !ok {
		t.Fatalf("expected String, got %T", result)
	}
	if string(str) != "hello" {
		t.Errorf("expected 'hello', got %s", str)
	}
}

func TestConvertResults_MultipleValues(t *testing.T) {
	val1 := reflect.ValueOf(1)
	val2 := reflect.ValueOf(2)

	result, err := ConvertResults([]reflect.Value{val1, val2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vec, ok := result.(*coll.Vector)
	if !ok {
		t.Fatalf("expected Vector, got %T", result)
	}
	if vec.Count() != 2 {
		t.Errorf("expected 2 elements, got %d", vec.Count())
	}
}

func TestResultsToVector(t *testing.T) {
	values := []reflect.Value{
		reflect.ValueOf(1),
		reflect.ValueOf("two"),
		reflect.ValueOf(3.0),
	}

	result, err := ResultsToVector(values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vec, ok := result.(*coll.Vector)
	if !ok {
		t.Fatalf("expected Vector, got %T", result)
	}
	if vec.Count() != 3 {
		t.Errorf("expected 3 elements, got %d", vec.Count())
	}
}
