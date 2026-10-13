package vm

import (
	"context"
	"fmt"
	"strings"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/vm/engine"
)

type commandLineArgsFn struct {
	runner *Runner
}

func (c *commandLineArgsFn) String() string {
	return "#<builtin:command-line-args*>"
}

func (c *commandLineArgsFn) Type() string { return "function" }
func (c *commandLineArgsFn) Hash() uint32 { return 0 }
func (c *commandLineArgsFn) Equals(other types.Value) bool {
	o, ok := other.(*commandLineArgsFn)
	return ok && c == o
}
func (c *commandLineArgsFn) IsSelfEvaluating() bool { return true }

func (c *commandLineArgsFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s: expected 0 arguments, got %d", c.String(), len(args))
	}
	return stringsToArgsVector(c.runner.commandLineArgs), nil
}

type entryPointFn struct {
	runner *Runner
}

func (e *entryPointFn) String() string {
	return "#<builtin:entrypoint?*>"
}

func (e *entryPointFn) Type() string { return "function" }
func (e *entryPointFn) Hash() uint32 { return 0 }
func (e *entryPointFn) Equals(other types.Value) bool {
	o, ok := other.(*entryPointFn)
	return ok && e == o
}
func (e *entryPointFn) IsSelfEvaluating() bool { return true }

func (e *entryPointFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s: expected 0 arguments, got %d", e.String(), len(args))
	}
	origin := currentSourceOrigin(ctx, e.runner)
	return atom.NewBoolean(e.runner.isEntryPointOrigin(origin)), nil
}

func currentSourceOrigin(ctx context.Context, runner *Runner) engine.SourceOrigin {
	if vm := GetVM(ctx); vm != nil && vm.frameIndex > 0 {
		for i := vm.frameIndex - 1; i >= 0; i-- {
			frame := vm.frames[i]
			if frame == nil {
				continue
			}
			if strings.TrimSpace(frame.CurrentNamespace()) == "doggo.cli" {
				continue
			}
			if file := strings.TrimSpace(frame.CurrentFile()); file != "" {
				if runner != nil {
					if origin, ok := runner.resolveSourceOrigin(file); ok {
						return origin
					}
				}
				return engine.SourceOrigin{File: file}
			}
		}
	}
	if origin, ok := sourceOriginFromContext(ctx); ok {
		return origin
	}
	return engine.SourceOrigin{}
}

func stringsToArgsVector(args []string) types.Value {
	if args == nil {
		return atom.NilValue
	}
	items := make([]types.Value, 0, len(args))
	for _, arg := range args {
		items = append(items, atom.NewString(arg))
	}
	return coll.NewVector(items...)
}
