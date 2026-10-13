package atom

import (
	"fmt"
	"strings"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// UUID is a canonical EDN/UUID value.
type UUID struct {
	value string
}

func (u *UUID) String() string {
	if u == nil {
		return `#uuid ""`
	}
	return `#uuid "` + u.value + `"`
}

func (u *UUID) Display() string {
	if u == nil {
		return ""
	}
	return u.value
}

func (u *UUID) Type() string {
	return "uuid"
}

func (u *UUID) Hash() uint32 {
	if u == nil {
		return 0
	}
	return util.HashString(u.value)
}

func (u *UUID) Equals(other types.Value) bool {
	if u == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	otherUUID, ok := other.(*UUID)
	if !ok {
		return false
	}
	return u.value == otherUUID.value
}

func (u *UUID) IsSelfEvaluating() bool {
	return true
}

func (u *UUID) Value() string {
	if u == nil {
		return ""
	}
	return u.value
}

func NewUUID(input string) (*UUID, error) {
	canonical, err := canonicalUUID(input)
	if err != nil {
		return nil, err
	}
	return &UUID{value: canonical}, nil
}

func canonicalUUID(input string) (string, error) {
	if len(input) != 36 {
		return "", fmt.Errorf("invalid UUID string: %s", input)
	}

	var builder strings.Builder
	builder.Grow(36)
	for i := range input {
		ch := input[i]
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return "", fmt.Errorf("invalid UUID string: %s", input)
			}
			builder.WriteByte('-')
		default:
			if !isHexByte(ch) {
				return "", fmt.Errorf("invalid UUID string: %s", input)
			}
			builder.WriteByte(lowerHexByte(ch))
		}
	}

	return builder.String(), nil
}

func isHexByte(ch byte) bool {
	return ('0' <= ch && ch <= '9') ||
		('a' <= ch && ch <= 'f') ||
		('A' <= ch && ch <= 'F')
}

func lowerHexByte(ch byte) byte {
	if 'A' <= ch && ch <= 'F' {
		return ch + ('a' - 'A')
	}
	return ch
}
