package cli

import (
	"doggo/internal/vm"
	"errors"
)

var runREPLWithRunner = vm.RunREPLWithRunner

// RunREPL はREPLを起動します。
func (app *App) RunREPL() (err error) {
	session, err := defaultOpenGenerationSession("")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	runner, err := newStandardVMRunner(session.Config, true)
	if err != nil {
		return err
	}
	return runREPLWithRunner(runner)
}
