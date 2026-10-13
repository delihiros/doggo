package vm

import (
	"strings"
	"testing"

	"doggo/internal/vm/engine"
)

func TestNewExactEngineRunnerDefersRuntimeInitialization(t *testing.T) {
	projectRoot := t.TempDir()
	namespaces := engine.NewNamespaceServices(engine.NamespaceConfig{
		ProjectRoot: projectRoot,
		LoadPaths:   []string{projectRoot},
	})

	exec, err := NewExactEngineRunner(
		NewRuntimeProfile("exact").
			SetBuiltinCatalog(DefaultBuiltinCatalog()).
			SetFeatures([]RuntimeFeature{RuntimeFeatureFileLoad}),
		false,
		namespaces,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if exec.Runner == nil {
		t.Fatal("expected exact execution to expose a VM runner")
	}
	if exec.Sources == nil {
		t.Fatal("expected exact execution to expose a source registry")
	}

	if _, err := exec.Runner.Run("load-file"); err == nil || !strings.Contains(err.Error(), "undefined variable: load-file") {
		t.Fatalf("expected deferred runtime initialization, got %v", err)
	}
}
