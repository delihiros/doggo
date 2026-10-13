package compiler

// diagnosticLocationReader provides the current compiler source location.
type diagnosticLocationReader interface {
	EffectiveSourceLocation() (string, int, int)
}

// diagnosticOwner assembles compile errors from the current source location
// and the configured source line provider.
type diagnosticOwner struct {
	locationReader diagnosticLocationReader
	lineProvider   func(file string, line int) (string, bool)
}

func newDiagnosticOwner(locationReader diagnosticLocationReader, lineProvider func(file string, line int) (string, bool)) *diagnosticOwner {
	return &diagnosticOwner{
		locationReader: locationReader,
		lineProvider:   lineProvider,
	}
}

func (d *diagnosticOwner) SetSourceLineProvider(provider func(file string, line int) (string, bool)) {
	if d == nil {
		return
	}
	d.lineProvider = provider
}

func (d *diagnosticOwner) Errorf(format string, args ...interface{}) *CompileError {
	if d == nil {
		return newCompileErrorWithContext("", 0, 0, format, args...)
	}

	file, line, column := "", 0, 0
	if d.locationReader != nil {
		file, line, column = d.locationReader.EffectiveSourceLocation()
	}

	err := newCompileErrorWithContext(file, line, column, format, args...)
	if d.lineProvider == nil || line <= 0 {
		return err
	}

	lookupFile := file
	if lookupFile == "" {
		lookupFile = "<unknown>"
	}
	if snippet, ok := d.lineProvider(lookupFile, line); ok {
		col := column
		if col <= 0 {
			col = 1
		}
		err.SetSnippet(snippet, col)
	}

	return err
}
