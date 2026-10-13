package coll

import (
	"fmt"

	"doggo/internal/types"
)

func invokeMapLookup(typeName string, get func(types.Value) types.Value, args ...types.Value) (types.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%s invocation requires at least 1 argument (key)", typeName)
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("%s invocation expects 1 or 2 arguments (key, default?), got %d", typeName, len(args))
	}

	val := get(args[0])
	if val == nil {
		if len(args) == 2 {
			return args[1], nil
		}
		return nil, nil
	}
	return val, nil
}
