package interop

import (
	"fmt"
	"reflect"
)

// CallReflect converts panics at the Go boundary into ordinary Doggo errors.
func CallReflect(fn reflect.Value, args []reflect.Value) (results []reflect.Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			results = nil
			err = fmt.Errorf("Go call panicked: %v", recovered)
		}
	}()
	return fn.Call(args), nil
}
