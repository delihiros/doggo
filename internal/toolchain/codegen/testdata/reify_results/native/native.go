package native

import (
	"doggo/reifyprobe/aliases"
	"doggo/reifyprobe/locals"
	"doggo/reifyprobe/valueslocals"
)

type Checker interface{ Check() (int, error) }
type Count int8
type Values interface {
	Values() (Count, *int, []int, error)
}
type Plain interface{ Fail() error }
type Alias interface{ Fail() unwrapResults.Error }
type Chain interface{ Fail() unwrapResults.Chain }
type AliasPair interface {
	Check() (int, unwrapResults.Chain)
}
type Arguments interface {
	Fail(string) unwrapResults.Chain
}
type NoError interface {
	Single() int
	Pair() (int, string)
}
type ConcreteError string

func (e ConcreteError) Error() string { return string(e) }

type Concrete interface{ Fail() ConcreteError }
type Variadic interface {
	Join(result int, err ...int) (int, unwrapResults.Error)
}
type MultiError interface {
	Check() (error, int, unwrapResults.Chain)
}
type Direct interface{ Direct() Value }
type Value interface{ Type() string }
type Any interface{ Any() any }
type LocalCollision interface {
	Check() (result0.Token, result0.Error)
}
type ValuesCollision interface {
	Check() (resultValues.Token, resultValues.Error)
}
