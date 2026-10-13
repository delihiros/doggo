package vm

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func TestOSNamespaceReportsRuntimeLookupFailures(t *testing.T) {
	oldGetwd := osBuiltinGetwd
	oldUserHomeDir := osBuiltinUserHomeDir
	oldHostname := osBuiltinHostname
	t.Cleanup(func() {
		osBuiltinGetwd = oldGetwd
		osBuiltinUserHomeDir = oldUserHomeDir
		osBuiltinHostname = oldHostname
	})

	osBuiltinGetwd = func() (string, error) { return "", errors.New("cwd unavailable") }
	osBuiltinUserHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
	osBuiltinHostname = func() (string, error) { return "", errors.New("host unavailable") }

	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.os :as os])`); err != nil {
		t.Fatal(err)
	}
	for _, function := range []string{"cwd", "user-home", "hostname"} {
		value, err := runner.Run(`(try (os/` + function + `) (catch [e] (ex-data e)))`)
		if err != nil {
			t.Fatalf("%s: %v", function, err)
		}
		data := mustMap(t, value)
		if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "runtime-failure" {
			t.Errorf("%s kind = %s", function, got)
		}
		wantFunction := "doggo.os/" + function
		if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != wantFunction {
			t.Errorf("%s function = %v", function, data.Get(atom.NewKeyword("function")))
		}
	}
}

func TestOSNamespaceWhichHandlesHostResults(t *testing.T) {
	oldLookPath := osBuiltinLookPath
	t.Cleanup(func() {
		osBuiltinLookPath = oldLookPath
	})

	toolPath := filepath.Join("tools", "doggo-tool")
	osBuiltinLookPath = func(file string) (string, error) {
		switch file {
		case "doggo-tool":
			return toolPath, nil
		case "missing-tool":
			return "", exec.ErrNotFound
		default:
			return "", errors.New("lookup failed")
		}
	}

	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.os :as os])
		[(os/which "doggo-tool") (os/which "missing-tool")]
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != `["`+toolPath+`" nil]` {
		t.Fatalf("which = %s", got)
	}
	if _, err := runner.Run(`(os/which "broken-tool")`); err == nil {
		t.Fatal("broken lookup succeeded")
	}
}

func TestOSNamespacePreservesLiteralBackslashesOnUnix(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("Windows uses backslash as a path separator")
	}

	oldGetwd := osBuiltinGetwd
	t.Cleanup(func() {
		osBuiltinGetwd = oldGetwd
	})
	osBuiltinGetwd = func() (string, error) {
		return `/tmp/doggo\literal`, nil
	}

	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.os :as os])
		(os/cwd)
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != `"/tmp/doggo\literal"` {
		t.Fatalf("cwd = %s", got)
	}
}
