package newproject

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
	toolchainbuild "doggo/internal/toolchain/build"
)

func TestServiceDryRunPlansAppProject(t *testing.T) {
	tmp := t.TempDir()
	service := Service{}

	result, err := service.Run(Options{
		BaseDir:  tmp,
		Template: "app",
		Name:     "my-app",
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !result.DryRun {
		t.Fatal("expected dry-run result")
	}
	if got, want := filepath.Base(result.ProjectDir), "my-app"; got != want {
		t.Fatalf("ProjectDir base = %q, want %q", got, want)
	}
	assertPlannedPaths(t, result.Files,
		"doggo.mod",
		".gitignore",
		"README.md",
		"src/my_app/core.dog",
		"test/my_app/core_test.dog",
	)
	if _, err := os.Stat(filepath.Join(tmp, "my-app")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create project directory, stat err = %v", err)
	}
	assertPlanContains(t, result.Files, "src/my_app/core.dog", "(ns my_app.core")
	assertPlanContains(t, result.Files, "src/my_app/core.dog", "(:require [doggo.cli :as cli])")
	assertPlanContains(t, result.Files, "src/my_app/core.dog", "(when (cli/entrypoint?)\n  (cli/main main))")
	assertPlanContains(t, result.Files, "test/my_app/core_test.dog", "(ns my_app.core-test")
	assertPlanContains(t, result.Files, ".gitignore", "/.doggo/\n")
	assertPlanExcludes(t, result.Files, ".gitignore", "bin/")
	assertPlanContains(t, result.Files, "README.md", "doggo mod sync\ndoggo build")
	assertPlanContains(t, result.Files, "README.md", "doggo exec -- src/my_app/core.dog Doggo")
	assertPlanContains(t, result.Files, "README.md", "doggo test test/my_app/core_test.dog")
}

func TestServicePlansLibAndCliProjectLayouts(t *testing.T) {
	tmp := t.TempDir()
	service := Service{}

	lib, err := service.Run(Options{BaseDir: tmp, Template: "lib", Name: "acme.math", DryRun: true})
	if err != nil {
		t.Fatalf("lib Run returned error: %v", err)
	}
	assertPlannedPaths(t, lib.Files,
		"doggo.mod",
		".gitignore",
		"README.md",
		"src/acme/math/core.dog",
		"test/acme/math/core_test.dog",
	)
	assertPlanContains(t, lib.Files, "src/acme/math/core.dog", "(ns acme.math.core")
	assertPlanContains(t, lib.Files, "README.md", "doggo mod sync\ndoggo test")

	cli, err := service.Run(Options{BaseDir: tmp, Template: "cli", Name: "my-tool", DryRun: true})
	if err != nil {
		t.Fatalf("cli Run returned error: %v", err)
	}
	assertPlannedPaths(t, cli.Files,
		"doggo.mod",
		".gitignore",
		"README.md",
		"src/my_tool/main.dog",
		"test/my_tool/main_test.dog",
	)
	assertPlanContains(t, cli.Files, "src/my_tool/main.dog", "(ns my_tool.main")
	assertPlanContains(t, cli.Files, "src/my_tool/main.dog", "(:require [doggo.cli :as cli])")
	assertPlanContains(t, cli.Files, "src/my_tool/main.dog", "(when (cli/entrypoint?)\n  (cli/main main))")
	assertPlanContains(t, cli.Files, "README.md", "doggo mod sync\ndoggo build")
	assertPlanContains(t, cli.Files, "README.md", "doggo exec -- src/my_tool/main.dog one two")
}

func TestServiceWritesProjectAndModuleOverride(t *testing.T) {
	tmp := t.TempDir()
	service := Service{}

	result, err := service.Run(Options{
		BaseDir:  tmp,
		Template: "app",
		Name:     "my-app",
		Module:   "github.com/acme/my-app",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.DryRun {
		t.Fatal("did not expect dry-run result")
	}

	mod, err := project.LoadModFile(filepath.Join(tmp, "my-app", "doggo.mod"))
	if err != nil {
		t.Fatalf("load generated doggo.mod: %v", err)
	}
	if mod.Module != "github.com/acme/my-app" {
		t.Fatalf("module = %q", mod.Module)
	}
	if mod.Name != "my-app" || mod.Version != "0.1.0" || mod.License != "MIT" || mod.Compat != ">=0.1.0" {
		t.Fatalf("unexpected mod metadata: %#v", mod)
	}
	if _, err := os.Stat(filepath.Join(tmp, "my-app", "src", "my_app", "core.dog")); err != nil {
		t.Fatalf("expected source file: %v", err)
	}
	projectRoot := filepath.Join(tmp, "my-app")
	mod, err = project.LoadModFile(project.ModPath(projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := project.LoadLockFile(project.LockPath(projectRoot))
	if err != nil {
		t.Fatalf("load generated doggo.lock: %v", err)
	}
	if lock.Build.Binary != "" || lock.Build.SHA256 != "" {
		t.Fatalf("new project generation must be sync-only: %#v", lock.Build)
	}
	if err := project.ValidateGeneration(projectRoot, mod, lock); err != nil {
		t.Fatalf("validate generated project: %v", err)
	}
}

func TestServicePublishesNormalProjectDirectoryMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX directory permissions")
	}
	tmp := t.TempDir()
	if _, err := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(tmp, "my-app"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0755 {
		t.Fatalf("project mode = %#o, want 0755", got)
	}
}

func TestServiceCreatesStageOnlyInRetainedParent(t *testing.T) {
	base := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(base, 0755); err != nil {
		t.Fatal(err)
	}
	moved := base + "-owned"
	replaced := false
	afterNewProjectParentOpenForTest = func(parent string) {
		replaced = renameRetainedForTest(t, parent, moved)
		if !replaced {
			return
		}
		if err := os.Mkdir(parent, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(parent, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterNewProjectParentOpenForTest = nil })

	_, runErr := (Service{}).Run(Options{BaseDir: base, Template: "app", Name: "my-app"})
	if !replaced {
		if runErr != nil {
			t.Fatalf("creation after blocked parent replacement: %v", runErr)
		}
		assertNoTemporaryStage(t, base)
		return
	}
	if runErr == nil {
		t.Fatal("Run accepted replaced parent")
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("replacement parent was mutated: %v", entries)
	}
}

func TestServiceCleansOwnedStageWhenOpeningItFails(t *testing.T) {
	tmp := t.TempDir()
	var replacement string
	beforeOpenNewProjectStageForTest = func(parent, name string) {
		owned := filepath.Join(parent, name)
		if err := os.Rename(owned, owned+"-owned"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(owned, 0755); err != nil {
			t.Fatal(err)
		}
		replacement = owned
		if err := os.WriteFile(filepath.Join(owned, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeOpenNewProjectStageForTest = nil })

	if _, err := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"}); err == nil {
		t.Fatal("Run accepted a replaced stage while opening it")
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(replacement) {
		t.Fatalf("owned stage leaked beside replacement: %v", entries)
	}
	replacementEntries, err := os.ReadDir(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if len(replacementEntries) != 1 || replacementEntries[0].Name() != "replacement" {
		t.Fatalf("replacement stage was mutated: %v", replacementEntries)
	}
}

func TestServiceGenerationNeverMutatesReplacedStage(t *testing.T) {
	tmp := t.TempDir()
	var replacement string
	replaced := false
	beforeNewProjectGenerationForTest = func(stage string) {
		owned := stage + "-owned"
		replaced = renameRetainedForTest(t, stage, owned)
		if !replaced {
			return
		}
		if err := os.Mkdir(stage, 0755); err != nil {
			t.Fatal(err)
		}
		replacement = stage
		if err := os.WriteFile(filepath.Join(stage, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeNewProjectGenerationForTest = nil })

	_, runErr := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	if !replaced {
		if runErr != nil {
			t.Fatalf("creation after blocked stage replacement: %v", runErr)
		}
		assertNoTemporaryStage(t, tmp)
		return
	}
	if runErr == nil {
		t.Fatal("Run accepted replaced generation stage")
	}
	entries, err := os.ReadDir(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("replacement stage was mutated: %v", entries)
	}
}

func TestServiceGenerationPreservesReplacedToolchainThroughAbort(t *testing.T) {
	tmp := t.TempDir()
	var replacement string
	replaced := false
	blocked := errors.New("abort after blocked toolchain replacement")
	afterNewProjectGenerationForTest = func(root string) error {
		lock, err := project.LoadLockFile(project.LockPath(root))
		if err != nil {
			return err
		}
		replacement = filepath.Join(root, ".doggo", "generations", lock.Generation, "toolchain")
		replaced = renameRetainedForTest(t, replacement, replacement+"-owned")
		if !replaced {
			return blocked
		}
		if err := os.Mkdir(replacement, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(replacement, "replacement"), []byte("racer"), 0644)
	}
	t.Cleanup(func() { afterNewProjectGenerationForTest = nil })

	_, runErr := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	if !replaced {
		if !errors.Is(runErr, blocked) {
			t.Fatalf("Run error = %v, want blocked-replacement abort", runErr)
		}
		assertNoProjectOrStage(t, tmp, "my-app")
		return
	}
	if runErr == nil {
		t.Fatal("Run accepted a replaced generated toolchain")
	}
	if _, err := os.Lstat(filepath.Join(tmp, "my-app")); !os.IsNotExist(err) {
		t.Fatalf("requested target was published: %v", err)
	}
	entries, err := os.ReadDir(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("replacement toolchain was mutated during abort: %v", entries)
	}
}

func TestServiceRejectsCommittedGenerationReplacementBeforeFinalValidation(t *testing.T) {
	tmp := t.TempDir()
	var replacement string
	replaced := false
	blocked := errors.New("abort after blocked generation replacement")
	afterNewProjectCommitForTest = func(root string) error {
		lock, err := project.LoadLockFile(project.LockPath(root))
		if err != nil {
			return err
		}
		replacement = filepath.Join(root, ".doggo", "generations", lock.Generation)
		replaced = renameRetainedForTest(t, replacement, replacement+"-owned")
		if !replaced {
			return blocked
		}
		if err := os.Mkdir(replacement, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(replacement, "replacement"), []byte("racer"), 0644)
	}
	t.Cleanup(func() { afterNewProjectCommitForTest = nil })

	_, runErr := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	if !replaced {
		if !errors.Is(runErr, blocked) {
			t.Fatalf("Run error = %v, want blocked-replacement abort", runErr)
		}
		assertNoProjectOrStage(t, tmp, "my-app")
		return
	}
	if runErr == nil {
		t.Fatal("Run accepted a replaced committed generation")
	}
	if _, err := os.Lstat(filepath.Join(tmp, "my-app")); !os.IsNotExist(err) {
		t.Fatalf("requested target was published: %v", err)
	}
	entries, err := os.ReadDir(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("replacement generation was mutated during abort: %v", entries)
	}
}

func TestServiceCleanupPreservesStageReplacementAtRemoval(t *testing.T) {
	tmp := t.TempDir()
	want := errors.New("generation failed")
	afterNewProjectGenerationForTest = func(string) error { return want }
	t.Cleanup(func() { afterNewProjectGenerationForTest = nil })
	var replacement string
	beforeRemoveProjectStageForTest = func(name string) {
		stage := filepath.Join(tmp, name)
		if err := os.Rename(stage, stage+"-owned"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(stage, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
		replacement = stage
	}
	t.Cleanup(func() { beforeRemoveProjectStageForTest = nil })

	_, err := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
	if !errors.Is(err, project.ErrOwnershipChanged) {
		t.Fatalf("Run error = %v, want ownership failure", err)
	}
	if content, readErr := os.ReadFile(filepath.Join(replacement, "replacement")); readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement stage changed: content=%q err=%v", content, readErr)
	}
}

func TestProjectStageCleanupPreservesRetainedReplacementAfterVerification(t *testing.T) {
	stage, err := beginProjectStage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	generation := strings.Repeat("a", 64)
	toolchainPath := filepath.Join(".doggo", "generations", generation, "toolchain")
	if err := stage.tree.CreateAll(filepath.ToSlash(toolchainPath), 0755); err != nil {
		t.Fatal(err)
	}
	var replacement string
	afterProjectRetainedVerifyForTest = func(stagePath string) {
		replacement = filepath.Join(stagePath, toolchainPath)
		if err := os.Rename(replacement, replacement+"-owned"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(replacement, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(replacement, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterProjectRetainedVerifyForTest = nil })

	if err := stage.Close(); err == nil {
		t.Fatal("project stage cleanup accepted a retained replacement after verification")
	}
	content, readErr := os.ReadFile(filepath.Join(replacement, "replacement"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
}

func TestProjectStageCleanupPreservesUnrecordedEntryReplacement(t *testing.T) {
	for _, kind := range []string{"directory", "file"} {
		t.Run(kind, func(t *testing.T) {
			stage, err := beginProjectStage(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				root, err := stage.tree.CreateRoot("entry", 0755, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := root.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := stage.tree.WriteFile("entry", []byte("owned"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var marker string
			afterProjectRetainedVerifyForTest = func(stagePath string) {
				entry := filepath.Join(stagePath, "entry")
				if err := os.Rename(entry, entry+"-owned"); err != nil {
					t.Fatal(err)
				}
				if kind == "directory" {
					if err := os.Mkdir(entry, 0755); err != nil {
						t.Fatal(err)
					}
					marker = filepath.Join(entry, "replacement")
				} else {
					marker = entry
				}
				if err := os.WriteFile(marker, []byte("racer"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { afterProjectRetainedVerifyForTest = nil })

			if err := stage.Close(); err == nil {
				t.Fatalf("Close accepted an unrecorded %s replacement", kind)
			}
			content, readErr := os.ReadFile(marker)
			if readErr != nil || string(content) != "racer" {
				t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
			}
		})
	}
}

func TestServiceCleanupPreservesNestedReplacementAtRemoval(t *testing.T) {
	tmp := t.TempDir()
	want := errors.New("generation failed")
	var stage string
	afterNewProjectGenerationForTest = func(root string) error {
		stage = root
		return want
	}
	t.Cleanup(func() { afterNewProjectGenerationForTest = nil })
	var replacement string
	beforeRemoveProjectEntryForTest = func(name string) {
		if name != ".gitignore" || replacement != "" {
			return
		}
		path := filepath.Join(stage, name)
		if err := os.Rename(path, path+"-owned"); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		replacement = path
	}
	t.Cleanup(func() { beforeRemoveProjectEntryForTest = nil })

	_, err := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
	if info, statErr := os.Stat(replacement); statErr != nil || !info.Mode().IsRegular() {
		t.Fatalf("nested replacement was removed: %v", statErr)
	}
}

func TestServiceKeepsPublishedTargetWhenPublisherReportsCommittedError(t *testing.T) {
	tmp := t.TempDir()
	want := errors.New("publisher failed after rename")
	original := publishProjectTree
	publishProjectTree = func(tree *project.OwnedTree, parent *os.Root, oldName, newName string) error {
		if err := original(tree, parent, oldName, newName); err != nil {
			return err
		}
		return &project.AtomicWriteError{Renamed: true, Err: want}
	}
	t.Cleanup(func() { publishProjectTree = original })

	_, err := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	var uncertain *PublishUncertainError
	if !errors.As(err, &uncertain) || !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want publication uncertainty wrapping %v", err, want)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "my-app", project.ModFileName)); statErr != nil {
		t.Fatalf("published target was removed: %v", statErr)
	}
	assertNoTemporaryStage(t, tmp)
}

func TestServiceFirstBuildCreatesDistinctBuiltGeneration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my-app")
	if _, err := (Service{}).Run(Options{BaseDir: filepath.Dir(root), Template: "app", Name: filepath.Base(root)}); err != nil {
		t.Fatal(err)
	}
	initial, err := project.LoadLockFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	built, err := (toolchainbuild.Service{}).Run(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if built.Generation == initial.Generation {
		t.Fatalf("build reused sync-only generation %s", initial.Generation)
	}
	if built.Build.Binary == "" || built.Build.SHA256 == "" {
		t.Fatalf("build did not create a binary-bearing generation: %#v", built)
	}
	mod, err := project.LoadModFile(project.ModPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.ValidateGeneration(root, mod, built); err != nil {
		t.Fatalf("validate built generation: %v", err)
	}
}

func TestServiceRejectsInvalidNames(t *testing.T) {
	tmp := t.TempDir()
	service := Service{}
	tests := []string{"", "   ", "../bad", "bad/name", `bad\name`, "..", "123app", "acme.123", "doggo", "doggo.cli", "doggo.foo"}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := service.Run(Options{BaseDir: tmp, Template: "app", Name: name, DryRun: true})
			if err == nil {
				t.Fatal("expected invalid name error")
			}
			if !strings.Contains(err.Error(), "invalid project name") {
				t.Fatalf("expected invalid project name error, got %v", err)
			}
		})
	}
}

func TestServiceRejectsInvalidModuleOverrides(t *testing.T) {
	tmp := t.TempDir()
	service := Service{}
	tests := []string{
		"bad module",
		"../bad",
		"/bad",
		"bad/module/",
		"bad//module",
		`bad\module`,
		"bad@main",
		"bad?ref=main",
	}

	for _, module := range tests {
		t.Run(module, func(t *testing.T) {
			_, err := service.Run(Options{BaseDir: tmp, Template: "app", Name: "my-app", Module: module, DryRun: true})
			if err == nil {
				t.Fatal("expected invalid module error")
			}
			if !strings.Contains(err.Error(), "invalid module") {
				t.Fatalf("expected invalid module error, got %v", err)
			}
		})
	}
}

func TestServiceRejectsUnknownTemplate(t *testing.T) {
	service := Service{}
	_, err := service.Run(Options{BaseDir: t.TempDir(), Template: "web", Name: "demo", DryRun: true})
	if err == nil {
		t.Fatal("expected unknown template error")
	}
	if !strings.Contains(err.Error(), "unknown template") || !strings.Contains(err.Error(), "app, lib, cli") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceRejectsEveryExistingTarget(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "my-app")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	service := Service{}
	_, err := service.Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), target) || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected collision error: %v", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("existing target changed: entries=%v err=%v", entries, err)
	}
}

func TestServiceDryRunReportsCollisions(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "my-app", "README.md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("custom readme"), 0644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	service := Service{}
	_, err := service.Run(Options{BaseDir: tmp, Template: "app", Name: "my-app", DryRun: true})
	if err == nil {
		t.Fatal("expected dry-run collision error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected dry-run collision error: %v", err)
	}
}

func TestServiceGenerationFailureLeavesNoTargetOrStage(t *testing.T) {
	tmp := t.TempDir()
	want := errors.New("generation failed")
	afterNewProjectGenerationForTest = func(string) error { return want }
	t.Cleanup(func() { afterNewProjectGenerationForTest = nil })

	_, err := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
	assertNoProjectOrStage(t, tmp, "my-app")
}

func TestServicePublicationFailureLeavesNoTargetOrStage(t *testing.T) {
	tmp := t.TempDir()
	want := errors.New("publication failed")
	beforeNewProjectRenameForTest = func(_, _ string) error { return want }
	t.Cleanup(func() { beforeNewProjectRenameForTest = nil })

	_, err := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
	assertNoProjectOrStage(t, tmp, "my-app")
}

func TestServiceNeverReplacesTargetCreatedDuringAssembly(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "my-app")
	beforeNewProjectRenameForTest = func(_, destination string) error {
		if err := os.Mkdir(destination, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destination, "owner"), []byte("racer"), 0644); err != nil {
			return err
		}
		return nil
	}
	t.Cleanup(func() { beforeNewProjectRenameForTest = nil })

	_, err := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	if err == nil || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Run error = %v, want existing-target error", err)
	}
	content, readErr := os.ReadFile(filepath.Join(target, "owner"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("racing target changed: content=%q err=%v", content, readErr)
	}
	assertNoTemporaryStage(t, tmp)
}

func TestServiceRejectsReplacedParentOrStageBeforePublish(t *testing.T) {
	for _, replacement := range []string{"parent", "stage"} {
		t.Run(replacement, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "workspace")
			if err := os.Mkdir(base, 0755); err != nil {
				t.Fatal(err)
			}
			var replacementMarker, ownedStage string
			beforeNewProjectRenameForTest = func(stage, _ string) error {
				replacementMarker, ownedStage = replaceNewProjectOwnership(t, replacement, stage)
				return nil
			}
			t.Cleanup(func() { beforeNewProjectRenameForTest = nil })

			_, err := (Service{}).Run(Options{BaseDir: base, Template: "app", Name: "my-app"})
			if replacementMarker == "" {
				if err != nil {
					t.Fatalf("creation after blocked replacement: %v", err)
				}
				assertNoTemporaryStage(t, base)
				return
			}
			if err == nil {
				t.Fatal("Run accepted replaced project ownership")
			}
			if content, readErr := os.ReadFile(replacementMarker); readErr != nil || string(content) != "replacement" {
				t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
			}
			if _, statErr := os.Lstat(ownedStage); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("owned stage was not cleaned: %v", statErr)
			}
		})
	}
}

func TestServiceCleanupDoesNotDeleteReplacedParentOrStage(t *testing.T) {
	for _, replacement := range []string{"parent", "stage"} {
		t.Run(replacement, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "workspace")
			if err := os.Mkdir(base, 0755); err != nil {
				t.Fatal(err)
			}
			var replacementMarker, ownedStage, targetMarker string
			beforeNewProjectRenameForTest = func(stage, target string) error {
				replacementMarker, ownedStage = replaceNewProjectOwnership(t, replacement, stage)
				if err := os.Mkdir(target, 0755); err != nil {
					return err
				}
				targetMarker = filepath.Join(target, "owner")
				if err := os.WriteFile(targetMarker, []byte("racer"), 0644); err != nil {
					return err
				}
				return nil
			}
			t.Cleanup(func() { beforeNewProjectRenameForTest = nil })

			_, err := (Service{}).Run(Options{BaseDir: base, Template: "app", Name: "my-app"})
			if err == nil {
				t.Fatal("Run accepted replaced project ownership")
			}
			for path, want := range map[string]string{replacementMarker: "replacement", targetMarker: "racer"} {
				if path == "" { // The OS blocked replacement of the retained source.
					continue
				}
				if content, readErr := os.ReadFile(path); readErr != nil || string(content) != want {
					t.Fatalf("unowned path %s changed: content=%q err=%v", path, content, readErr)
				}
			}
			if _, statErr := os.Lstat(ownedStage); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("owned stage was not cleaned: %v", statErr)
			}
		})
	}
}

func TestServicePostRenameFailurePreservesPublishedTargetAndRecreatedStage(t *testing.T) {
	tmp := t.TempDir()
	want := errors.New("post-rename failure")
	var recreatedMarker string
	original := publishProjectTree
	publishProjectTree = func(tree *project.OwnedTree, parent *os.Root, oldName, newName string) error {
		if err := original(tree, parent, oldName, newName); err != nil {
			return err
		}
		if err := parent.Mkdir(oldName, 0755); err != nil {
			t.Fatal(err)
		}
		recreatedMarker = filepath.Join(tmp, oldName, "owner")
		if err := os.WriteFile(recreatedMarker, []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
		return &project.AtomicWriteError{Renamed: true, Err: want}
	}
	t.Cleanup(func() { publishProjectTree = original })

	_, err := (Service{}).Run(Options{BaseDir: tmp, Template: "app", Name: "my-app"})
	var uncertain *PublishUncertainError
	if !errors.Is(err, want) || !errors.As(err, &uncertain) {
		t.Fatalf("Run error = %v, want publication uncertainty wrapping %v", err, want)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "my-app", project.LockFileName)); statErr != nil {
		t.Fatalf("published target missing after commit point: %v", statErr)
	}
	if content, readErr := os.ReadFile(recreatedMarker); readErr != nil || string(content) != "racer" {
		t.Fatalf("recreated stage was cleaned: content=%q err=%v", content, readErr)
	}
}

func assertPlannedPaths(t *testing.T, files []PlannedFile, want ...string) {
	t.Helper()
	if len(files) != len(want) {
		t.Fatalf("planned file count = %d, want %d: %#v", len(files), len(want), files)
	}
	for i, file := range files {
		if file.Path != want[i] {
			t.Fatalf("planned path[%d] = %q, want %q", i, file.Path, want[i])
		}
	}
}

func assertPlanContains(t *testing.T, files []PlannedFile, path, want string) {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			if !strings.Contains(file.Content, want) {
				t.Fatalf("%s does not contain %q:\n%s", path, want, file.Content)
			}
			return
		}
	}
	t.Fatalf("planned file not found: %s", path)
}

func assertPlanExcludes(t *testing.T, files []PlannedFile, path, unwanted string) {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			if strings.Contains(file.Content, unwanted) {
				t.Fatalf("%s contains obsolete %q:\n%s", path, unwanted, file.Content)
			}
			return
		}
	}
	t.Fatalf("planned file not found: %s", path)
}

func assertNoProjectOrStage(t *testing.T, base, name string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(base, name)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("target exists after failure: %v", err)
	}
	assertNoTemporaryStage(t, base)
}

func assertNoTemporaryStage(t *testing.T, base string) {
	t.Helper()
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".doggo-new-") {
			t.Fatalf("temporary project was not removed: %s", entry.Name())
		}
	}
}

// Windows may reject an attempted replacement while handles are retained.
// Assert that the operation left the source and destination unchanged.
func renameRetainedForTest(t *testing.T, oldPath, newPath string) bool {
	t.Helper()
	before, err := os.Lstat(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, newPath); err == nil {
		return true
	} else if runtime.GOOS != "windows" || (!errors.Is(err, fs.ErrPermission) && !errors.Is(err, syscall.Errno(32))) { // ERROR_SHARING_VIOLATION
		t.Fatal(err)
	}
	after, err := os.Lstat(oldPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("blocked rename changed retained source: %v", err)
	}
	if _, err := os.Lstat(newPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("blocked rename created destination: %v", err)
	}
	return false
}

func replaceNewProjectOwnership(t *testing.T, replacement, stage string) (replacementMarker, ownedStage string) {
	t.Helper()
	parent := filepath.Dir(stage)
	switch replacement {
	case "parent":
		movedParent := parent + "-owned"
		if !renameRetainedForTest(t, parent, movedParent) {
			return "", stage
		}
		if err := os.Mkdir(parent, 0755); err != nil {
			t.Fatal(err)
		}
		ownedStage = filepath.Join(movedParent, filepath.Base(stage))
	case "stage":
		ownedStage = stage + "-owned"
		if !renameRetainedForTest(t, stage, ownedStage) {
			return "", stage
		}
	default:
		t.Fatalf("unknown replacement %q", replacement)
	}
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	replacementMarker = filepath.Join(stage, "replacement")
	if err := os.WriteFile(replacementMarker, []byte("replacement"), 0644); err != nil {
		t.Fatal(err)
	}
	return replacementMarker, ownedStage
}
