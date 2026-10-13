package engine

import (
	"path/filepath"
	"testing"

	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestBasisServicesCaptureInitialAndCurrentSnapshots(t *testing.T) {
	projectRoot := t.TempDir()
	loader := &stubNamespaceLoader{
		root:      projectRoot,
		loadPaths: []string{projectRoot},
	}
	svcs := NewBasisServices(loader, nil)
	svcs.CaptureInitial()

	current, err := svcs.Current()
	if err != nil {
		t.Fatal(err)
	}
	initial, err := svcs.Initial()
	if err != nil {
		t.Fatal(err)
	}

	currentMap := current.(*coll.Map)
	initialMap := initial.(*coll.Map)
	currentPaths := currentMap.Get(atom.NewKeyword("load-paths")).(*coll.Vector)
	initialPaths := initialMap.Get(atom.NewKeyword("load-paths")).(*coll.Vector)
	if len(currentPaths.Items) != 1 {
		t.Fatalf("current load path len = %d", len(currentPaths.Items))
	}
	if len(initialPaths.Items) != 1 {
		t.Fatalf("initial load path len = %d", len(initialPaths.Items))
	}
	if currentPaths.Items[0] != atom.NewString(filepath.ToSlash(projectRoot)) {
		t.Fatalf("unexpected current path: %s", currentPaths.Items[0].String())
	}
	targets := currentMap.Get(atom.NewKeyword("generated-targets")).(*coll.Map)
	reifyTargets, ok := targets.Get(atom.NewKeyword("reify-targets")).(*coll.Vector)
	if !ok || len(reifyTargets.Items) != 0 {
		t.Fatalf("unexpected empty reify-targets: %T %v", targets.Get(atom.NewKeyword("reify-targets")), targets.Get(atom.NewKeyword("reify-targets")))
	}
	if removed := targets.Get(atom.NewKeyword("reify-interfaces")); removed != nil {
		t.Fatalf("unexpected removed reify-interfaces: %v", removed)
	}
}
