package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"doggo/internal/runtime/profile"
	"doggo/internal/testutil"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestRuntimeRunnerBuildsVMExecutionFromProfileFeatures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("hello from runtime"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	r, err := New(Config{
		ProjectRoot: t.TempDir(),
		Profile:     profile.New("file-read").SetFeatures([]profile.Feature{profile.FeatureFileRead}),
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.EvalString(fmt.Sprintf("(slurp %q)", path))
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "\"hello from runtime\"" {
		t.Fatalf("unexpected result: %s", got.String())
	}

	if _, err := r.EvalString("(count [1 2])"); err == nil {
		t.Fatal("expected non-IO builtin to stay unavailable")
	}
}

func TestRuntimeRunnerSupportsProjectRootLoadPathsAndBootstrapCore(t *testing.T) {
	projectRoot := testutil.CanonicalTempDir(t)

	r, err := New(Config{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{"lib"},
		BootstrapCore: true,
		Profile:       profile.New("runtime-owned").SetFeatures([]profile.Feature{profile.FeatureFileRead}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected runner")
	}
	currentBasis, err := r.services.Basis.Current()
	if err != nil {
		t.Fatalf("basis current: %v", err)
	}
	if got := basisProjectRoot(t, currentBasis); got != filepath.ToSlash(projectRoot) {
		t.Fatalf("basis project root = %q, want %q", got, filepath.ToSlash(projectRoot))
	}
	wantLoadPaths := []string{filepath.ToSlash(filepath.Join(projectRoot, "lib"))}
	if got := basisLoadPaths(t, currentBasis); !reflect.DeepEqual(got, wantLoadPaths) {
		t.Fatalf("basis load paths = %v, want %v", got, wantLoadPaths)
	}
}

func TestRuntimeRunnerRejectsUnknownProfileFeature(t *testing.T) {
	_, err := New(Config{
		ProjectRoot: t.TempDir(),
		Profile:     profile.New("bad").SetFeatures([]profile.Feature{"no-such-feature"}),
	})
	if err == nil || !strings.Contains(err.Error(), `unknown runtime profile feature "no-such-feature"`) {
		t.Fatalf("expected unknown feature error, got %v", err)
	}
}

func TestRunnerNewBuildsPracticalPureStandardByDefault(t *testing.T) {
	r, err := New(Config{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{"lib"},
		BootstrapCore: true,
		Profile:       nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.EvalString("(count [1 2 3])")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "3" {
		t.Fatalf("unexpected result: %s", got)
	}
	if _, err := r.EvalString(`(slurp "sample.txt")`); err == nil {
		t.Fatal("expected standard runtime defaults to keep slurp unavailable")
	}
}

func basisProjectRoot(t *testing.T, value types.Value) string {
	t.Helper()

	basisMap, ok := value.(*coll.Map)
	if !ok {
		t.Fatalf("expected basis map, got %T", value)
	}
	rootValue := basisMap.Get(atom.NewKeyword("project-root"))
	root, ok := rootValue.(atom.String)
	if !ok {
		t.Fatalf("expected project-root string, got %T", rootValue)
	}
	return string(root)
}

func basisLoadPaths(t *testing.T, value types.Value) []string {
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
