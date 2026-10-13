package gohelpers

import (
	"reflect"

	doggoerr "doggo/internal/errors"
	"doggo/internal/interop"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// ConvertResults はreflect.Valueスライスをdoggo値に変換する
func ConvertResults(values []reflect.Value) (types.Value, error) {
	if len(values) == 0 {
		return atom.NilValue, nil
	}

	if len(values) == 1 {
		return interop.ConvertReflectValueToDoggo(values[0])
	}

	lastValue := values[len(values)-1]
	hasErrorReturn := false
	if lastValue.IsValid() {
		errorType := reflect.TypeOf((*error)(nil)).Elem()
		if lastValue.Type().Implements(errorType) {
			hasErrorReturn = true
			if !lastValue.IsNil() {
				errVal := lastValue.Interface().(error)
				return nil, doggoerr.Wrap(doggoerr.KindRuntime, errVal, "go function error")
			}
		}
	}

	if hasErrorReturn && len(values) == 2 {
		return interop.ConvertReflectValueToDoggo(values[0])
	}

	return ResultsToVector(values)
}

// ResultsToVector はreflect.ValueスライスをVectorに変換する
func ResultsToVector(values []reflect.Value) (types.Value, error) {
	doggoValues := make([]types.Value, len(values))
	for i, val := range values {
		converted, err := interop.ConvertReflectValueToDoggo(val)
		if err != nil {
			return nil, err
		}
		doggoValues[i] = converted
	}
	return coll.NewVector(doggoValues...), nil
}
