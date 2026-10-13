package generation

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"doggo/internal/project"
	"doggo/internal/testutil"
)

func TestServicePrepareSyncSnapshotsIntoOwnedStage(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(filepath.Join(root, "dep", project.ModFileName), &project.ModFile{Module: "example/dep", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}

	p, err := (Service{}).PrepareSync(root, []string{"./dep"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Abort()
	if p.state != statePreparing || p.Lock.Generation != "" || len(p.Lock.Deps) != 1 {
		t.Fatalf("prepared = state %v lock %#v", p.state, p.Lock)
	}
	if p.Lock.Deps[0].ModuleID != "doggo:example/dep" {
		t.Fatalf("snapshot dependency = %#v", p.Lock.Deps[0])
	}
	for _, path := range []string{
		filepath.Join(p.stage, filepath.FromSlash(p.Lock.Deps[0].InstallDir)),
		filepath.Join(p.stage, "toolchain", "go.mod"),
		filepath.Join(p.stage, "toolchain", "internal", "interop", "generated.go"),
		filepath.Join(p.stage, "toolchain", "internal", "reify", "generated_reify.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("prepared path %s: %v", path, err)
		}
	}
}

func TestServicePrepareSyncTreeNeverMutatesReplacedProjectPath(t *testing.T) {
	for _, boundary := range []string{"dependencies", "scan", "host", "interop", "reify", "tidy"} {
		t.Run(boundary, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
				t.Fatal(err)
			}
			retained, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			ledgerRoot, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			defer ledgerRoot.Close()
			tree := project.NewOwnedTree(ledgerRoot)
			defer tree.Close()
			if err := tree.CaptureExisting(); err != nil {
				t.Fatal(err)
			}
			moved := root + "-owned"
			attempted, replaced := false, false
			afterPrepareSyncStageVerifyForTest = func(current string) {
				if current != boundary {
					return
				}
				attempted = true
				replaced = testutil.RenameRetained(t, root, moved)
				if !replaced {
					return
				}
				if err := os.Mkdir(root, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "replacement"), []byte("racer"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { afterPrepareSyncStageVerifyForTest = nil })

			p, prepareErr := (Service{}).PrepareSyncTree(retained, root, tree, []string{})
			if !attempted {
				t.Fatal("project replacement hook was not called")
			}
			if !replaced {
				if prepareErr != nil {
					t.Fatalf("preparation after blocked project replacement: %v", prepareErr)
				}
				if err := errors.Join(p.Abort(), tree.VerifyComplete(), project.VerifyRootPath(root, ledgerRoot)); err != nil {
					t.Fatal(err)
				}
				return
			}
			if prepareErr == nil {
				_ = p.Abort()
				t.Fatal("PrepareSyncTree accepted a replaced project path")
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "replacement" {
				t.Fatalf("replacement project was mutated: %v", entries)
			}
		})
	}
}

func TestServicePrepareSyncPreservesReplacedOwnedChildrenThroughAbort(t *testing.T) {
	for _, boundary := range []string{"scan", "interop", "tidy"} {
		t.Run(boundary, func(t *testing.T) {
			root := t.TempDir()
			if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
				t.Fatal(err)
			}
			dep := filepath.Join(root, "dep")
			if err := os.Mkdir(dep, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dep, "go.mod"), []byte("module example.com/dep\n\ngo 1.24.0\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dep, "dep.go"), []byte("package dep\n\ntype Handler interface { Handle() }\n"), 0644); err != nil {
				t.Fatal(err)
			}
			var replacement string
			replaced := false
			afterPrepareSyncStageVerifyForTest = func(current string) {
				if current != boundary || replacement != "" {
					return
				}
				stage := currentGenerationStage(t, root)
				if boundary == "scan" {
					entries, err := os.ReadDir(filepath.Join(stage, "deps"))
					if err != nil || len(entries) != 1 {
						t.Fatalf("read staged dependencies: %v, entries %v", err, entries)
					}
					replacement = filepath.Join(stage, "deps", entries[0].Name())
				} else {
					replacement = filepath.Join(stage, "toolchain")
				}
				replaced = testutil.RenameRetained(t, replacement, replacement+"-owned")
				if !replaced {
					return
				}
				if err := os.Mkdir(replacement, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(replacement, "replacement"), []byte("racer"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { afterPrepareSyncStageVerifyForTest = nil })

			p, prepareErr := (Service{}).PrepareSync(root, []string{"./dep"})
			if replacement == "" {
				t.Fatal("owned-child replacement hook was not called")
			}
			if !replaced {
				if prepareErr != nil {
					t.Fatalf("preparation after blocked owned-child replacement: %v", prepareErr)
				}
				if err := p.Abort(); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(replacement); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("owned stage remains after abort: %v", err)
				}
				return
			}
			if prepareErr == nil {
				_ = p.Abort()
				t.Fatal("PrepareSync accepted a replaced owned child")
			}
			entries, err := os.ReadDir(replacement)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "replacement" {
				t.Fatalf("replacement child was mutated during abort: %v", entries)
			}
		})
	}
}

func TestServicePrepareSyncNeverWritesThroughReopenedToolchain(t *testing.T) {
	for _, tc := range []struct {
		boundary string
		rel      string
		prepare  func(*testing.T, string) []byte
	}{
		{
			boundary: "interop",
			rel:      filepath.Join("internal", "interop", "generated.go"),
			prepare: func(t *testing.T, toolchain string) []byte {
				path := filepath.Join(toolchain, "internal", "interop", "generated.go")
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				content = append(content, []byte("\n// replacement sentinel\n")...)
				if err := os.WriteFile(path, content, 0644); err != nil {
					t.Fatal(err)
				}
				return content
			},
		},
		{
			boundary: "reify",
			rel:      filepath.Join("internal", "reify", "generated_reify.go"),
			prepare: func(t *testing.T, toolchain string) []byte {
				path := filepath.Join(toolchain, "internal", "reify", "generated_reify.go")
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				content = append(content, []byte("\n// replacement sentinel\n")...)
				if err := os.WriteFile(path, content, 0644); err != nil {
					t.Fatal(err)
				}
				return content
			},
		},
		{
			boundary: "tidy",
			rel:      "go.mod",
			prepare: func(t *testing.T, toolchain string) []byte {
				unused := filepath.Join(toolchain, "unused")
				if err := os.Mkdir(unused, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(unused, "go.mod"), []byte("module example.invalid/unused\n\ngo 1.24.0\n"), 0644); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(toolchain, "go.mod")
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				content = append(content, []byte("\nrequire example.invalid/unused v0.0.0\nreplace example.invalid/unused => ./unused\n")...)
				if err := os.WriteFile(path, content, 0644); err != nil {
					t.Fatal(err)
				}
				return content
			},
		},
	} {
		t.Run(tc.boundary, func(t *testing.T) {
			root := t.TempDir()
			if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
				t.Fatal(err)
			}
			var replacement string
			var expected []byte
			attempted := false
			afterPrepareSyncStageVerifyForTest = func(boundary string) {
				if boundary != tc.boundary || attempted {
					return
				}
				toolchain := filepath.Join(currentGenerationStage(t, root), "toolchain")
				attempted = true
				replacement = replaceWithCopiedTree(t, toolchain)
				if replacement == "" {
					return
				}
				expected = tc.prepare(t, replacement)
			}
			t.Cleanup(func() { afterPrepareSyncStageVerifyForTest = nil })

			p, prepareErr := (Service{}).PrepareSync(root, []string{})
			if !attempted {
				t.Fatal("toolchain replacement hook was not called")
			}
			if replacement == "" {
				if prepareErr != nil {
					t.Fatalf("preparation after blocked toolchain replacement: %v", prepareErr)
				}
				if err := p.Abort(); err != nil {
					t.Fatal(err)
				}
				return
			}
			if prepareErr == nil {
				_ = p.Abort()
				t.Fatal("PrepareSync accepted a toolchain replacement after verification")
			}
			content, err := os.ReadFile(filepath.Join(replacement, tc.rel))
			if err != nil || string(content) != string(expected) {
				t.Fatalf("replacement %s changed: content=%q err=%v", tc.rel, content, err)
			}
		})
	}
}

func TestPreparedGenerationBuildNeverUsesReplacedStagePath(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	p, err := (Service{}).PrepareSync(root, []string{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Abort()
	var replacement string
	attempted := false
	afterPreparedBuildVerifyForTest = func(stage string) {
		attempted = true
		replacement = replaceWithCopiedTree(t, stage)
		if replacement == "" {
			return
		}
		if err := os.WriteFile(filepath.Join(replacement, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterPreparedBuildVerifyForTest = nil })

	buildErr := p.Build()
	if !attempted {
		t.Fatal("build-stage replacement hook was not called")
	}
	if replacement == "" {
		if buildErr != nil {
			t.Fatalf("build after blocked stage replacement: %v", buildErr)
		}
		return
	}
	if buildErr == nil {
		t.Fatal("Build accepted a stage replacement after verification")
	}
	if _, err := os.Lstat(filepath.Join(replacement, "bin")); !os.IsNotExist(err) {
		t.Fatalf("Build mutated replacement bin: %v", err)
	}
	content, readErr := os.ReadFile(filepath.Join(replacement, "replacement"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement marker changed: content=%q err=%v", content, readErr)
	}
}

func TestPreparedGenerationBuildNeverUsesReplacedDependencyInstall(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
		t.Fatal(err)
	}
	dep := filepath.Join(root, "dep")
	if err := os.Mkdir(dep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "go.mod"), []byte("module example.com/dep\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "dep.go"), []byte("package dep\n\ntype Runner interface { Run() }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := (Service{}).PrepareSync(root, []string{"./dep"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Abort()
	install := filepath.Join(p.stage, filepath.FromSlash(p.Lock.Deps[0].InstallDir))
	replacement := replaceWithCopiedTree(t, install)
	if replacement == "" {
		if err := p.Build(); err != nil {
			t.Fatalf("build after blocked dependency replacement: %v", err)
		}
		return
	}
	marker := filepath.Join(replacement, "replacement")
	if err := os.WriteFile(marker, []byte("racer"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := p.Build(); !errors.Is(err, project.ErrOwnershipChanged) {
		t.Fatalf("Build error = %v, want dependency ownership failure", err)
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "racer" {
		t.Fatalf("replacement marker changed: content=%q err=%v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(replacement, "bin")); !os.IsNotExist(err) {
		t.Fatalf("Build mutated replacement dependency: %v", err)
	}
}

func TestServicePrepareSyncRejectsReplacementBeforeCreatedRootHandoff(t *testing.T) {
	for _, boundary := range []string{"dependencies", "toolchain"} {
		t.Run(boundary, func(t *testing.T) {
			root := t.TempDir()
			if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
				t.Fatal(err)
			}
			dep := filepath.Join(root, "dep")
			if err := os.Mkdir(dep, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dep, "go.mod"), []byte("module example.com/dep\n\ngo 1.24.0\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dep, "dep.go"), []byte("package dep\n\ntype Handler interface { Handle() }\n"), 0644); err != nil {
				t.Fatal(err)
			}
			var replacement string
			replaced := false
			afterPrepareSyncStageRootsCreatedForTest = func(current string) {
				if current != boundary || replacement != "" {
					return
				}
				stage := currentGenerationStage(t, root)
				if boundary == "dependencies" {
					entries, err := os.ReadDir(filepath.Join(stage, "deps"))
					if err != nil || len(entries) != 1 {
						t.Fatalf("read staged dependencies: %v, entries %v", err, entries)
					}
					replacement = filepath.Join(stage, "deps", entries[0].Name())
				} else {
					replacement = filepath.Join(stage, "toolchain")
				}
				replaced = testutil.RenameRetained(t, replacement, replacement+"-owned")
				if !replaced {
					return
				}
				if err := os.Mkdir(replacement, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(replacement, "replacement"), []byte("racer"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { afterPrepareSyncStageRootsCreatedForTest = nil })

			p, prepareErr := (Service{}).PrepareSync(root, []string{"./dep"})
			if replacement == "" {
				t.Fatal("created-root replacement hook was not called")
			}
			if !replaced {
				if prepareErr != nil {
					t.Fatalf("preparation after blocked created-root replacement: %v", prepareErr)
				}
				if err := errors.Join(p.owner.VerifyStage(), p.Abort()); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(replacement); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("owned stage remains after abort: %v", err)
				}
				return
			}
			if prepareErr == nil {
				_ = p.Abort()
				t.Fatal("PrepareSync accepted a replacement before root handoff")
			}
			entries, err := os.ReadDir(replacement)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "replacement" {
				t.Fatalf("replacement was mutated during abort: %v", entries)
			}
			if content, err := os.ReadFile(filepath.Join(replacement, "replacement")); err != nil || string(content) != "racer" {
				t.Fatalf("replacement marker changed: content=%q err=%v", content, err)
			}
		})
	}
}

func currentGenerationStage(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".doggo", "generations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".stage-") {
			return filepath.Join(root, ".doggo", "generations", entry.Name())
		}
	}
	t.Fatal("active generation stage not found")
	return ""
}

func replaceWithCopiedTree(t *testing.T, path string) string {
	t.Helper()
	replacement := path + "-replacement"
	if err := os.Mkdir(replacement, 0755); err != nil {
		t.Fatal(err)
	}
	source, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := os.OpenRoot(replacement)
	if err != nil {
		_ = source.Close()
		t.Fatal(err)
	}
	sourceTree := project.NewOwnedTree(source)
	destinationTree := project.NewOwnedTree(destination)
	copyErr := sourceTree.CaptureExisting()
	if copyErr == nil {
		copyErr = sourceTree.CopyTo(destinationTree, nil)
	}
	if err := errors.Join(copyErr, destinationTree.Close(), sourceTree.Close(), destination.Close(), source.Close()); err != nil {
		t.Fatal(err)
	}
	if !testutil.RenameRetained(t, path, path+"-owned") {
		if err := os.RemoveAll(replacement); err != nil {
			t.Fatal(err)
		}
		return ""
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServicePrepareSyncGeneratesExternalImportsAndReadonlySemanticBuild(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
		t.Fatal(err)
	}
	dep := filepath.Join(root, "dep")
	if err := os.Mkdir(dep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "go.mod"), []byte("module example.com/external\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "external.go"), []byte("package external\n\ntype Runner interface { Run() string }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := (Service{}).PrepareSync(root, []string{"./dep"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Abort()
	toolchainRoot := filepath.Join(p.stage, "toolchain")
	for _, generated := range []string{
		filepath.Join(toolchainRoot, "internal", "interop", "generated.go"),
		filepath.Join(toolchainRoot, "internal", "reify", "generated_reify.go"),
	} {
		content, err := os.ReadFile(generated)
		if err != nil || !strings.Contains(string(content), "example.com/external") {
			t.Fatalf("generated import missing in %s: %v", generated, err)
		}
	}
	cmd := exec.Command("go", "test", "-mod=readonly", "-run", "^$", "./internal/interop", "./internal/reify")
	cmd.Dir = toolchainRoot
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("readonly generated package build: %v\n%s", err, output)
	}
}

func TestServicePrepareCurrentBuildCopiesValidatedGeneration(t *testing.T) {
	committed := newPreparedFixture(t)
	if err := committed.Commit(); err != nil {
		t.Fatal(err)
	}

	p, err := (Service{}).PrepareCurrentBuild(committed.projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Abort()
	if p == committed || p.Lock == committed.Lock {
		t.Fatal("PrepareCurrentBuild reused mutable prepared state")
	}
	if p.Lock.Generation != "" || p.state != statePreparing {
		t.Fatalf("prepared current build = state %v generation %q", p.state, p.Lock.Generation)
	}
	content, err := os.ReadFile(filepath.Join(p.stage, "toolchain", "main.go"))
	if err != nil || string(content) != "package main\n" {
		t.Fatalf("copied toolchain = %q, %v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(p.stage, ".generation")); !os.IsNotExist(err) {
		t.Fatalf("stage retained excluded marker: %v", err)
	}
}

func TestServicePrepareCurrentBuildCreatesFreshBinaryGenerationOnRepeatedBuild(t *testing.T) {
	first := newPreparedFixture(t)
	if err := first.Build(); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	prior := cloneLock(first.Lock)

	second, err := (Service{}).PrepareCurrentBuild(first.projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Abort()
	if second.Lock.Build != (project.LockBuild{}) {
		t.Fatalf("prepared build inherited prior build metadata: %#v", second.Lock.Build)
	}
	if _, err := os.Lstat(filepath.Join(second.stage, "bin")); !os.IsNotExist(err) {
		t.Fatalf("prepared build inherited prior bin directory: %v", err)
	}
	secondTree, err := second.owner.StageTree()
	if err != nil {
		t.Fatal(err)
	}
	if inherited, err := secondTree.OpenRoot("bin"); err == nil {
		_ = inherited.Close()
		t.Fatal("prepared build retained the prior bin handle")
	}
	if err := second.Build(); err != nil {
		t.Fatalf("second Build: %v", err)
	}
	priorBinary, err := os.Stat(filepath.Join(first.projectRoot, filepath.FromSlash(prior.Build.Binary)))
	if err != nil {
		t.Fatal(err)
	}
	freshBinary, err := os.Stat(filepath.Join(second.stage, "bin", project.GenerationBinaryName()))
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(priorBinary, freshBinary) {
		t.Fatal("second build retained the prior binary identity")
	}
	if second.Lock.Build.Binary != "" || second.Lock.Build.SHA256 == "" {
		t.Fatalf("fresh tentative build metadata = %#v", second.Lock.Build)
	}
	if err := second.Commit(); err != nil {
		t.Fatalf("second Commit: %v", err)
	}
	if second.Lock.Build.Binary == "" || second.Lock.Build.SHA256 == "" {
		t.Fatalf("second build did not publish a binary: %#v", second.Lock.Build)
	}
	mod, err := project.LoadModFile(project.ModPath(first.projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.ValidateGeneration(first.projectRoot, mod, second.Lock); err != nil {
		t.Fatalf("fresh binary generation is invalid: %v", err)
	}
	session, err := project.OpenGenerationSession(project.ModPath(first.projectRoot))
	if err != nil {
		t.Fatalf("open fresh generation session: %v", err)
	}
	if !session.HasBinary() {
		t.Fatal("fresh generation session has no retained binary")
	}
	snapshot, err := session.BinarySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ProjectPath != second.Lock.Build.Binary || snapshot.SHA256 != second.Lock.Build.SHA256 || len(snapshot.Content) == 0 {
		t.Fatalf("session binary snapshot = %#v", snapshot)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareCurrentBuildWaitsForOldSharedLease(t *testing.T) {
	committed := newPreparedFixture(t)
	if err := committed.Commit(); err != nil {
		t.Fatal(err)
	}
	shared, err := project.AcquireGenerationLease(committed.projectRoot, false)
	if err != nil {
		t.Fatal(err)
	}

	prepared := make(chan *PreparedGeneration, 1)
	errs := make(chan error, 1)
	go func() {
		p, err := (Service{}).PrepareCurrentBuild(committed.projectRoot)
		if err != nil {
			errs <- err
			return
		}
		prepared <- p
	}()
	select {
	case p := <-prepared:
		_ = p.Abort()
		t.Fatal("exclusive preparation acquired while shared lease was held")
	case err := <-errs:
		t.Fatalf("PrepareCurrentBuild returned before lease release: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := shared.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-prepared:
		if err := p.Abort(); err != nil {
			t.Fatal(err)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("PrepareCurrentBuild remained blocked after shared lease closed")
	}
}
