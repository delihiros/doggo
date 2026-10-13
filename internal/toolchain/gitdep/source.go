package gitdep

import (
	"fmt"

	"doggo/internal/depsource"
)

// Source is a parsed git dependency source.
type Source struct {
	Repo string
	Ref  string
}

// ParseSource separates a git dependency source into repository and optional ref.
func ParseSource(source string) (Source, error) {
	parsed, err := depsource.Parse(source)
	if err != nil {
		return Source{}, err
	}
	if parsed.Kind != depsource.Git {
		return Source{}, fmt.Errorf("source %q is not a git source", source)
	}
	return Source{Repo: parsed.Repository, Ref: parsed.Ref}, nil
}

// IdentityKey returns the source identity without a ref pin.
func IdentityKey(source string) string {
	parsed, err := depsource.Parse(source)
	if err != nil || parsed.Kind != depsource.Git {
		return ""
	}
	return parsed.Repository
}
