package services

import (
	"doggo/internal/types"
)

type ExecutionServices interface {
	EvalString(string) (types.Value, error)
}
