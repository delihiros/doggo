package services

import (
	"path/filepath"
	"testing"

	"doggo/internal/project"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/vm/engine"
)

func TestRuntimeGraphConfigAccessorsReturnDefensivePathSliceCopies(t *testing.T) {
	source := &project.GenerationSource{}
	graph := NewGraph(engine.RuntimeConfig{
		LoadPaths:         []string{"live"},
		GenerationSources: []*project.GenerationSource{source},
	})

	engineConfig := graph.EngineConfig()
	if engineConfig.GenerationSources[0] != source {
		t.Fatal("engine config cloned generation source object")
	}
	engineConfig.LoadPaths[0] = "replacement"
	engineConfig.GenerationSources[0] = &project.GenerationSource{}
	if got := graph.EngineConfig(); got.LoadPaths[0] != "live" || got.GenerationSources[0] != source {
		t.Fatalf("EngineConfig exposed graph slices: %#v %#v", got.LoadPaths, got.GenerationSources)
	}

	handles := graph.Handles()
	if handles.Engine.GenerationSources[0] != source {
		t.Fatal("graph handles cloned generation source object")
	}
	handles.Engine.LoadPaths[0] = "replacement"
	handles.Engine.GenerationSources[0] = &project.GenerationSource{}
	if got := graph.EngineConfig(); got.LoadPaths[0] != "live" || got.GenerationSources[0] != source {
		t.Fatalf("Handles exposed graph slices: %#v %#v", got.LoadPaths, got.GenerationSources)
	}
}

func TestRuntimeGraphSharesNamespaceAndBasisState(t *testing.T) {
	projectRoot := t.TempDir()
	vendorPath := filepath.Join(projectRoot, "vendor")

	graph := NewGraph(engine.RuntimeConfig{
		ProjectRoot: projectRoot,
		LoadPaths:   []string{projectRoot, vendorPath},
	})

	current, err := graph.Services().Basis.Current()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.ToSlash(projectRoot),
		filepath.ToSlash(vendorPath),
	}
	got := graphBasisLoadPaths(t, current)
	if len(got) != len(want) {
		t.Fatalf("load path count = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("loadPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func graphBasisLoadPaths(t *testing.T, value types.Value) []string {
	t.Helper()

	basisMap, ok := value.(*coll.Map)
	if !ok {
		t.Fatalf("expected basis map, got %T", value)
	}
	loadPathsValue := basisMap.Get(atom.NewKeyword("load-paths"))
	loadPaths, ok := loadPathsValue.(*coll.Vector)
	if !ok {
		t.Fatalf("expected load-path vector, got %T", loadPathsValue)
	}
	out := make([]string, 0, len(loadPaths.Items))
	for _, item := range loadPaths.Items {
		str, ok := item.(atom.String)
		if !ok {
			t.Fatalf("expected load-path string, got %T", item)
		}
		out = append(out, string(str))
	}
	return out
}
