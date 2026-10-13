package gohelpers

import (
	"reflect"

	doggoerr "doggo/internal/errors"
	"doggo/internal/interop"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// InstantiateGoType はGo型のインスタンスを作成する
func InstantiateGoType(goType reflect.Type, args []types.Value) (interface{}, error) {
	// ポインタ型の処理
	elemType := goType
	if goType.Kind() == reflect.Ptr {
		elemType = goType.Elem()
	}

	// 構造体
	if elemType.Kind() == reflect.Struct {
		// 構造体は常にポインタとしてインスタンス化
		instancePtr := reflect.New(elemType)
		instance := instancePtr.Interface()
		if len(args) > 0 {
			if err := SetStructFields(instancePtr.Elem(), args); err != nil {
				return nil, err
			}
		}
		return instance, nil
	}

	// 非構造体
	if goType.Kind() == reflect.Ptr {
		// 基本型へのポインタ
		if len(args) == 0 {
			return reflect.New(elemType).Interface(), nil
		}
		elemValue := reflect.New(elemType).Elem()
		if err := SetValueFromArgs(elemValue, args); err != nil {
			return nil, err
		}
		return elemValue.Addr().Interface(), nil
	}

	// 値型
	zeroValue := reflect.New(goType).Elem()
	if len(args) > 0 {
		if err := SetValueFromArgs(zeroValue, args); err != nil {
			return nil, err
		}
	}
	return zeroValue.Interface(), nil
}

// SetStructFields は構造体のフィールドを設定する
func SetStructFields(structValue reflect.Value, args []types.Value) error {
	if structValue.Kind() != reflect.Struct {
		return doggoerr.TypeError("new", "struct", structValue.Kind().String())
	}

	// マップ引数
	if len(args) == 1 {
		if mapVal, ok := args[0].(*coll.Map); ok {
			return interop.PopulateStructFromMap(structValue, mapVal)
		}
	}

	// キーワード引数
	if len(args) > 0 && len(args)%2 == 0 {
		// 最初の引数がキーワードかチェック
		if _, ok := args[0].(atom.Keyword); ok {
			return interop.PopulateStructFromKeywords(structValue, args)
		}
	}

	// 同じ型からのコピー
	if len(args) == 1 {
		argValue := args[0]
		if goObj, ok := argValue.(*atom.GoObject); ok {
			if goObj == nil {
				return doggoerr.EvalErrorf("new", "cannot copy nil into struct %s", structValue.Type())
			}
			unwrapped := goObj.Unwrap()
			argReflectValue := reflect.ValueOf(unwrapped)
			if !argReflectValue.IsValid() {
				return doggoerr.EvalErrorf("new", "cannot copy nil into struct %s", structValue.Type())
			}
			if argReflectValue.Kind() == reflect.Ptr {
				if argReflectValue.IsNil() {
					return doggoerr.EvalErrorf("new", "cannot copy nil into struct %s", structValue.Type())
				}
				argReflectValue = argReflectValue.Elem()
			}
			if argReflectValue.Type().AssignableTo(structValue.Type()) {
				structValue.Set(argReflectValue)
				return nil
			}
		}
	}

	// 位置引数
	numFields := structValue.NumField()
	if len(args) == numFields {
		for i := 0; i < numFields; i++ {
			field := structValue.Field(i)
			if !field.CanSet() {
				fieldName := structValue.Type().Field(i).Name
				return doggoerr.EvalErrorf("new", "field %d (%s) cannot be set", i, fieldName)
			}
			fieldType := field.Type()
			converted, err := interop.ConvertDoggoToReflectValue(args[i], fieldType)
			if err != nil {
				return doggoerr.Wrapf(doggoerr.KindType, err, "failed to set field %d", i)
			}
			field.Set(converted)
		}
		return nil
	}

	return doggoerr.EvalErrorf("new", "cannot set struct fields: expected %d arguments or a map, got %d", numFields, len(args))
}

// SetValueFromArgs は引数から値を設定する
func SetValueFromArgs(value reflect.Value, args []types.Value) error {
	if len(args) == 0 {
		return nil
	}

	if len(args) == 1 {
		converted, err := interop.ConvertDoggoToReflectValue(args[0], value.Type())
		if err != nil {
			return err
		}
		if value.CanSet() {
			value.Set(converted)
		} else {
			return doggoerr.EvalErrorf("new", "cannot set value of type %s", value.Type())
		}
		return nil
	}

	return doggoerr.ArgCountError("new", 1, 1, len(args))
}
