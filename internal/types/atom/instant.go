package atom

import (
	"fmt"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// Instant is a Clojure-compatible EDN #inst value.
// It stores epoch milliseconds only, matching java.util.Date semantics.
type Instant struct {
	epochMillis int64
}

func NewInstantFromTime(value time.Time) *Instant {
	return &Instant{epochMillis: value.UTC().UnixMilli()}
}

func NewInstantFromEpochMillis(epochMillis int64) *Instant {
	return &Instant{epochMillis: epochMillis}
}

func (i *Instant) String() string {
	if i == nil {
		return `#inst "1970-01-01T00:00:00.000-00:00"`
	}
	return `#inst "` + i.EDNString() + `"`
}

func (i *Instant) Display() string {
	if i == nil {
		return ""
	}
	return i.EDNString()
}

func (i *Instant) Type() string {
	return "instant"
}

func (i *Instant) Hash() uint32 {
	if i == nil {
		return 0
	}
	return util.HashString(fmt.Sprintf("%d", i.epochMillis))
}

func (i *Instant) Equals(other types.Value) bool {
	if i == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	otherInstant, ok := other.(*Instant)
	if !ok {
		return false
	}
	return i.epochMillis == otherInstant.epochMillis
}

func (i *Instant) IsSelfEvaluating() bool {
	return true
}

func (i *Instant) EpochMillis() int64 {
	if i == nil {
		return 0
	}
	return i.epochMillis
}

func (i *Instant) Time() time.Time {
	if i == nil {
		return time.UnixMilli(0).UTC()
	}
	return time.UnixMilli(i.epochMillis).UTC()
}

func (i *Instant) EDNString() string {
	return i.Time().Format("2006-01-02T15:04:05.000") + "-00:00"
}
