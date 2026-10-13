package vm

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type vmUnregisteredReifyInterface interface{ vmUnregisteredReifyMarker() }

// TestVMReifySuccess はVMでのreifyの基本的な動作をテストします。
func TestVMReifySuccess(t *testing.T) {
	runner := MustNewRunner()

	// fmt.Stringerインターフェースの実装を作成
	code := `(do
  (import "fmt")
  (reify fmt/Stringer {:string (fn [] "hello from doggo")}))`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("reify failed: %v", err)
	}

	// 結果はGoObjectであるべき
	goObj, ok := result.(*atom.GoObject)
	if !ok {
		t.Fatalf("expected GoObject, got %T", result)
	}

	// fmt.Stringerインターフェースを実装しているか確認
	stringer, ok := goObj.Unwrap().(fmt.Stringer)
	if !ok {
		t.Fatalf("expected fmt.Stringer, got %T", goObj.Unwrap())
	}

	// String()メソッドを呼び出して結果を確認
	str := stringer.String()
	if str != "hello from doggo" {
		t.Errorf("String() = %q, want %q", str, "hello from doggo")
	}
}

func TestVMReifyBoundInterfaceType(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`(do
  (import "fmt")
  (let [target fmt/Stringer]
    (reify target {:string (fn [] "bound type")})))`)
	if err != nil {
		t.Fatalf("reify failed: %v", err)
	}

	goObj, ok := result.(*atom.GoObject)
	if !ok {
		t.Fatalf("expected GoObject, got %T", result)
	}
	stringer, ok := goObj.Unwrap().(fmt.Stringer)
	if !ok {
		t.Fatalf("expected fmt.Stringer, got %T", goObj.Unwrap())
	}
	if got := stringer.String(); got != "bound type" {
		t.Fatalf("String() = %q, want %q", got, "bound type")
	}
}

// TestVMReifyMissingMethod はメソッドが不足している場合の動作をテストします。
func TestVMReifyMissingMethod(t *testing.T) {
	runner := MustNewRunner()

	// :stringメソッドを提供しないマップで実装を作成
	code := `(do
  (import "fmt")
  (reify fmt/Stringer {}))`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("reify failed: %v", err)
	}

	goObj, ok := result.(*atom.GoObject)
	if !ok {
		t.Fatalf("expected GoObject, got %T", result)
	}

	stringer, ok := goObj.Unwrap().(fmt.Stringer)
	if !ok {
		t.Fatalf("expected fmt.Stringer, got %T", goObj.Unwrap())
	}

	// メソッドがない場合は空文字列を返すべき（ゼロ値）
	str := stringer.String()
	if str != "" {
		t.Errorf("String() = %q, want empty string for missing method", str)
	}
}

// TestVMReifyUnknownInterface は未登録のインターフェースの場合のエラーをテストします。
func TestVMReifyUnknownInterface(t *testing.T) {
	runner := MustNewRunner()
	_, err := runner.Run(`(do (import "io") (reify io/Reader {}))`)
	const want = `no reify factory registered for interface "io.Reader"`
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got: %v", want, err)
	}
}

func TestVMReifyUnknownInterfaceUsesTypeDiagnosticName(t *testing.T) {
	vm, err := New(&compiler.Bytecode{})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	interfaceType := reflect.TypeOf((*vmUnregisteredReifyInterface)(nil)).Elem()
	if err := vm.push(atom.NewGoObject(interfaceType)); err != nil {
		t.Fatalf("push interface type: %v", err)
	}
	if err := vm.push(coll.NewEmptyMap()); err != nil {
		t.Fatalf("push implementation map: %v", err)
	}

	err = vm.executeReify()
	want := `no reify factory registered for interface "` + interfaceType.String() + `"`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("executeReify() error = %v, want error containing %q", err, want)
	}
}

// TestVMReifyInvalidArguments は無効な引数の場合のエラーをテストします。
func TestVMReifyInvalidArguments(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		want         string
		compileError bool
	}{
		{"missing arguments", `(reify)`, "reify requires exactly 2 arguments", true},
		{"only one argument", `(reify fmt/Stringer)`, "reify requires exactly 2 arguments", true},
		{"string target", `(reify "fmt.Stringer" {})`, "reify expects a symbol", true},
		{"non-interface value", `(let [target 1] (reify target {}))`, "first argument must be a Go interface type", false},
		{"named concrete type", `(do (import "strings") (reify strings/Builder {}))`, "named Go interface type", false},
		{"non-map implementation", `(do (import "fmt") (reify fmt/Stringer "not a map"))`, "second argument must be a map", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			_, err := runner.Run(tt.code)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.want)
			}
			if tt.compileError {
				var compileErr *compiler.CompileError
				if !errors.As(err, &compileErr) {
					t.Fatalf("expected CompileError, got %T", err)
				}
			} else {
				var runtimeErr *RuntimeError
				if !errors.As(err, &runtimeErr) {
					t.Fatalf("expected RuntimeError, got %T", err)
				}
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got: %v", tt.want, err)
			}
		})
	}
}

// TestVMReifyWithStrConcat は文字列連結を使った関数をテストします。
func TestVMReifyWithStrConcat(t *testing.T) {
	runner := MustNewRunner()

	// 文字列連結を使用する実装を作成
	code := `
		(do
			(import "fmt")
			(reify fmt/Stringer
				{:string (fn [] (str "hello" " " "world"))}))
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("reify failed: %v", err)
	}

	goObj, ok := result.(*atom.GoObject)
	if !ok {
		t.Fatalf("expected GoObject, got %T", result)
	}

	stringer := goObj.Unwrap().(fmt.Stringer)

	// String()メソッドを呼び出して結果を確認
	if str := stringer.String(); str != "hello world" {
		t.Errorf("String() = %q, want %q", str, "hello world")
	}
}

// TestVMReifyHttpHandler はhttp.Handlerの実装をテストします。
func TestVMReifyHttpHandler(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{
			name: "UpperCamelCase Keyword",
			code: `(do (import "net/http")
  (def reify-handler-called (atom false))
  (reify http/Handler {:ServeHTTP (fn [w r] (reset! reify-handler-called true))}))`,
		},
		{
			name: "LowerCase Keyword",
			code: `(do (import "net/http")
  (def reify-handler-called (atom false))
  (reify http/Handler {:servehttp (fn [w r] (reset! reify-handler-called true))}))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.code)
			if err != nil {
				t.Fatalf("reify failed: %v", err)
			}

			goObj, ok := result.(*atom.GoObject)
			if !ok {
				t.Fatalf("expected GoObject, got %T", result)
			}

			handler, ok := goObj.Unwrap().(http.Handler)
			if !ok {
				t.Fatalf("expected http.Handler, got %T", goObj.Unwrap())
			}

			handler.ServeHTTP(nil, nil)
			called, err := runner.Run(`@reify-handler-called`)
			if err != nil {
				t.Fatalf("read callback atom: %v", err)
			}
			if got := called.String(); got != "true" {
				t.Fatalf("callback atom = %s, want true", got)
			}
		})
	}
}

// TestVMReifyWithClosure はクロージャを使用した実装をテストします。
func TestVMReifyWithClosure(t *testing.T) {
	runner := MustNewRunner()

	// クロージャで外部変数を参照する実装を作成
	code := `
		(do
			(import "fmt")
			(let [prefix "VM: "]
				(reify fmt/Stringer
					{:string (fn [] (str prefix "test"))})))
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("reify failed: %v", err)
	}

	goObj, ok := result.(*atom.GoObject)
	if !ok {
		t.Fatalf("expected GoObject, got %T", result)
	}

	stringer := goObj.Unwrap().(fmt.Stringer)

	// String()メソッドを呼び出して結果を確認
	if str := stringer.String(); str != "VM: test" {
		t.Errorf("String() = %q, want %q", str, "VM: test")
	}
}

func TestVMReifyPreservesDynamicBinding(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(import "fmt")
		(def ^:dynamic *reify-binding* "root")
		(let [implementation (reify fmt/Stringer
		                      {:string (fn [] *reify-binding*)})]
		  (binding [*reify-binding* "bound"]
		    (. implementation String)))
	`)
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if got := result.String(); got != `"bound"` {
		t.Fatalf("reify binding = %s, want %q", got, "bound")
	}
}

// TestVMReifyMultipleCalls は複数回の実装呼び出しをテストします。
func TestVMReifyMultipleCalls(t *testing.T) {
	runner := MustNewRunner()

	// アトムを使ってカウントする実装
	code := `
		(import "fmt")
		(def counter (atom 0))
		(def implementation (reify fmt/Stringer
			{:string (fn [] 
				(swap! counter inc)
				(str "call count: " @counter))}))
		implementation
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("reify failed: %v", err)
	}

	goObj, ok := result.(*atom.GoObject)
	if !ok {
		t.Fatalf("expected GoObject, got %T", result)
	}

	stringer := goObj.Unwrap().(fmt.Stringer)

	// String()メソッドを複数回呼び出して結果を確認
	expected := []string{"call count: 1", "call count: 2", "call count: 3"}
	for i, want := range expected {
		got := stringer.String()
		if got != want {
			t.Errorf("call %d: String() = %q, want %q", i+1, got, want)
		}
	}
}
