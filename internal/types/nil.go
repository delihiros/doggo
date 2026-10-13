package types

// Nil is Doggo's language-level nil value.
type Nil struct{}

func (Nil) String() string         { return "nil" }
func (Nil) Type() string           { return "nil" }
func (Nil) Hash() uint32           { return 0 }
func (Nil) IsSelfEvaluating() bool { return true }
func (Nil) Equals(other Value) bool {
	if other == nil {
		return true
	}
	_, ok := other.(Nil)
	return ok
}

var NilValue Nil
