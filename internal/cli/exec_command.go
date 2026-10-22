package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
)

var beforeExecChildStartForTest func()

// ChildExitError reports termination of the binary launched by doggo exec.
type ChildExitError struct {
	err *exec.ExitError
}

func (e *ChildExitError) Error() string {
	if e == nil || e.err == nil {
		return "child process exited"
	}
	return e.err.Error()
}

func (e *ChildExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *ChildExitError) ExitCode() int {
	if e == nil || e.err == nil {
		return 1
	}
	return execChildExitCode(e.err)
}

func (app *App) runExecCommand() (err error) {
	if len(app.args) < 3 || app.args[1] != "--" {
		return fmt.Errorf("usage: doggo exec -- <command> [arguments...]")
	}
	args := app.args[2:]
	if writer := execGenerationWriter(args); writer != "" {
		return fmt.Errorf("doggo exec cannot run generation-writing command %q; run 'doggo %s' directly", writer, writer)
	}
	session, err := defaultOpenGenerationSession(execProjectHint(args))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	if !session.HasBinary() {
		return fmt.Errorf("current generation has no executable; run 'doggo build' first")
	}

	command, err := session.BinaryCommand(args...)
	if err != nil {
		return err
	}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = os.Environ()
	return startAndWaitExecChild(command)
}

// execChildDispatchArgs follows Run/Dispatch through global flags and nested
// exec wrappers without profiling, generation access, or child diagnostics.
func execChildDispatchArgs(args []string) []string {
	for {
		child := &App{args: args}
		normalized, err := child.parseGlobalFlagsWithOutput(io.Discard)
		if err != nil || len(normalized) == 0 {
			return nil
		}
		command := DefaultCLIRegistry().Lookup(normalized[0])
		if command == nil || command.Name != "exec" {
			return normalized
		}
		if len(normalized) < 3 || normalized[1] != "--" {
			return nil
		}
		args = normalized[2:]
	}
}

func execProjectHint(args []string) string {
	normalized := execChildDispatchArgs(args)
	if len(normalized) == 0 {
		return ""
	}
	command := normalized[0]
	if command == "" || command[0] == '-' || DefaultCLIRegistry().Lookup(command) != nil {
		return ""
	}
	return command
}

// execGenerationWriter rejects the child's generation writer before opening
// the retained runtime session, while leaving invalid/help diagnostics to the child.
func execGenerationWriter(args []string) string {
	normalized := execChildDispatchArgs(args)
	if len(normalized) == 0 {
		return ""
	}
	command := DefaultCLIRegistry().Lookup(normalized[0])
	if command == nil {
		return "" // Unregistered names dispatch as script paths.
	}
	if command.WritesGeneration {
		if command.Name == "build" {
			if _, err := parseBuildCommandArgsWithOutput(normalized[1:], io.Discard); err != nil {
				return ""
			}
		}
		return command.Name
	}
	var subcommands *cliSubcommandRegistry
	switch command.Name {
	case "mod":
		subcommands = defaultModSubcommandRegistry()
	case "deps":
		subcommands = defaultDepsSubcommandRegistry()
	default:
		return ""
	}
	if len(normalized) >= 2 {
		if subcommand := subcommands.Lookup(normalized[1]); subcommand != nil && subcommand.WritesGeneration {
			return command.Name + " " + subcommand.Name
		}
	}
	return ""
}

func startAndWaitExecChild(command *exec.Cmd) error {
	signals := execForwardSignals()
	forwarded := make(chan os.Signal, 1)
	signal.Notify(forwarded, signals...)
	if beforeExecChildStartForTest != nil {
		beforeExecChildStartForTest()
	}
	if err := command.Start(); err != nil {
		signal.Stop(forwarded)
		return err
	}

	done := make(chan struct{})
	forwardDone := make(chan error, 1)
	go func() {
		for {
			select {
			case forwardedSignal := <-forwarded:
				if err := forwardExecSignal(command.Process, forwardedSignal); err != nil && !errors.Is(err, os.ErrProcessDone) {
					forwardDone <- fmt.Errorf("forward %s to child: %w", forwardedSignal, err)
					return
				}
			case <-done:
				forwardDone <- nil
				return
			}
		}
	}()
	waitErr := command.Wait()
	signal.Stop(forwarded)
	close(done)
	forwardErr := <-forwardDone
	var exitError *exec.ExitError
	if errors.As(waitErr, &exitError) {
		waitErr = &ChildExitError{err: exitError}
	}
	return errors.Join(waitErr, forwardErr)
}
