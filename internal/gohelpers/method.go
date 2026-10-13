package gohelpers

import (
	"fmt"
	"reflect"

	doggoerr "doggo/internal/errors"
	"doggo/internal/interop"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// ExtractMethodNameFromSymbol はシンボルからメソッド名を抽出する（先頭の.を除去）
func ExtractMethodNameFromSymbol(sym types.Symbol) string {
	symStr := string(sym)
	if len(symStr) > 1 && symStr[0] == '.' {
		return symStr[1:]
	}
	return symStr
}

// ExtractMethodName はtypes.Valueからメソッド名を抽出する
func ExtractMethodName(methodNameValue types.Value) (string, error) {
	methodNameSym, ok := methodNameValue.(types.Symbol)
	if !ok {
		return "", doggoerr.TypeError("method call", "symbol", methodNameValue.Type())
	}
	return ExtractMethodNameFromSymbol(methodNameSym), nil
}

func unwrapGoObject(obj *atom.GoObject) (reflect.Value, interface{}, error) {
	goValue := obj.Unwrap()
	if goValue == nil {
		return reflect.Value{}, nil, doggoerr.RuntimeError("cannot access nil object")
	}

	rv := reflect.ValueOf(goValue)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return reflect.Value{}, nil, doggoerr.RuntimeError("cannot access nil pointer")
	}

	return rv, goValue, nil
}

func derefStructValue(rv reflect.Value) (reflect.Value, error) {
	structValue := rv
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return reflect.Value{}, doggoerr.RuntimeError("cannot access field on nil pointer")
		}
		structValue = rv.Elem()
	}

	if structValue.Kind() != reflect.Struct {
		return reflect.Value{}, doggoerr.TypeError("field access", "struct", structValue.Kind().String())
	}

	return structValue, nil
}

// FindMethod はreflect.Value上のメソッドを検索する
func FindMethod(rv reflect.Value, methodName string, goValue interface{}) (reflect.Value, error) {
	method := rv.MethodByName(methodName)
	if method.IsValid() {
		return method, nil
	}

	if rv.Kind() != reflect.Ptr && rv.CanAddr() {
		method = rv.Addr().MethodByName(methodName)
		if method.IsValid() {
			return method, nil
		}
	}

	if rv.Kind() == reflect.Ptr {
		method = rv.Elem().MethodByName(methodName)
		if method.IsValid() {
			return method, nil
		}
	}

	return reflect.Value{}, doggoerr.NotFoundError("method call", "method", fmt.Sprintf("%s on %T", methodName, goValue))
}

// ConvertMethodArgs はメソッド呼び出し用に引数を変換する
func ConvertMethodArgs(args []types.Value, methodType reflect.Type) ([]reflect.Value, error) {
	return interop.ConvertArgsToReflectValues(args, methodType)
}

// CallMethod はメソッドを呼び出して結果を返す
func CallMethod(method reflect.Value, args []types.Value) (types.Value, error) {
	argValues, err := ConvertMethodArgs(args, method.Type())
	if err != nil {
		return nil, doggoerr.Wrap(doggoerr.KindArgument, err, "cannot convert method arguments")
	}

	results, err := interop.CallReflect(method, argValues)
	if err != nil {
		return nil, err
	}
	return ConvertResults(results)
}

// GetField はフィールドの値を取得する
func GetField(structValue reflect.Value, fieldName string, goValue interface{}) (types.Value, error) {
	field := structValue.FieldByName(fieldName)
	if !field.IsValid() {
		return nil, doggoerr.NotFoundError("field access", "field", fmt.Sprintf("%s on %T", fieldName, goValue))
	}
	if !field.CanInterface() {
		return nil, doggoerr.EvalErrorf("field access", "field %s cannot be accessed (unexported)", fieldName)
	}

	return interop.ConvertReflectValueToDoggo(field)
}

// SetField はフィールドの値を設定する
func SetField(structValue reflect.Value, fieldName string, value types.Value, goValue interface{}) (types.Value, error) {
	field := structValue.FieldByName(fieldName)
	if !field.IsValid() {
		return nil, doggoerr.NotFoundError("field access", "field", fmt.Sprintf("%s on %T", fieldName, goValue))
	}

	if !field.CanSet() {
		return nil, doggoerr.EvalErrorf("field access", "field %s cannot be set (unexported or unaddressable)", fieldName)
	}

	converted, err := interop.ConvertDoggoToReflectValue(value, field.Type())
	if err != nil {
		return nil, doggoerr.Wrapf(doggoerr.KindType, err, "cannot convert value for field %s", fieldName)
	}

	field.Set(converted)
	return atom.NilValue, nil
}

// AccessField はフィールドにアクセスする（取得または設定）
func AccessField(rv reflect.Value, fieldName string, args []types.Value, goValue interface{}) (types.Value, error) {
	structValue, err := derefStructValue(rv)
	if err != nil {
		return nil, doggoerr.Wrapf(doggoerr.KindRuntime, err, "field %s", fieldName)
	}

	if len(args) > 0 {
		if len(args) != 1 {
			return nil, doggoerr.ArgCountError("field assignment", 1, 1, len(args))
		}
		return SetField(structValue, fieldName, args[0], goValue)
	}

	return GetField(structValue, fieldName, goValue)
}

// CallMethodOrAccessField はメソッド呼び出しまたはフィールドアクセスを行う
func CallMethodOrAccessField(obj *atom.GoObject, methodName string, args []types.Value) (types.Value, error) {
	rv, goValue, err := unwrapGoObject(obj)
	if err != nil {
		return nil, err
	}

	// メソッドを試行
	method, err := FindMethod(rv, methodName, goValue)
	if err == nil {
		return CallMethod(method, args)
	}

	// フィールドを試行
	return AccessField(rv, methodName, args, goValue)
}
