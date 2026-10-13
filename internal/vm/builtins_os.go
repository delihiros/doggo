package vm

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var (
	osBuiltinLookupEnv   = os.LookupEnv
	osBuiltinEnviron     = os.Environ
	osBuiltinSetenv      = os.Setenv
	osBuiltinUnsetenv    = os.Unsetenv
	osBuiltinGetwd       = os.Getwd
	osBuiltinUserHomeDir = os.UserHomeDir
	osBuiltinTempDir     = os.TempDir
	osBuiltinHostname    = os.Hostname
	osBuiltinName        = func() string { return runtime.GOOS }
	osBuiltinLookPath    = exec.LookPath
)

func osHostPath(path string) string {
	return osHostPathForGOOS(osBuiltinName(), path)
}

func osHostPathForGOOS(_ string, path string) string {
	return path
}

func osStringArg(fnName string, arg types.Value) (string, types.Value) {
	value, ok := arg.(atom.String)
	if !ok {
		return "", newArgumentError("%s: expected string argument, got %s", fnName, arg.Type())
	}
	return string(value), nil
}

var builtinsOS = []BuiltinDefinition{
	{
		Name:       "os-getenv*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("getenv", args, 1); err != nil {
				return err
			}
			key, errValue := osStringArg("getenv", args[0])
			if errValue != nil {
				return errValue
			}
			value, ok := osBuiltinLookupEnv(key)
			if !ok {
				return atom.NilValue
			}
			return atom.NewString(value)
		}},
	},
	{
		Name:       "os-setenv*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("setenv", args, 2); err != nil {
				return err
			}
			key, errValue := osStringArg("setenv", args[0])
			if errValue != nil {
				return errValue
			}
			value, errValue := osStringArg("setenv", args[1])
			if errValue != nil {
				return errValue
			}
			if err := osBuiltinSetenv(key, value); err != nil {
				return newArgumentError("setenv: %v", err)
			}
			return atom.NilValue
		}},
	},
	{
		Name:       "os-env*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("env", args, 0); err != nil {
				return err
			}
			entries := osBuiltinEnviron()
			sort.Strings(entries)
			pairs := make([]types.Value, 0, len(entries)*2)
			for _, entry := range entries {
				key, value := osSplitEnvEntry(entry)
				pairs = append(pairs, atom.NewString(key), atom.NewString(value))
			}
			return coll.NewMapFromPairs(pairs)
		}},
	},
	{
		Name:       "os-unsetenv*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("unsetenv", args, 1); err != nil {
				return err
			}
			key, errValue := osStringArg("unsetenv", args[0])
			if errValue != nil {
				return errValue
			}
			if err := osBuiltinUnsetenv(key); err != nil {
				return newArgumentError("unsetenv: %v", err)
			}
			return atom.NilValue
		}},
	},
	{
		Name:       "os-cwd*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("cwd", args, 0); err != nil {
				return err
			}
			value, err := osBuiltinGetwd()
			if err != nil {
				return RuntimeErrf("cwd", "%v", err)
			}
			return atom.NewString(osHostPath(value))
		}},
	},
	{
		Name:       "os-user-home*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("user-home", args, 0); err != nil {
				return err
			}
			value, err := osBuiltinUserHomeDir()
			if err != nil {
				return RuntimeErrf("user-home", "%v", err)
			}
			return atom.NewString(osHostPath(value))
		}},
	},
	{
		Name:       "os-system-temp-dir*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("temp-dir", args, 0); err != nil {
				return err
			}
			return atom.NewString(osHostPath(osBuiltinTempDir()))
		}},
	},
	{
		Name:       "os-hostname*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("hostname", args, 0); err != nil {
				return err
			}
			value, err := osBuiltinHostname()
			if err != nil {
				return RuntimeErrf("hostname", "%v", err)
			}
			return atom.NewString(value)
		}},
	},
	{
		Name:       "os-name*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("os-name", args, 0); err != nil {
				return err
			}
			return atom.NewString(osBuiltinName())
		}},
	},
	{
		Name:       "os-which*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("which", args, 1); err != nil {
				return err
			}
			name, errValue := osStringArg("which", args[0])
			if errValue != nil {
				return errValue
			}
			value, err := osBuiltinLookPath(name)
			if err != nil {
				if errors.Is(err, exec.ErrNotFound) {
					return atom.NilValue
				}
				return RuntimeErrf("which", "%v", err)
			}
			return atom.NewString(osHostPath(value))
		}},
	},
}

func osSplitEnvEntry(entry string) (string, string) {
	index := strings.IndexByte(entry, '=')
	if index < 0 {
		return entry, ""
	}
	return entry[:index], entry[index+1:]
}
