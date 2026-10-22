package deps

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"doggo/internal/project"
	"doggo/internal/testutil"
	"doggo/internal/toolchain/generation"

	"golang.org/x/mod/modfile"
)

func TestAuditCopiesClosedGenerationWorkspaceForGoDependencies(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
		t.Fatal(err)
	}
	dependency := filepath.Join(root, "dep")
	if err := os.Mkdir(dependency, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "go.mod"), []byte("module example.com/external\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	const originalDependency = "package external\n\ntype Runner interface { Run() string }\n"
	if err := os.WriteFile(filepath.Join(dependency, "external.go"), []byte(originalDependency), 0644); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}

	called := false
	auditor := Auditor{
		OpenGenerationSession: func(scriptPath string) (*project.GenerationSession, error) {
			session, err := project.OpenGenerationSession(scriptPath)
			if err != nil {
				return nil, err
			}
			generationRoot := filepath.Join(root, ".doggo", "generations", session.Config.Lock.Generation)
			retained := generationRoot + "-retained"
			if !testutil.RenameRetained(t, generationRoot, retained) {
				return session, nil
			}
			replacement := filepath.Join(generationRoot, filepath.FromSlash(session.Config.Lock.Deps[0].InstallDir))
			if err := os.MkdirAll(replacement, 0755); err != nil {
				_ = session.Close()
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(replacement, "external.go"), []byte("package external\n\nconst Attacker = true\n"), 0644); err != nil {
				_ = session.Close()
				return nil, err
			}
			return session, nil
		},
		AuditLock: func(contentRoot, _ string, _ *project.ModFile, lock *project.LockFile) ([]DependencyAuditFinding, error) {
			called = true
			toolchainRoot := filepath.Join(contentRoot, "toolchain")
			goMod, err := os.ReadFile(filepath.Join(toolchainRoot, "go.mod"))
			if err != nil {
				return nil, err
			}
			parsed, err := modfile.Parse("go.mod", goMod, nil)
			if err != nil {
				return nil, err
			}
			dependencyRoot := filepath.Join(contentRoot, filepath.FromSlash(lock.Deps[0].InstallDir))
			foundReplacement := false
			for _, replacement := range parsed.Replace {
				if replacement.Old.Path != lock.Deps[0].GoModule {
					continue
				}
				foundReplacement = true
				resolved := filepath.Clean(filepath.Join(toolchainRoot, filepath.FromSlash(replacement.New.Path)))
				if resolved != dependencyRoot {
					t.Fatalf("replace target = %q, want private dependency %q", resolved, dependencyRoot)
				}
				if err := project.ValidateContainedPath(contentRoot, resolved); err != nil {
					t.Fatalf("replace target escaped private audit root: %v", err)
				}
			}
			if !foundReplacement {
				t.Fatalf("go.mod has no replacement for %s", lock.Deps[0].GoModule)
			}
			gotDependency, err := os.ReadFile(filepath.Join(dependencyRoot, "external.go"))
			if err != nil {
				return nil, err
			}
			if string(gotDependency) != originalDependency {
				t.Fatalf("audit dependency = %q, want validated original", gotDependency)
			}
			cmd := exec.Command("go", "test", "-mod=readonly", "-run", "^$", "./internal/interop", "./internal/reify")
			cmd.Dir = toolchainRoot
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("resolve imported audit dependency: %v\n%s", err, output)
			}
			return nil, nil
		},
	}

	if _, err := auditor.Audit(root); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("AuditLock was not called")
	}
}

func TestAuditCleansPublishedWorkspaceAfterCopyFailure(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}

	want := errors.New("later audit workspace write failed")
	var temporaryPath string
	originalCopy := auditWorkspaceCopyForTest
	auditWorkspaceCopyForTest = func(_ *project.GenerationSession, temporary *project.OwnedTempRoot) error {
		temporaryPath = temporary.Path()
		toolchain, err := temporary.Tree().CreateRoot("toolchain", 0755, nil)
		if err != nil {
			return err
		}
		if err := toolchain.Close(); err != nil {
			return err
		}
		if err := temporary.Tree().WriteOwnedFile("toolchain/first.go", []byte("package main\n"), 0644); err != nil {
			return err
		}
		return want
	}
	t.Cleanup(func() { auditWorkspaceCopyForTest = originalCopy })

	if _, err := (Auditor{}).Audit(root); !errors.Is(err, want) {
		t.Fatalf("Audit error = %v, want %v", err, want)
	}
	if temporaryPath == "" {
		t.Fatal("audit workspace copy seam did not run")
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial audit workspace remained after cleanup: %v", err)
	}
}

func TestAuditCopiesVerifiedGenerationWorkspaceToPrivateRoot(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}

	const generatedPath = "internal/interop/generated.go"
	var expectedMod, expectedGenerated []byte
	var auditRoot string
	auditor := Auditor{
		OpenGenerationSession: func(scriptPath string) (*project.GenerationSession, error) {
			session, err := project.OpenGenerationSession(scriptPath)
			if err != nil {
				return nil, err
			}
			generationRoot := filepath.Join(root, ".doggo", "generations", session.Config.Lock.Generation)
			expectedMod, err = os.ReadFile(filepath.Join(generationRoot, "toolchain", "go.mod"))
			if err != nil {
				_ = session.Close()
				return nil, err
			}
			expectedGenerated, err = os.ReadFile(filepath.Join(generationRoot, "toolchain", filepath.FromSlash(generatedPath)))
			if err != nil {
				_ = session.Close()
				return nil, err
			}
			if !testutil.RenameRetained(t, generationRoot, generationRoot+"-retained") {
				return session, nil
			}
			if err := os.MkdirAll(filepath.Join(generationRoot, "toolchain"), 0755); err != nil {
				_ = session.Close()
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(generationRoot, "toolchain", "go.mod"), []byte("replacement\n"), 0644); err != nil {
				_ = session.Close()
				return nil, err
			}
			return session, nil
		},
		AuditLock: func(contentRoot, sourceRoot string, _ *project.ModFile, _ *project.LockFile) ([]DependencyAuditFinding, error) {
			auditRoot = contentRoot
			if sourceRoot != root {
				t.Fatalf("source root = %q, want live project %q", sourceRoot, root)
			}
			generationPrefix := filepath.Join(root, ".doggo", "generations") + string(filepath.Separator)
			if strings.HasPrefix(contentRoot, generationPrefix) {
				t.Fatalf("audit content root uses project generation path: %q", contentRoot)
			}
			gotMod, err := os.ReadFile(filepath.Join(contentRoot, "toolchain", "go.mod"))
			if err != nil {
				return nil, err
			}
			gotGenerated, err := os.ReadFile(filepath.Join(contentRoot, "toolchain", filepath.FromSlash(generatedPath)))
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(gotMod, expectedMod) || !bytes.Equal(gotGenerated, expectedGenerated) {
				t.Fatal("audit copied replacement toolchain")
			}
			return nil, nil
		},
	}

	if _, err := auditor.Audit(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(auditRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private audit root remained after audit: %v", err)
	}
}

func TestAuditRejectsReplacedGenerationToolchainBeforeAuditLock(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}

	var replacement, retained string
	called := false
	auditor := Auditor{
		OpenGenerationSession: func(scriptPath string) (*project.GenerationSession, error) {
			session, err := project.OpenGenerationSession(scriptPath)
			if err != nil {
				return nil, err
			}
			replacement = filepath.Join(root, ".doggo", "generations", session.Config.Lock.Generation, "toolchain")
			retained = replaceDirectoryWithHardLinks(t, replacement)
			return session, nil
		},
		AuditLock: func(string, string, *project.ModFile, *project.LockFile) ([]DependencyAuditFinding, error) {
			called = true
			return nil, nil
		},
	}

	_, auditErr := auditor.Audit(root)
	if retained == "" {
		if auditErr != nil || !called {
			t.Fatalf("audit after blocked toolchain replacement: called=%v, error=%v", called, auditErr)
		}
		return
	}
	if auditErr == nil {
		t.Fatal("Audit accepted a replaced toolchain directory with validated hard links")
	}
	if called {
		t.Fatal("AuditLock consumed a snapshot from a replaced toolchain directory")
	}
	for _, path := range []string{replacement, retained} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("toolchain path %s was not preserved: %v", path, err)
		}
	}
}

func TestAuditPreparedLockUsesTentativeRoot(t *testing.T) {
	root := t.TempDir()
	stage := t.TempDir()
	mod := &project.ModFile{Module: "example/project", Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(root), mod); err != nil {
		t.Fatal(err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps:                []project.LockDep{},
		Scan:                project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	called := false
	auditor := Auditor{AuditLock: func(contentRoot, sourceRoot string, gotMod *project.ModFile, gotLock *project.LockFile) ([]DependencyAuditFinding, error) {
		called = true
		if contentRoot != stage || sourceRoot != root || gotMod.Module != mod.Module || gotLock != lock {
			t.Fatalf("audit args = %q %q %#v %p", contentRoot, sourceRoot, gotMod, gotLock)
		}
		return nil, nil
	}}
	if _, err := auditor.AuditPreparedLock(root, stage, lock); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("tentative audit callback was not called")
	}
}

func TestAuditPinsGenerationWithoutBlockingWriters(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	for i, want := range []error{nil, errors.New("audit failed")} {
		t.Run([]string{"success", "failure"}[i], func(t *testing.T) {

			var session *project.GenerationSession
			openSession := func(path string) (*project.GenerationSession, error) {
				var err error
				session, err = project.OpenGenerationSession(path)
				if session != nil {
					t.Cleanup(func() { _ = session.Close() })
				}
				return session, err
			}
			assertReadableWithConcurrentWriter := func() {
				t.Helper()
				done := make(chan error, 1)
				go func() {
					lease, err := project.AcquireGenerationLease(root, true)
					if err == nil {
						err = lease.Close()
					}
					done <- err
				}()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("generation reader blocked a writer")
				}
				temporary, err := project.NewOwnedTempRoot("snapshot-check-")
				if err != nil {
					t.Fatal(err)
				}
				defer temporary.Close()
				if err := session.CopyAuditWorkspaceTo(temporary.Tree()); err != nil {
					t.Fatalf("active snapshot became unavailable: %v", err)
				}
			}

			auditor := Auditor{OpenGenerationSession: openSession, AuditLock: func(_, _ string, _ *project.ModFile, _ *project.LockFile) ([]DependencyAuditFinding, error) {
				assertReadableWithConcurrentWriter()
				return nil, want
			}}
			if _, err := auditor.Audit(root); !errors.Is(err, want) {
				t.Fatalf("Audit error = %v, want %v", err, want)
			}

			if session == nil {
				t.Fatal("session was not opened")
			}
			if _, err := session.BinarySnapshot(); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("service leaked its generation session: %v", err)
			}
		})
	}
}

func replaceDirectoryWithHardLinks(t *testing.T, directory string) string {
	t.Helper()
	retained := directory + "-retained"
	if !testutil.RenameRetained(t, directory, retained) {
		return ""
	}
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(retained, func(source string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(retained, source)
		if err != nil || relative == "." {
			return err
		}
		destination := filepath.Join(directory, relative)
		if entry.IsDir() {
			return os.Mkdir(destination, 0755)
		}
		return os.Link(source, destination)
	}); err != nil {
		t.Fatal(err)
	}
	return retained
}
