package interop

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// ConvertReflectValueToDoggo converts reflect.Value to doggo value.
func ConvertReflectValueToDoggo(rv reflect.Value) (types.Value, error) {
	if !rv.IsValid() {
		return atom.NilValue, nil
	}

	for rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return atom.NilValue, nil
		}
		rv = rv.Elem()
	}
	if isNilableKind(rv.Kind()) && rv.IsZero() {
		return atom.NilValue, nil
	}

	switch rv.Kind() {
	case reflect.Bool:
		return atom.Boolean(rv.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return atom.NewNumberInt(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value := rv.Uint()
		if value > uint64(^uint64(0)>>1) {
			return nil, fmt.Errorf("%s value %d cannot be represented as a Doggo integer", rv.Type(), value)
		}
		return atom.NewNumberInt(int64(value)), nil
	case reflect.Float32, reflect.Float64:
		return atom.NewNumberFloat(rv.Float()), nil
	case reflect.String:
		value := rv.String()
		if !utf8.ValidString(value) {
			return nil, fmt.Errorf("%s contains invalid UTF-8", rv.Type())
		}
		return atom.String(value), nil
	default:
		return atom.NewGoObject(rv.Interface()), nil
	}
}

// ConvertDoggoToReflectValue converts doggo value to reflect.Value.
func ConvertDoggoToReflectValue(val types.Value, targetType reflect.Type) (reflect.Value, error) {
	// interface{} type
	if targetType.Kind() == reflect.Interface && targetType.NumMethod() == 0 {
		var goVal interface{}
		switch v := val.(type) {
		case *atom.GoObject:
			if v != nil {
				goVal = v.Unwrap()
			}
		case *atom.Number:
			if v.IntVal != nil {
				goVal = *v.IntVal
			} else if v.FloatVal != nil {
				goVal = *v.FloatVal
			}
		case atom.String:
			goVal = string(v)
		case atom.Boolean:
			goVal = bool(v)
		case types.Nil:
			goVal = nil
		default:
			goVal = val
		}
		converted := reflect.ValueOf(goVal)
		if !converted.IsValid() {
			return reflect.Zero(targetType), nil
		}
		return converted, nil
	}

	// Map to Struct conversion
	if mapVal, ok := val.(*coll.Map); ok {
		if targetType.Kind() == reflect.Struct {
			return MapToStruct(mapVal, targetType)
		}
		if targetType.Kind() == reflect.Ptr && targetType.Elem().Kind() == reflect.Struct {
			structVal, err := MapToStruct(mapVal, targetType.Elem())
			if err != nil {
				return reflect.Value{}, err
			}
			// return pointer to the struct
			ptrVal := reflect.New(targetType.Elem())
			ptrVal.Elem().Set(structVal)
			return ptrVal, nil
		}
	}

	switch v := val.(type) {
	case *atom.GoObject:
		return convertGoObjectToReflectValue(v, targetType)
	case *atom.Number:
		return convertNumberToReflectValue(v, targetType)
	case atom.String:
		return convertStringToReflectValue(v, targetType)
	case atom.Boolean:
		return convertBooleanToReflectValue(v, targetType)
	case types.Nil:
		return convertNilToReflectValue(targetType)
	default:
		return reflect.Value{}, fmt.Errorf("cannot convert %s to %s", val.Type(), targetType)
	}
}

// MapToStruct converts a doggo map to a reflect.Value of struct type.
func MapToStruct(m *coll.Map, targetType reflect.Type) (reflect.Value, error) {
	if targetType.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("MapToStruct expects a struct type, got %s", targetType.Kind())
	}

	structVal := reflect.New(targetType).Elem()
	if err := PopulateStructFromMap(structVal, m); err != nil {
		return reflect.Value{}, err
	}
	return structVal, nil
}

// PopulateStructFromMap sets fields of a struct value from a doggo map.
func PopulateStructFromMap(structVal reflect.Value, m *coll.Map) error {
	if !structVal.IsValid() || structVal.Kind() != reflect.Struct {
		return fmt.Errorf("PopulateStructFromMap expects a struct value")
	}

	var populateErr error
	m.Range(func(key, value types.Value) bool {
		fieldName, ok := structFieldName(key)
		if !ok {
			populateErr = fmt.Errorf("struct field key must be a keyword or symbol, got %T", key)
			return false
		}

		fieldValue, fieldInfo, err := settableStructField(structVal, fieldName)
		if err != nil {
			populateErr = err
			return false
		}

		converted, err := ConvertDoggoToReflectValue(value, fieldInfo.Type)
		if err != nil {
			populateErr = fmt.Errorf("failed to set field %s: %w", fieldName, err)
			return false
		}
		fieldValue.Set(converted)
		return true
	})
	return populateErr
}

func structFieldName(key types.Value) (string, bool) {
	switch key := key.(type) {
	case atom.Keyword:
		return string(key), true
	case types.Symbol:
		return string(key), true
	case *types.SymbolWithMeta:
		if key != nil {
			return key.Name, true
		}
	case *types.ResolvedSymbol:
		if key != nil {
			return key.Name, true
		}
	}
	return "", false
}

// PopulateStructFromKeywords sets fields of a struct value from keyword arguments.
func PopulateStructFromKeywords(structVal reflect.Value, args []types.Value) error {
	if !structVal.IsValid() || structVal.Kind() != reflect.Struct {
		return fmt.Errorf("PopulateStructFromKeywords expects a struct value")
	}
	if len(args)%2 != 0 {
		return fmt.Errorf("PopulateStructFromKeywords expects even number of arguments, got %d", len(args))
	}

	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(atom.Keyword)
		if !ok {
			return fmt.Errorf("PopulateStructFromKeywords expects keywords as keys, got %T at index %d", args[i], i)
		}
		val := args[i+1]

		fieldName := string(key) // Keyword string is just the name

		field, fieldInfo, err := settableStructField(structVal, fieldName)
		if err != nil {
			return err
		}

		converted, err := ConvertDoggoToReflectValue(val, fieldInfo.Type)
		if err != nil {
			return fmt.Errorf("failed to set field %s: %w", fieldName, err)
		}
		field.Set(converted)
	}
	return nil
}

func settableStructField(structVal reflect.Value, fieldName string) (reflect.Value, reflect.StructField, error) {
	fieldInfo, ok := structVal.Type().FieldByName(fieldName)
	if !ok {
		return reflect.Value{}, reflect.StructField{}, fmt.Errorf("field %s not found on type %s", fieldName, structVal.Type())
	}
	fieldValue, err := structVal.FieldByIndexErr(fieldInfo.Index)
	if err != nil {
		return reflect.Value{}, reflect.StructField{}, fmt.Errorf("field %s cannot be reached: %w", fieldName, err)
	}
	if !fieldValue.CanSet() {
		return reflect.Value{}, reflect.StructField{}, fmt.Errorf("field %s cannot be set", fieldName)
	}
	return fieldValue, fieldInfo, nil
}

// createPointerFromValue creates a pointer from value.
func createPointerFromValue(rv reflect.Value) reflect.Value {
	if rv.CanAddr() {
		return rv.Addr()
	}
	ptrVal := reflect.New(rv.Type())
	ptrVal.Elem().Set(rv)
	return ptrVal
}

// convertGoObjectToReflectValue converts GoObject to reflect.Value.
func convertGoObjectToReflectValue(goObj *atom.GoObject, targetType reflect.Type) (reflect.Value, error) {
	if goObj == nil || goObj.Unwrap() == nil {
		return convertNilToReflectValue(targetType)
	}
	unwrapped := goObj.Unwrap()
	rv := reflect.ValueOf(unwrapped)
	if rv.Type().AssignableTo(targetType) {
		return rv, nil
	}
	if rv.Type().ConvertibleTo(targetType) {
		return rv.Convert(targetType), nil
	}
	if targetType.Kind() == reflect.Interface {
		if rv.Type().Implements(targetType) {
			return rv.Convert(targetType), nil
		}
		if rv.Kind() != reflect.Ptr {
			ptrType := reflect.PointerTo(rv.Type())
			if ptrType.Implements(targetType) {
				ptrVal := createPointerFromValue(rv)
				return ptrVal.Convert(targetType), nil
			}
		}
	}
	return reflect.Value{}, fmt.Errorf("cannot convert %T to %s", unwrapped, targetType)
}

// convertStringToReflectValue converts string to reflect.Value.
func convertStringToReflectValue(str atom.String, targetType reflect.Type) (reflect.Value, error) {
	if targetType.Kind() != reflect.String {
		return reflect.Value{}, fmt.Errorf("cannot convert string to %s", targetType)
	}
	return reflect.ValueOf(string(str)).Convert(targetType), nil
}

// convertBooleanToReflectValue converts boolean to reflect.Value.
func convertBooleanToReflectValue(b atom.Boolean, targetType reflect.Type) (reflect.Value, error) {
	if targetType.Kind() != reflect.Bool {
		return reflect.Value{}, fmt.Errorf("cannot convert boolean to %s", targetType)
	}
	return reflect.ValueOf(bool(b)).Convert(targetType), nil
}

// convertNilToReflectValue converts nil to reflect.Value.
func convertNilToReflectValue(targetType reflect.Type) (reflect.Value, error) {
	if isNilableKind(targetType.Kind()) {
		return reflect.Zero(targetType), nil
	}
	return reflect.Value{}, fmt.Errorf("cannot convert nil to %s", targetType)
}

func isNilableKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice, reflect.UnsafePointer:
		return true
	default:
		return false
	}
}

// convertNumberToReflectValue converts Number to reflect.Value.
// 名前付き型（例: time.Duration）への変換もサポートします。
// Go のリフレクションでは、underlying type が同じでも型が異なる場合は明示的な変換が必要です。
func convertNumberToReflectValue(num *atom.Number, targetType reflect.Type) (reflect.Value, error) {
	switch targetType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var value int64
		if num.IntVal != nil {
			value = *num.IntVal
		} else if num.FloatVal != nil {
			if math.IsNaN(*num.FloatVal) || math.IsInf(*num.FloatVal, 0) {
				return reflect.Value{}, fmt.Errorf("cannot convert non-finite float to %s", targetType)
			}
			converted, accuracy := big.NewFloat(*num.FloatVal).Int64()
			if accuracy != big.Exact {
				return reflect.Value{}, fmt.Errorf("cannot convert float to %s without loss", targetType)
			}
			value = converted
		}
		if targetType.OverflowInt(value) {
			return reflect.Value{}, fmt.Errorf("integer %d overflows %s", value, targetType)
		}
		rv := reflect.New(targetType).Elem()
		rv.SetInt(value)
		return rv, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var value uint64
		if num.IntVal != nil {
			if *num.IntVal < 0 {
				return reflect.Value{}, fmt.Errorf("cannot convert negative number to unsigned type")
			}
			value = uint64(*num.IntVal)
		} else if num.FloatVal != nil {
			if math.IsNaN(*num.FloatVal) || math.IsInf(*num.FloatVal, 0) {
				return reflect.Value{}, fmt.Errorf("cannot convert non-finite float to %s", targetType)
			}
			converted, accuracy := big.NewFloat(*num.FloatVal).Uint64()
			if accuracy != big.Exact {
				return reflect.Value{}, fmt.Errorf("cannot convert float to %s without loss", targetType)
			}
			value = converted
		}
		if targetType.OverflowUint(value) {
			return reflect.Value{}, fmt.Errorf("integer %d overflows %s", value, targetType)
		}
		rv := reflect.New(targetType).Elem()
		rv.SetUint(value)
		return rv, nil
	case reflect.Float32, reflect.Float64:
		if num.FloatVal != nil {
			val := *num.FloatVal
			rv := createFloatReflectValue(val, targetType.Kind())
			return convertToTargetType(rv, targetType), nil
		} else if num.IntVal != nil {
			val := float64(*num.IntVal)
			rv := createFloatReflectValue(val, targetType.Kind())
			return convertToTargetType(rv, targetType), nil
		}
	}

	return reflect.Value{}, fmt.Errorf("cannot convert number to %s", targetType)
}

// createFloatReflectValue は浮動小数点値から適切な型の reflect.Value を作成します。
func createFloatReflectValue(val float64, kind reflect.Kind) reflect.Value {
	if kind == reflect.Float32 {
		return reflect.ValueOf(float32(val))
	}
	return reflect.ValueOf(val)
}

// convertToTargetType は基本型から名前付き型への変換を行います。
// 例: int64 -> time.Duration
func convertToTargetType(rv reflect.Value, targetType reflect.Type) reflect.Value {
	if rv.Type() != targetType && rv.Type().ConvertibleTo(targetType) {
		return rv.Convert(targetType)
	}
	return rv
}

// ConvertArgsToReflectValues converts doggo arguments to reflect.Values.
func ConvertArgsToReflectValues(args []types.Value, fnType reflect.Type) ([]reflect.Value, error) {
	numIn := fnType.NumIn()
	isVariadic := fnType.IsVariadic()

	if !isVariadic {
		if len(args) != numIn {
			return nil, fmt.Errorf("function expects %d arguments, got %d", numIn, len(args))
		}
	} else {
		minArgs := numIn - 1
		if len(args) < minArgs {
			return nil, fmt.Errorf("function expects at least %d arguments, got %d", minArgs, len(args))
		}
	}

	reflectArgs := make([]reflect.Value, len(args))
	for i := 0; i < len(args); i++ {
		var expectedType reflect.Type
		if isVariadic && i >= numIn-1 {
			expectedType = fnType.In(numIn - 1).Elem()
		} else {
			expectedType = fnType.In(i)
		}
		converted, err := ConvertDoggoToReflectValue(args[i], expectedType)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i, err)
		}
		reflectArgs[i] = converted
	}

	return reflectArgs, nil
}
