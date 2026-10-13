package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/runtime/composition"
	runtime_profile "doggo/internal/runtime/profile"
	"doggo/internal/runtime/services"
	"doggo/internal/testutil"
)

func newRealVMRunner(t *testing.T, projectRoot string, loadPaths []string, bootstrapCore bool, prof *runtime_profile.Profile) *Runner {
	t.Helper()

	graph := services.NewGraph(composition.StandardEngineConfig(composition.StandardConfig{
		ProjectRoot:   projectRoot,
		LoadPaths:     loadPaths,
		BootstrapCore: bootstrapCore,
		Profile:       prof,
	}))
	r, err := newWithGraph(prof, graph)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRuntimeRunnerRealVMPathUsesConfiguredLoadPathsAcrossServicesAndExecution(t *testing.T) {
	projectRoot := writeRuntimeBasisTestProject(t)
	dependencyPath := filepath.Join(projectRoot, "deps", "dep-one")

	r := newRealVMRunner(t, projectRoot, []string{projectRoot, dependencyPath}, true, runtime_profile.Default())

	current, err := r.Basis().Current()
	if err != nil {
		t.Fatal(err)
	}
	wantLoadPaths := []string{
		filepath.ToSlash(projectRoot),
		filepath.ToSlash(dependencyPath),
	}
	if got := basisLoadPaths(t, current); !reflect.DeepEqual(got, wantLoadPaths) {
		t.Fatalf("basis load paths mismatch: got=%v want=%v", got, wantLoadPaths)
	}

	if _, err := r.EvalString(`(require '[dep-one.core :as dep])`); err != nil {
		t.Fatalf("execution did not observe configured load paths: %v", err)
	}
	got, err := r.EvalString(`dep-one.core/answer`)
	if err != nil {
		t.Fatalf("dependency namespace should stay loaded after require: %v", err)
	}
	if got.String() != "42" {
		t.Fatalf("unexpected dependency answer: %s", got.String())
	}
}

func TestRuntimeRunnerRealVMPathPreservesGenerationBasis(t *testing.T) {
	projectRoot := writeRuntimeBasisTestProject(t)
	lock := &project.LockFile{
		Deps: []project.LockDep{{Source: "pkg:acme/dep-one@1.2.3"}},
		Scan: project.LockScan{ReifyTargets: []project.ReifyTarget{{
			Source:  "fmt.Stringer",
			Runtime: "fmt.Stringer",
		}}},
		Build: project.LockBuild{Binary: project.GenerationBinaryPath("example")},
	}
	r, err := New(Config{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot},
		Lock:          lock,
		BootstrapCore: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := r.EvalString(`(require '[doggo.basis :as basis])`); err != nil {
		t.Fatal(err)
	}
	got, err := r.EvalString(`(let [basis (basis/current-basis)
		generated (:generated-targets basis)]
		[(count (:lock-deps basis))
		 (count (:reify-targets generated))
		 (:binary (:build generated))])`)
	if err != nil {
		t.Fatal(err)
	}
	want := `[1 1 "` + project.GenerationBinaryPath("example") + `"]`
	if got.String() != want {
		t.Fatalf("generation basis = %s", got.String())
	}
}

func TestRuntimeRunnerRealVMPathExposesPracticalPureSurfaceAfterCoreBootstrap(t *testing.T) {
	r := newRealVMRunner(t, t.TempDir(), []string{}, true, runtime_profile.Default())

	got, err := r.EvalString(`[(count [1 2 3]) (map? {:a 1}) (vector? []) (contains? {:a 1} :a) (inc 2) (* 3 4)]`)
	if err != nil {
		t.Fatalf("expected practical pure builtins to be available on the standard path: %v", err)
	}
	if got.String() != `[3 true true true 3 12]` {
		t.Fatalf("unexpected practical pure result: %s", got.String())
	}
	if _, err := r.EvalString(`(slurp "sample.txt")`); err == nil {
		t.Fatal("expected slurp to stay unavailable on the practical pure standard path")
	}
}

func TestRuntimeRunnerRealVMPathExposesPracticalPureSurfaceWithoutCoreBootstrap(t *testing.T) {
	r := newRealVMRunner(t, t.TempDir(), []string{}, false, runtime_profile.Default())

	got, err := r.EvalString(`(count [1 2 3])`)
	if err != nil {
		t.Fatalf("expected count to be available on the practical pure standard path: %v", err)
	}
	if got.String() != "3" {
		t.Fatalf("unexpected count result: %s", got.String())
	}
	if _, err := r.EvalString(`(slurp "sample.txt")`); err == nil {
		t.Fatal("expected slurp to stay unavailable on the practical pure standard path")
	}
}

func TestRuntimeRunnerRealVMPathRejectsEffectfulStdlibNamespaces(t *testing.T) {
	r := newRealVMRunner(t, t.TempDir(), []string{}, true, runtime_profile.Default())

	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{
			name:    "os namespace stays unavailable",
			expr:    `(require '[doggo.os :as os]) (os/getenv "HOME")`,
			wantErr: "undefined variable: os-getenv*",
		},
		{
			name:    "time namespace stays unavailable",
			expr:    `(require '[doggo.time :as time]) (time/sleep 0)`,
			wantErr: "undefined variable: time-now*",
		},
		{
			name:    "process namespace stays unavailable",
			expr:    `(require '[doggo.process :as proc]) (proc/process-tree-supported?)`,
			wantErr: "undefined variable: io-file*",
		},
		{
			name:    "print stays unavailable",
			expr:    `(print "hi")`,
			wantErr: "undefined variable: print",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := r.EvalString(tt.expr); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func writeRuntimeBasisTestProject(t *testing.T) string {
	t.Helper()

	projectRoot := testutil.CanonicalTempDir(t)
	mod := &project.ModFile{
		Module: "github.com/acme/runtime-graph-demo",
		Deps:   []string{"pkg:acme/dep-one@1.2.3"},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod file: %v", err)
	}

	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     "pkg:acme/dep-one@1.2.3",
				Kind:       "git",
				Revision:   "v1.2.3",
				Digest:     "sha256:abc123",
				InstallDir: "deps/dep-one",
			},
		},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	depDir := filepath.Join(projectRoot, "deps", "dep-one", "dep-one")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("mkdir dep dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "core.dog"), []byte("(ns dep-one.core)\n(def answer 42)\n"), 0o644); err != nil {
		t.Fatalf("write dependency source: %v", err)
	}

	return projectRoot
}
