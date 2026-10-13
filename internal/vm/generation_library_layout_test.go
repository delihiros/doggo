package vm

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
	"doggo/internal/vm/engine"
)

func TestGenerationLibrarySourceLayoutRequiresGeneratedNamespace(t *testing.T) {
	_, session, _ := openVMGenerationLibraryLayout(t, []map[string]string{{
		"src/calc/core.dog": "(ns calc.core)\n(defn add [a b] (+ a b))\n",
	}})
	runner := newVMGenerationLibraryRunner(t, session)
	value, err := runner.Run("(require 'calc.core)\n(calc.core/add 1 2)")
	if err != nil {
		t.Fatalf("require generated library: %v", err)
	}
	if got, want := value.String(), "3"; got != want {
		t.Fatalf("generated library add = %s, want %s", got, want)
	}
}

func TestGenerationLibrarySourceLayoutPreservesSearchPrecedence(t *testing.T) {
	root, session, _ := openVMGenerationLibraryLayout(t, []map[string]string{
		{
			"root_only/core.dog":    ":root-only\n",
			"src/src_only/core.dog": ":src-only\n",
			"calc/core.dog":         ":root\n",
			"src/calc/core.dog":     ":src\n",
			"src/ordered/core.dog":  ":first-source-src\n",
			"src/live/core.dog":     ":dependency\n",
		},
		{
			"ordered/core.dog": ":second-source-root\n",
		},
	})
	writeVMGenerationLibraryFile(t, root, "live/core.dog", ":live-project\n")
	loader := NewLoader(root, session.Config.LoadPaths, session.Config.GenerationSources, &capturingExecutor{})
	for _, test := range []struct {
		path         string
		wantContent  string
		wantRelative string
		wantSource   *project.GenerationSource
	}{
		{"root_only/core.dog", ":root-only\n", "root_only/core.dog", session.Config.GenerationSources[0]},
		{"src_only//core.dog", ":src-only\n", "src/src_only/core.dog", session.Config.GenerationSources[0]},
		{"calc/core.dog", ":root\n", "calc/core.dog", session.Config.GenerationSources[0]},
		{"ordered/core.dog", ":first-source-src\n", "src/ordered/core.dog", session.Config.GenerationSources[0]},
		{"live/core.dog", ":live-project\n", "", nil},
	} {
		t.Run(test.path, func(t *testing.T) {
			resolved, err := loader.resolveSource(test.path)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Content != test.wantContent || resolved.Origin.RelativePath != test.wantRelative || resolved.Origin.GenerationSource != test.wantSource {
				t.Fatalf("resolved source = %#v, want content %q, relative path %q and source %p", resolved, test.wantContent, test.wantRelative, test.wantSource)
			}
			if test.wantSource != nil && resolved.Origin.File != test.wantSource.DisplayPath(test.wantRelative) {
				t.Fatalf("display origin = %q, want %q", resolved.Origin.File, test.wantSource.DisplayPath(test.wantRelative))
			}
		})
	}
}

func TestGenerationLibrarySourceLayoutRetainsRelativeAuxiliaryOrigin(t *testing.T) {
	root, session, _ := openVMGenerationLibraryLayout(t, []map[string]string{{
		"src/calc/core.dog":        "(ns calc.core)\n(defn read-relative [] (load-file \"parts/value.dog\"))\n(defn read-outside [] (load-file \"../../../escape.dog\"))\n",
		"src/calc/parts/value.dog": "(load-file \"../value.dog\")\n",
		"src/calc/value.dog":       ":src-verified\n",
		"calc/parts/value.dog":     ":root-wrong\n",
		"calc/value.dog":           ":root-wrong\n",
	}})
	writeVMGenerationLibraryFile(t, root, "escape.dog", "(def escaped true)\n")
	runner := newVMGenerationLibraryRunner(t, session)
	if _, err := runner.Run("(require 'calc.core)"); err != nil {
		t.Fatal(err)
	}
	value, err := runner.Run("(calc.core/read-relative)")
	if err != nil {
		t.Fatalf("nested relative load-file: %v", err)
	}
	if got, want := value.String(), ":src-verified"; got != want {
		t.Fatalf("relative auxiliary result = %s, want %s", got, want)
	}
	for _, relative := range []string{"src/calc/core.dog", "src/calc/parts/value.dog", "src/calc/value.dog"} {
		file := session.Config.GenerationSources[0].DisplayPath(relative)
		origin, found := runner.resolveSourceOrigin(file)
		if !found || origin.GenerationSource != session.Config.GenerationSources[0] || origin.RelativePath != relative {
			t.Fatalf("registered origin for %q = %#v, found = %v", relative, origin, found)
		}
	}
	if _, err := runner.Run("(calc.core/read-outside)"); err == nil || !strings.Contains(err.Error(), "invalid generation source path") {
		t.Fatalf("relative snapshot escape = %v, want invalid path", err)
	}
	if _, err := runner.Run("escaped"); err == nil || !strings.Contains(err.Error(), "undefined variable") {
		t.Fatalf("escaped definition lookup = %v, want undefined variable", err)
	}
}

func TestGenerationLibrarySourceLayoutIgnoresReservedStdlib(t *testing.T) {
	_, session, _ := openVMGenerationLibraryLayout(t, []map[string]string{{
		"doggo/core.dog":     "(def replacement-executed true)\n",
		"src/doggo/core.dog": "(def replacement-executed true)\n",
	}})
	loader := NewLoader(session.Config.ProjectRoot, nil, session.Config.GenerationSources, &capturingExecutor{})
	resolved, err := loader.resolveSource("doggo/core.dog")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Origin != (engine.SourceOrigin{File: "stdlib://doggo/core.dog"}) || strings.Contains(resolved.Content, "replacement-executed") {
		t.Fatalf("reserved namespace resolved to dependency source: %#v", resolved)
	}
}

func TestGenerationLibrarySourceLayoutRejectsSnapshotMutation(t *testing.T) {
	_, session, dependencyRoots := openVMGenerationLibraryLayout(t, []map[string]string{
		{
			"src/changed/core.dog":  ":verified\n",
			"src/replaced/core.dog": ":verified\n",
			"src/deleted/core.dog":  ":verified\n",
			"corrupt/core.dog":      ":verified\n",
			"src/corrupt/core.dog":  ":fallback\n",
		},
		{
			"changed/core.dog":  ":later-source\n",
			"replaced/core.dog": ":later-source\n",
			"deleted/core.dog":  ":later-source\n",
		},
	})
	dependency := dependencyRoots[0]
	writeVMGenerationLibraryFile(t, dependency, "src/changed/core.dog", ":tampered\n")
	writeVMGenerationLibraryFile(t, dependency, "corrupt/core.dog", ":tampered\n")
	original := filepath.Join(dependency, "src", "replaced", "core.dog")
	if err := os.Rename(original, original+".verified"); err != nil {
		t.Fatal(err)
	}
	writeVMGenerationLibraryFile(t, dependency, "src/replaced/core.dog", ":replacement\n")
	if err := os.Remove(filepath.Join(dependency, "src", "deleted", "core.dog")); err != nil {
		t.Fatal(err)
	}
	writeVMGenerationLibraryFile(t, dependency, "src/unmanifested/core.dog", ":unmanifested\n")
	loader := NewLoader(session.Config.ProjectRoot, nil, session.Config.GenerationSources, &capturingExecutor{})
	for _, namespace := range []string{"changed", "replaced", "deleted", "corrupt"} {
		t.Run(namespace, func(t *testing.T) {
			resolved, err := loader.resolveSource(namespace + "/core.dog")
			if err == nil || errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "integrity") {
				t.Fatalf("resolve mutated source = (%#v, %v), want integrity error without fallback", resolved, err)
			}
		})
	}
	if resolved, err := loader.resolveSource("unmanifested/core.dog"); err == nil || resolved != nil {
		t.Fatalf("resolve unmanifested source = (%#v, %v), want missing", resolved, err)
	}
	if resolved, err := loader.resolveSource("../outside.dog"); err == nil || resolved != nil || !strings.Contains(err.Error(), "invalid generation source path") {
		t.Fatalf("resolve outside snapshot = (%#v, %v), want invalid path", resolved, err)
	}
}

func TestGenerationLibrarySourceLayoutRetainsRenamedSnapshot(t *testing.T) {
	_, session, dependencyRoots := openVMGenerationLibraryLayout(t, []map[string]string{{
		"src/calc/core.dog":  "(ns calc.core)\n(def marker :verified)\n(defn read-relative [] (load-file \"value.dog\"))\n",
		"src/calc/value.dog": ":verified\n",
	}})
	runner := newVMGenerationLibraryRunner(t, session)
	dependency := dependencyRoots[0]
	if renameVMGenerationLibrarySnapshot(t, dependency, dependency+".verified") {
		writeVMGenerationLibraryFile(t, dependency, "src/calc/core.dog", "(ns calc.core)\n(def marker :replacement)\n(defn read-relative [] (load-file \"value.dog\"))\n")
		writeVMGenerationLibraryFile(t, dependency, "src/calc/value.dog", ":replacement\n")
	}
	value, err := runner.Run("(require 'calc.core)\n[calc.core/marker (calc.core/read-relative)]")
	if err != nil {
		if !strings.Contains(err.Error(), "integrity") {
			t.Fatalf("require renamed snapshot = %v, want verified result or integrity error", err)
		}
	} else if got, want := value.String(), "[:verified :verified]"; got != want {
		t.Fatalf("renamed snapshot result = %s, want %s", got, want)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(session.Config.ProjectRoot, nil, session.Config.GenerationSources, &capturingExecutor{})
	if resolved, err := loader.resolveSource("calc/core.dog"); err == nil || resolved != nil || !strings.Contains(err.Error(), "source is closed") {
		t.Fatalf("resolve closed snapshot = (%#v, %v), want closed source error", resolved, err)
	}
}

func TestGenerationLibrarySourceLayoutUsesOnlySelectedGeneration(t *testing.T) {
	root, previous, _ := openVMGenerationLibraryLayout(t, []map[string]string{{
		"src/calc/core.dog":       "(ns calc.core)\n(def marker :previous)\n",
		"src/unselected/core.dog": "(ns unselected.core)\n(def marker :previous)\n",
	}})
	previousGeneration := previous.Config.Lock.Generation
	if err := previous.Close(); err != nil {
		t.Fatal(err)
	}
	liveDependency := filepath.Join(root, "depa")
	writeVMGenerationLibraryFile(t, liveDependency, "src/calc/core.dog", "(ns calc.core)\n(def marker :selected)\n")
	if err := os.Remove(filepath.Join(liveDependency, "src", "unselected", "core.dog")); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	selected, err := project.OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := selected.Close(); err != nil {
			t.Errorf("close selected generation session: %v", err)
		}
	})
	if selected.Config.Lock.Generation == previousGeneration {
		t.Fatal("changed dependency did not select a new generation")
	}
	if _, err := os.Stat(filepath.Join(root, ".doggo", "generations", previousGeneration)); err != nil {
		t.Fatalf("previous generation must remain as an unselected control: %v", err)
	}
	writeVMGenerationLibraryFile(t, liveDependency, "src/calc/core.dog", "(ns calc.core)\n(def marker :live)\n")
	runner := newVMGenerationLibraryRunner(t, selected)
	value, err := runner.Run("(require 'calc.core)\ncalc.core/marker")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), ":selected"; got != want {
		t.Fatalf("selected dependency value = %s, want %s", got, want)
	}
	if _, err := runner.Run("(require 'unselected.core)"); err == nil || !strings.Contains(err.Error(), "could not find library") {
		t.Fatalf("require from previous generation = %v, want missing", err)
	}
}

func renameVMGenerationLibrarySnapshot(t *testing.T, original, backup string) bool {
	t.Helper()
	before, err := os.Lstat(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, backup); err == nil {
		return true
	} else if runtime.GOOS != "windows" || (!errors.Is(err, fs.ErrPermission) && !errors.Is(err, syscall.Errno(32))) {
		t.Fatal(err)
	}
	after, err := os.Lstat(original)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("blocked rename changed retained snapshot: %v", err)
	}
	if _, err := os.Lstat(backup); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("blocked rename created destination: %v", err)
	}
	return false
}

func openVMGenerationLibraryLayout(t *testing.T, layouts []map[string]string) (string, *project.GenerationSession, []string) {
	t.Helper()
	root := t.TempDir()
	deps := make([]string, len(layouts))
	for i, layout := range layouts {
		name := "dep" + string(rune('a'+i))
		deps[i] = "./" + name
		dependency := filepath.Join(root, name)
		writeVMGenerationLibraryFile(t, dependency, "doggo.mod", "{:module \"example/"+name+"\" :deps []}\n")
		for path, content := range layout {
			writeVMGenerationLibraryFile(t, dependency, path, content)
		}
	}
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: deps}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	session, err := project.OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close generation session: %v", err)
		}
	})
	if len(session.Config.GenerationSources) != len(layouts) || len(session.Config.Lock.Deps) != len(layouts) {
		t.Fatalf("generation sources = %d, lock deps = %d, want %d", len(session.Config.GenerationSources), len(session.Config.Lock.Deps), len(layouts))
	}
	dependencyRoots := make([]string, len(layouts))
	for i, dep := range session.Config.Lock.Deps {
		dependencyRoots[i] = filepath.Join(root, ".doggo", "generations", session.Config.Lock.Generation, filepath.FromSlash(dep.InstallDir))
	}
	return root, session, dependencyRoots
}

func newVMGenerationLibraryRunner(t *testing.T, session *project.GenerationSession) *Runner {
	t.Helper()
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:       session.Config.ProjectRoot,
		LoadPaths:         session.Config.LoadPaths,
		GenerationSources: session.Config.GenerationSources,
		Lock:              session.Config.Lock,
		BootstrapCore:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func writeVMGenerationLibraryFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
