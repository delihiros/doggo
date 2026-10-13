package vm

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"doggo/stdlib"
)

func TestEmbeddedStdlibNamespacesLoad(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: true,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	assertRunResult(t, runner, `(do (defn twice [x] (* x 2)) (twice 5))`, "10")

	files, err := fs.Glob(stdlib.StdlibFS, "doggo/*.dog")
	if err != nil {
		t.Fatalf("list embedded stdlib: %v", err)
	}
	for _, file := range files {
		if file == "doggo/core.dog" || strings.HasSuffix(file, "_test.dog") {
			continue
		}
		namespace := "doggo." + strings.TrimSuffix(path.Base(file), ".dog")
		t.Run(namespace, func(t *testing.T) {
			if _, err := runner.Run("(require '" + namespace + ")"); err != nil {
				t.Fatalf("require %s from embedded stdlib: %v", namespace, err)
			}
		})
	}
}
