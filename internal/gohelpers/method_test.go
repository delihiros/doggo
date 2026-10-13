package gohelpers

import (
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"reflect"
	"strings"
	"testing"
)

// テスト用の構造体
type testStruct struct {
	Name         string
	Value        int
	hidden       string
	hiddenValues []string
}

func (t *testStruct) GetName() string {
	return t.Name
}

func (t *testStruct) SetName(name string) {
	t.Name = name
}

func (t *testStruct) Add(a, b int) int {
	return a + b
}

func (t *testStruct) Join(prefix string, values ...string) string {
	return prefix + strings.Join(values, ",")
}

func (t *testStruct) Panic() {
	panic("method boom")
}

func TestExtractMethodNameFromSymbol(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{".MethodName", "MethodName"},
		{"MethodName", "MethodName"},
		{".", "."},
		{".a", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			sym := types.NewSymbol(tt.input)
			result := ExtractMethodNameFromSymbol(sym)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtractMethodName(t *testing.T) {
	// 正常系: シンボル
	sym := types.NewSymbol(".GetName")
	name, err := ExtractMethodName(sym)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "GetName" {
		t.Errorf("expected 'GetName', got %q", name)
	}

	// 異常系: 非シンボル
	str := atom.String("NotASymbol")
	_, err = ExtractMethodName(str)
	if err == nil {
		t.Error("expected error for non-symbol")
	}
}

func TestUnwrapGoObject(t *testing.T) {
	// 正常系: 有効なオブジェクト
	obj := atom.NewGoObject(&testStruct{Name: "test"})
	rv, goVal, err := unwrapGoObject(obj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rv.IsValid() {
		t.Error("expected valid reflect.Value")
	}
	if goVal == nil {
		t.Error("expected non-nil goValue")
	}

	// 異常系: nilオブジェクト
	nilObj := atom.NewGoObject(nil)
	_, _, err = unwrapGoObject(nilObj)
	if err == nil {
		t.Error("expected error for nil object")
	}

	// 異常系: nilポインタ
	var nilPtr *testStruct
	nilPtrObj := atom.NewGoObject(nilPtr)
	_, _, err = unwrapGoObject(nilPtrObj)
	if err == nil {
		t.Error("expected error for nil pointer")
	}
}

func TestDerefStructValue(t *testing.T) {
	// 正常系: ポインタ
	ptr := &testStruct{Name: "test"}
	rv := reflect.ValueOf(ptr)
	structVal, err := derefStructValue(rv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if structVal.Kind() != reflect.Struct {
		t.Errorf("expected struct, got %s", structVal.Kind())
	}

	// 正常系: 値型
	val := testStruct{Name: "test"}
	rv = reflect.ValueOf(val)
	structVal, err = derefStructValue(rv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if structVal.Kind() != reflect.Struct {
		t.Errorf("expected struct, got %s", structVal.Kind())
	}

	// 異常系: 非構造体
	intVal := 42
	rv = reflect.ValueOf(intVal)
	_, err = derefStructValue(rv)
	if err == nil {
		t.Error("expected error for non-struct")
	}
}

func TestFindMethod(t *testing.T) {
	obj := &testStruct{Name: "test"}
	rv := reflect.ValueOf(obj)

	// 正常系: メソッドが存在
	method, err := FindMethod(rv, "GetName", obj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !method.IsValid() {
		t.Error("expected valid method")
	}

	// 異常系: メソッドが存在しない
	_, err = FindMethod(rv, "NonExistent", obj)
	if err == nil {
		t.Error("expected error for non-existent method")
	}
}

func TestGetField(t *testing.T) {
	obj := testStruct{Name: "test", Value: 42}
	rv := reflect.ValueOf(obj)

	// 正常系: フィールドが存在
	result, err := GetField(rv, "Name", obj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	str, ok := result.(atom.String)
	if !ok {
		t.Fatalf("expected String, got %T", result)
	}
	if string(str) != "test" {
		t.Errorf("expected 'test', got %s", str)
	}

	// 異常系: フィールドが存在しない
	_, err = GetField(rv, "NonExistent", obj)
	if err == nil {
		t.Error("expected error for non-existent field")
	}
}

func TestGetFieldRejectsUnexportedFieldsWithoutPanicking(t *testing.T) {
	obj := testStruct{hidden: "secret", hiddenValues: []string{"secret"}}
	rv := reflect.ValueOf(obj)

	for _, fieldName := range []string{"hidden", "hiddenValues"} {
		t.Run(fieldName, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("GetField(%q) panicked: %v", fieldName, recovered)
				}
			}()

			if value, err := GetField(rv, fieldName, obj); err == nil {
				t.Fatalf("GetField(%q) = %v, want access error", fieldName, value)
			}
		})
	}
}

func TestSetField(t *testing.T) {
	obj := &testStruct{Name: "original"}
	rv := reflect.ValueOf(obj).Elem()

	// 正常系: フィールドを設定
	_, err := SetField(rv, "Name", atom.String("updated"), obj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.Name != "updated" {
		t.Errorf("expected 'updated', got %s", obj.Name)
	}

	// 異常系: 存在しないフィールド
	_, err = SetField(rv, "NonExistent", atom.String("value"), obj)
	if err == nil {
		t.Error("expected error for non-existent field")
	}
}

func TestAccessField(t *testing.T) {
	obj := &testStruct{Name: "test", Value: 42}
	rv := reflect.ValueOf(obj)

	// 取得
	result, err := AccessField(rv, "Name", []types.Value{}, obj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	str, ok := result.(atom.String)
	if !ok {
		t.Fatalf("expected String, got %T", result)
	}
	if string(str) != "test" {
		t.Errorf("expected 'test', got %s", str)
	}

	// 設定
	_, err = AccessField(rv, "Name", []types.Value{atom.String("new")}, obj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.Name != "new" {
		t.Errorf("expected 'new', got %s", obj.Name)
	}
}

func TestCallMethodOrAccessField(t *testing.T) {
	obj := &testStruct{Name: "test", Value: 42}
	goObj := atom.NewGoObject(obj)

	// メソッド呼び出し
	result, err := CallMethodOrAccessField(goObj, "GetName", []types.Value{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	str, ok := result.(atom.String)
	if !ok {
		t.Fatalf("expected String, got %T", result)
	}
	if string(str) != "test" {
		t.Errorf("expected 'test', got %s", str)
	}

	// フィールドアクセス
	result, err = CallMethodOrAccessField(goObj, "Value", []types.Value{})
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

// TestCallMethodWithArgs は引数付きメソッド呼び出しをテストします。
func TestCallMethodWithArgs(t *testing.T) {
	obj := &testStruct{Name: "test"}
	goObj := atom.NewGoObject(obj)

	// Addメソッド呼び出し
	args := []types.Value{
		atom.NewNumberInt(3),
		atom.NewNumberInt(5),
	}
	result, err := CallMethodOrAccessField(goObj, "Add", args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	num, ok := result.(*atom.Number)
	if !ok {
		t.Fatalf("expected Number, got %T", result)
	}
	if *num.IntVal != 8 {
		t.Errorf("expected 8, got %d", *num.IntVal)
	}
}

func TestCallVariadicMethod(t *testing.T) {
	obj := atom.NewGoObject(&testStruct{})
	tests := []struct {
		name string
		args []types.Value
		want string
	}{
		{name: "zero variadic arguments", args: []types.Value{atom.String("prefix:")}, want: "prefix:"},
		{
			name: "multiple variadic arguments",
			args: []types.Value{atom.String("prefix:"), atom.String("a"), atom.String("b")},
			want: "prefix:a,b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CallMethodOrAccessField(obj, "Join", tt.args)
			if err != nil {
				t.Fatalf("CallMethodOrAccessField() error = %v", err)
			}
			if got := result.String(); got != `"`+tt.want+`"` {
				t.Fatalf("result = %s, want %q", got, tt.want)
			}
		})
	}
}

func TestCallMethod_ConvertsPanicToError(t *testing.T) {
	obj := atom.NewGoObject(&testStruct{})

	_, err := CallMethodOrAccessField(obj, "Panic", nil)
	if err == nil || !strings.Contains(err.Error(), "method boom") {
		t.Fatalf("CallMethodOrAccessField() error = %v, want panic converted to error", err)
	}
}

// TestCallMethodOrAccessField_NilObject はnilオブジェクトのエラーをテストします。
func TestCallMethodOrAccessField_NilObject(t *testing.T) {
	goObj := atom.NewGoObject(nil)

	_, err := CallMethodOrAccessField(goObj, "GetName", []types.Value{})
	if err == nil {
		t.Error("expected error for nil object")
	}
}

// TestCallMethodOrAccessField_NonExistent は存在しないメンバーのエラーをテストします。
func TestCallMethodOrAccessField_NonExistent(t *testing.T) {
	obj := &testStruct{Name: "test"}
	goObj := atom.NewGoObject(obj)

	_, err := CallMethodOrAccessField(goObj, "NonExistent", []types.Value{})
	if err == nil {
		t.Error("expected error for non-existent method/field")
	}
}

// TestAccessField_TooManyArgs は引数過多のエラーをテストします。
func TestAccessField_TooManyArgs(t *testing.T) {
	obj := &testStruct{Name: "test"}
	rv := reflect.ValueOf(obj)

	_, err := AccessField(rv, "Name", []types.Value{
		atom.String("one"),
		atom.String("two"),
	}, obj)
	if err == nil {
		t.Error("expected error for too many arguments")
	}
}

// TestConvertMethodArgs_WrongCount は引数数不一致のエラーをテストします。
func TestConvertMethodArgs_WrongCount(t *testing.T) {
	obj := &testStruct{}
	rv := reflect.ValueOf(obj)
	method, _ := FindMethod(rv, "Add", obj)

	// Add は2引数なのに1引数しか渡さない
	_, err := ConvertMethodArgs([]types.Value{atom.NewNumberInt(1)}, method.Type())
	if err == nil {
		t.Error("expected error for wrong argument count")
	}
}
