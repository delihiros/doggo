package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBorrowedOwnedTreeRenameUpdatesOuterLedger(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	outer := NewOwnedTree(root)
	defer outer.Close()
	stage, err := outer.CreateRoot("stage", 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	borrowed, err := outer.subtree("stage")
	if err != nil {
		t.Fatal(err)
	}
	temporary, err := borrowed.CreateRoot("temporary", 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}
	if err := borrowed.WriteFile("temporary/value", []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := borrowed.RenameRoot("temporary", "final"); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(borrowed.VerifyComplete(), outer.VerifyComplete()); err != nil {
		t.Fatalf("renamed borrowed tree diverged from outer ledger: %v", err)
	}
}

func TestGenerationOwnerRetainsPublicationAfterPublisherCommitError(t *testing.T) {
	for _, borrowed := range []bool{false, true} {
		name := "owned"
		if borrowed {
			name = "borrowed"
		}
		t.Run(name, func(t *testing.T) {
			projectPath := t.TempDir()
			var owner *GenerationOwner
			var err error
			if borrowed {
				root, openErr := os.OpenRoot(projectPath)
				if openErr != nil {
					t.Fatal(openErr)
				}
				outer := NewOwnedTree(root)
				t.Cleanup(func() { _ = outer.Close() })
				owner, err = BeginGenerationOwnerTree(root, projectPath, outer)
			} else {
				owner, err = BeginGenerationOwner(projectPath)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = owner.Close() })

			generation := strings.Repeat("d", 64)
			want := errors.New("publisher failed after rename")
			original := generationRenameNoReplace
			generationRenameNoReplace = func(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
				renamed, err := original(parent, retained, oldName, newName)
				if err != nil {
					return renamed, err
				}
				return true, want
			}
			t.Cleanup(func() { generationRenameNoReplace = original })

			renamed, err := owner.PublishStage(generation)
			var atomicErr *AtomicWriteError
			if !renamed || !errors.As(err, &atomicErr) || !atomicErr.Renamed || !errors.Is(err, want) {
				t.Fatalf("PublishStage = (%v, %T %v), want renamed publisher failure", renamed, err, err)
			}
			if err := owner.VerifyStage(); err != nil {
				t.Fatalf("publication ownership was not transferred: %v", err)
			}
			if err := owner.SyncPublished(generation); err != nil {
				t.Fatalf("SyncPublished after commit error: %v", err)
			}
			if renamed, err := owner.PublishStage(generation); renamed || !errors.Is(err, fs.ErrExist) {
				t.Fatalf("retry PublishStage = (%v, %v), want existing publication", renamed, err)
			}
			if err := owner.RemoveStage(); err != nil {
				t.Fatalf("abort cleanup of committed publication: %v", err)
			}
			published := filepath.Join(projectPath, ".doggo", "generations", generation)
			if _, err := os.Lstat(published); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("abort cleanup left publication: %v", err)
			}
		})
	}
}

func TestBorrowedGenerationOwnerTransfersStateBeforePostRenameVerification(t *testing.T) {
	projectPath := t.TempDir()
	root, err := os.OpenRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	outer := NewOwnedTree(root)
	defer outer.Close()
	owner, err := BeginGenerationOwnerTree(root, projectPath, outer)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	generation := strings.Repeat("e", 64)
	published := filepath.Join(projectPath, ".doggo", "generations", generation)
	retained := published + "-retained"
	want := errors.New("publisher failed after rename")
	original := generationRenameNoReplace
	generationRenameNoReplace = func(parent *os.File, stage *os.Root, oldName, newName string) (bool, error) {
		renamed, err := original(parent, stage, oldName, newName)
		if err != nil {
			return renamed, err
		}
		if err := os.Rename(published, retained); err != nil {
			return true, err
		}
		if err := os.Mkdir(published, 0755); err != nil {
			return true, err
		}
		if err := os.WriteFile(filepath.Join(published, "replacement"), []byte("racer"), 0644); err != nil {
			return true, err
		}
		return true, want
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	renamed, err := owner.PublishStage(generation)
	if !renamed || !errors.Is(err, want) {
		t.Fatalf("PublishStage = (%v, %v), want post-rename failure", renamed, err)
	}
	if owner.stageName != generation || owner.stagePrefix != filepath.ToSlash(filepath.Join(".doggo", "generations", generation)) || owner.stageTree == nil || owner.stageTree.prefix != owner.stagePrefix {
		t.Fatalf("publication state was not transferred at commit point: name=%q prefix=%q tree=%#v", owner.stageName, owner.stagePrefix, owner.stageTree)
	}
	content, readErr := os.ReadFile(filepath.Join(published, "replacement"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
	if err := os.Remove(filepath.Join(published, "replacement")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(published); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, published); err != nil {
		t.Fatal(err)
	}
	if err := owner.VerifyStage(); err != nil {
		t.Fatalf("restored publication identity is not recoverable: %v", err)
	}
	if err := owner.RemoveStage(); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationOwnerCleanupPreservesRetainedReplacementAfterVerification(t *testing.T) {
	root := t.TempDir()
	owner, err := BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	tree, err := owner.StageTree()
	if err != nil {
		t.Fatal(err)
	}
	toolchain, err := tree.CreateRoot("toolchain", 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := toolchain.Close(); err != nil {
		t.Fatal(err)
	}
	var replacement string
	afterGenerationCleanupVerifyForTest = func(stagePath string) {
		replacement = filepath.Join(stagePath, "toolchain")
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
	t.Cleanup(func() { afterGenerationCleanupVerifyForTest = nil })

	if err := owner.RemoveStage(); err == nil {
		t.Fatal("RemoveStage accepted a retained replacement after verification")
	}
	content, readErr := os.ReadFile(filepath.Join(replacement, "replacement"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
}

func TestGenerationOwnerRemoveStagePreservesFinalBoundaryReplacement(t *testing.T) {
	owner, err := BeginGenerationOwner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	parent, name := owner.generations, owner.stageName
	replacement := owner.StagePath()
	beforeRetainedRemoveForTest = func(gotParent *os.Root, gotName string) {
		if gotParent != parent || gotName != name {
			return
		}
		if err := os.Rename(replacement, replacement+"-retained"); err != nil {
			t.Fatal(err)
		}
		if err := gotParent.Mkdir(gotName, 0755); err != nil {
			t.Fatal(err)
		}
		if err := gotParent.WriteFile(gotName+"/replacement", []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeRetainedRemoveForTest = nil })

	if err := owner.RemoveStage(); !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("RemoveStage error = %v, want ownership failure", err)
	}
	content, readErr := os.ReadFile(filepath.Join(replacement, "replacement"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
}

func TestGenerationOwnerCleanupPreservesUnrecordedEntryReplacement(t *testing.T) {
	for _, kind := range []string{"directory", "file"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			owner, err := BeginGenerationOwner(root)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			tree, err := owner.StageTree()
			if err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				root, err := tree.CreateRoot("entry", 0755, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := root.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := tree.WriteFile("entry", []byte("owned"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var marker string
			afterGenerationCleanupVerifyForTest = func(stagePath string) {
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
			t.Cleanup(func() { afterGenerationCleanupVerifyForTest = nil })

			if err := owner.RemoveStage(); err == nil {
				t.Fatalf("RemoveStage accepted an unrecorded %s replacement", kind)
			}
			content, readErr := os.ReadFile(marker)
			if readErr != nil || string(content) != "racer" {
				t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
			}
		})
	}
}

func TestOwnedTreeCreateRootCleansOnlyCreatedIdentityAfterReplacement(t *testing.T) {
	base := t.TempDir()
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := NewOwnedTree(root)
	defer tree.Close()
	var replacement string
	afterCreate := func() {
		replacement = filepath.Join(base, "bin")
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

	if created, err := tree.CreateRoot("bin", 0755, afterCreate); err == nil {
		_ = created.Close()
		t.Fatal("OwnedTree.CreateRoot accepted a replacement")
	}
	content, readErr := os.ReadFile(filepath.Join(replacement, "replacement"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
	if _, err := os.Lstat(replacement + "-owned"); !os.IsNotExist(err) {
		t.Fatalf("created directory leaked after failed open: %v", err)
	}
}

func TestBeginGenerationOwnerCleansOnlyCreatedStageIdentityAfterReplacement(t *testing.T) {
	projectRoot := t.TempDir()
	var replacement string
	afterGenerationStageMkdirForTest = func(path string) {
		replacement = path
		if err := os.Rename(path, path+"-owned"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterGenerationStageMkdirForTest = nil })

	owner, err := BeginGenerationOwner(projectRoot)
	if owner != nil {
		_ = owner.Close()
	}
	if err == nil {
		t.Fatal("BeginGenerationOwner accepted a replaced stage")
	}
	if content, readErr := os.ReadFile(filepath.Join(replacement, "replacement")); readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
	if _, err := os.Lstat(replacement + "-owned"); !os.IsNotExist(err) {
		t.Fatalf("created generation stage leaked after failed open: %v", err)
	}
}

func TestCopyGenerationRetainsCopiedChildRoots(t *testing.T) {
	root := t.TempDir()
	first, err := BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := first.StageTree()
	if err != nil {
		t.Fatal(err)
	}
	toolchain, err := tree.CreateRoot("toolchain", 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := toolchain.Close(); err != nil {
		t.Fatal(err)
	}
	generation := strings.Repeat("b", 64)
	if _, err := first.PublishStage(generation); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.CopyGeneration(generation); err != nil {
		t.Fatal(err)
	}
	secondTree, err := second.StageTree()
	if err != nil {
		t.Fatal(err)
	}
	copiedToolchain, err := secondTree.OpenRoot("toolchain")
	if err != nil {
		t.Fatalf("copied toolchain was not retained: %v", err)
	}
	if err := copiedToolchain.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationOwnerCopyGenerationRejectsSourceReplacement(t *testing.T) {
	for _, target := range []string{"file", "directory"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			if err := WriteModFile(ModPath(root), &ModFile{Deps: []string{}}); err != nil {
				t.Fatal(err)
			}
			generation := strings.Repeat("a", 64)
			source := filepath.Join(root, ".doggo", "generations", generation, "toolchain")
			if err := os.MkdirAll(source, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "main.go"), []byte("owned"), 0644); err != nil {
				t.Fatal(err)
			}
			owner, err := BeginGenerationOwner(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = owner.Close() })

			fired := false
			afterOwnedTreeSourceOpenForTest = func(relative string) {
				if fired || (target == "file" && relative != "toolchain/main.go") || (target == "directory" && relative != "toolchain") {
					return
				}
				fired = true
				path := filepath.Join(root, ".doggo", "generations", generation, filepath.FromSlash(relative))
				if err := os.Rename(path, path+"-owned"); err != nil {
					t.Fatal(err)
				}
				if target == "directory" {
					if err := os.Mkdir(path, 0755); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(path, "main.go")
				}
				if err := os.WriteFile(path, []byte("outside"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { afterOwnedTreeSourceOpenForTest = nil })

			if err := owner.CopyGeneration(generation); err == nil {
				t.Fatal("CopyGeneration accepted replaced source identity")
			}
			if content, err := os.ReadFile(filepath.Join(owner.StagePath(), "toolchain", "main.go")); err == nil && string(content) == "outside" {
				t.Fatal("CopyGeneration copied bytes from replacement source")
			}
		})
	}
}
