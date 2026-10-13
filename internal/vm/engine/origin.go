package engine

import "doggo/internal/project"

// SourceOrigin describes where a piece of source code came from.
type SourceOrigin struct {
	File             string
	GenerationSource *project.GenerationSource
	RelativePath     string
}

// FileOrUnknown returns the file name or a diagnostic placeholder when it is empty.
func (o SourceOrigin) FileOrUnknown() string {
	if o.File == "" {
		return "<unknown>"
	}
	return o.File
}
