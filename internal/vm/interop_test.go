package vm

import (
	"strings"
	"testing"
)

func TestCallGoFunctionVMConvertsPanicToError(t *testing.T) {
	panicking := func() { panic("vm boom") }

	_, err := callGoFunctionVM(panicking, nil)
	if err == nil || !strings.Contains(err.Error(), "vm boom") {
		t.Fatalf("callGoFunctionVM() error = %v, want panic converted to error", err)
	}
}

func TestGoFunctionWrapperUsesIdentityEquality(t *testing.T) {
	fn := func() {}
	wrapper := &goFunctionWrapper{fn: fn}
	other := &goFunctionWrapper{fn: fn}

	if !wrapper.Equals(wrapper) {
		t.Fatal("go function wrapper must equal itself")
	}
	if wrapper.Equals(other) {
		t.Fatal("distinct go function wrappers must not be equal")
	}
}

func TestDotRejectsDoggoMap(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(. {:foo 42} foo)`); err == nil {
		t.Fatal("dot accepted a Doggo map instead of requiring a Go object")
	}
}

func TestInteropChainingUsesResultWhileDotoUsesReceiver(t *testing.T) {
	runner := MustNewRunner()
	assertRunErrorContains(t, runner, `(do
		(def builder (new "strings.Builder"))
		(.. builder (WriteString "Hello") (WriteString " World")))`, "expected go-object")
	assertRunResult(t, runner, `(do
		(def builder (new "strings.Builder"))
		(doto builder
			(. WriteString "Hello")
			(. WriteString " World"))
		(. builder String))`, `"Hello World"`)
}

func TestGoNilSliceReturnMapsToDoggoNil(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`(do (import "fmt") (nil? (fmt/Append nil)))`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if got := result.String(); got != "true" {
		t.Fatalf("Run() = %s, want true", got)
	}
}

func TestRunnerSurvivesNilGoObjectInterfaceArgument(t *testing.T) {
	runner := MustNewRunner()
	_, err := runner.Run(`(do (import "io") (io/ReadAll (new io/Reader)))`)
	if err == nil {
		t.Fatal("io/ReadAll accepted a nil io.Reader")
	}

	result, err := runner.Run(`:still-alive`)
	if err != nil {
		t.Fatalf("runner failed after interop error: %v", err)
	}
	if got := result.String(); got != ":still-alive" {
		t.Fatalf("runner result = %s, want :still-alive", got)
	}
}
