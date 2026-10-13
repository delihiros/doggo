package project

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// GenerationOwner holds the lease and directory identities for one generation
// transaction. All mutations are relative to these retained descriptors.
type GenerationOwner struct {
	lease       *GenerationLease
	project     *os.Root
	projectPath string
	doggo       *os.Root
	generations *os.Root
	stage       *os.Root
	stageName   string
	stageTree   *OwnedTree
	projectTree *OwnedTree
	stagePrefix string
	borrowed    bool
}

var (
	afterOwnedTreeSourceOpenForTest     func(string)
	afterGenerationStageMkdirForTest    func(string)
	afterGenerationCleanupVerifyForTest func(string)
)

func BeginGenerationOwner(projectRoot string) (_ *GenerationOwner, err error) {
	project, projectPath, err := openVerifiedRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	return beginGenerationOwnerRoot(project, projectPath)
}

// BeginGenerationOwnerTree consumes project and records every newly-created
// entry directly in the caller-owned complete project tree.
func BeginGenerationOwnerTree(project *os.Root, projectPath string, tree *OwnedTree) (_ *GenerationOwner, err error) {
	if project == nil || tree == nil {
		if project != nil {
			err = project.Close()
		}
		return nil, errors.Join(fmt.Errorf("project root or tree is nil"), err)
	}
	absolute, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, errors.Join(err, project.Close())
	}
	projectInfo, projectErr := project.Stat(".")
	treeInfo, treeErr := tree.root.Stat(".")
	if err := errors.Join(revalidateRootPath(absolute, project), tree.VerifyComplete(), projectErr, treeErr); err != nil || !os.SameFile(projectInfo, treeInfo) {
		if err == nil {
			err = fmt.Errorf("project root does not match owned tree")
		}
		return nil, errors.Join(err, project.Close())
	}
	return beginGenerationOwnerTree(project, absolute, tree)
}

func beginGenerationOwnerRoot(project *os.Root, projectPath string) (_ *GenerationOwner, err error) {
	o := &GenerationOwner{project: project, projectPath: projectPath}
	defer func() {
		if err != nil {
			err = errors.Join(err, o.Close())
		}
	}()
	o.doggo, err = openOrCreateGenerationRoot(o.project, ".doggo", 0755)
	if err != nil {
		return nil, err
	}
	o.lease, err = acquireGenerationLeaseRoot(o.projectPath, o.project, o.doggo, true)
	if err != nil {
		return nil, err
	}
	o.generations, err = openOrCreateGenerationRoot(o.doggo, "generations", 0755)
	if err != nil {
		return nil, err
	}
	for {
		o.stageName = ".stage-" + rand.Text()
		if err := o.generations.Mkdir(o.stageName, 0755); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		break
	}
	created, err := o.generations.Lstat(o.stageName)
	if err != nil || !created.IsDir() || created.Mode()&os.ModeSymlink != 0 {
		if err == nil {
			err = fmt.Errorf("created generation stage is not an ordinary directory")
		}
		return nil, err
	}
	if afterGenerationStageMkdirForTest != nil {
		afterGenerationStageMkdirForTest(filepath.Join(o.projectPath, ".doggo", "generations", o.stageName))
	}
	stage, openErr := openVerifiedSubroot(o.generations, o.stageName)
	if openErr == nil {
		opened, statErr := stage.Stat(".")
		if statErr != nil || !os.SameFile(created, opened) {
			openErr = errors.Join(fmt.Errorf("created generation stage changed before opening"), statErr)
		}
	}
	if openErr != nil {
		return nil, errors.Join(openErr, closeOwnedTreeRoot(stage), RemoveCreatedEmptyRoot(o.generations, created))
	}
	o.stage = stage
	o.stagePrefix = filepath.ToSlash(filepath.Join(".doggo", "generations", o.stageName))
	o.stageTree = NewOwnedTree(o.stage)
	if err := o.verify(true); err != nil {
		return nil, err
	}
	return o, nil
}

func beginGenerationOwnerTree(project *os.Root, projectPath string, tree *OwnedTree) (_ *GenerationOwner, err error) {
	o := &GenerationOwner{project: project, projectPath: projectPath, projectTree: tree, borrowed: true}
	defer func() {
		if err != nil {
			err = errors.Join(err, o.Close())
		}
	}()
	o.doggo, err = tree.CreateRoot(".doggo", 0755, nil)
	if err != nil {
		return nil, err
	}
	if err := tree.WriteFile(".doggo/generation.lock", nil, 0600); err != nil {
		return nil, err
	}
	o.lease, err = acquireExistingGenerationLeaseRoot(o.projectPath, o.project, o.doggo, true)
	if err != nil {
		return nil, err
	}
	o.generations, err = tree.CreateRoot(".doggo/generations", 0755, nil)
	if err != nil {
		return nil, err
	}
	for {
		o.stageName = ".stage-" + rand.Text()
		o.stagePrefix = filepath.ToSlash(filepath.Join(".doggo", "generations", o.stageName))
		o.stage, err = tree.CreateRoot(o.stagePrefix, 0755, func() {
			if afterGenerationStageMkdirForTest != nil {
				afterGenerationStageMkdirForTest(filepath.Join(o.projectPath, filepath.FromSlash(o.stagePrefix)))
			}
		})
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	o.stageTree, err = tree.subtree(o.stagePrefix)
	if err != nil {
		return nil, err
	}
	if err := o.verify(true); err != nil {
		return nil, err
	}
	return o, nil
}

func openOrCreateGenerationRoot(parent *os.Root, name string, perm fs.FileMode) (*os.Root, error) {
	createdNew := false
	var created fs.FileInfo
	if err := parent.Mkdir(name, perm); err == nil {
		createdNew = true
		var statErr error
		created, statErr = parent.Lstat(name)
		if statErr != nil || !created.IsDir() || created.Mode()&os.ModeSymlink != 0 {
			if statErr == nil {
				statErr = fmt.Errorf("created generation directory is not ordinary: %s", name)
			}
			return nil, errors.Join(statErr, func() error {
				if created != nil {
					return RemoveCreatedEmptyRoot(parent, created)
				}
				return nil
			}())
		}
	} else if !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	root, err := openVerifiedSubroot(parent, name)
	if err == nil && createdNew {
		opened, statErr := root.Stat(".")
		if statErr != nil || !os.SameFile(created, opened) {
			err = errors.Join(fmt.Errorf("created generation directory changed before opening: %s", name), statErr)
		}
	}
	if err != nil {
		cleanupErr := error(nil)
		if createdNew {
			cleanupErr = RemoveCreatedEmptyRoot(parent, created)
		}
		return nil, errors.Join(err, closeOwnedTreeRoot(root), cleanupErr)
	}
	return root, nil
}

func (o *GenerationOwner) StagePath() string {
	if o == nil || o.stageName == "" {
		return ""
	}
	return filepath.Join(o.projectPath, ".doggo", "generations", o.stageName)
}

func (o *GenerationOwner) LoadMod() (*ModFile, error) {
	if err := o.verify(o.stage != nil); err != nil {
		return nil, err
	}
	content, err := readVerifiedRootFile(o.project, ModFileName)
	if err != nil {
		return nil, err
	}
	if err := o.verify(o.stage != nil); err != nil {
		return nil, err
	}
	return parseModFileBytes(ModFileName, content)
}

func (o *GenerationOwner) LoadLock() (*LockFile, error) {
	if err := o.verify(o.stage != nil); err != nil {
		return nil, err
	}
	content, err := readVerifiedRootFile(o.project, LockFileName)
	if err != nil {
		return nil, err
	}
	if err := o.verify(o.stage != nil); err != nil {
		return nil, err
	}
	return parseLockFileBytes(LockFileName, content)
}

func (o *GenerationOwner) VerifyStage() error { return o.verify(true) }

// StageTree is the transaction-wide identity ledger. Callers mutate the
// generation only through this concrete owner.
func (o *GenerationOwner) StageTree() (*OwnedTree, error) {
	if err := o.verify(true); err != nil {
		return nil, err
	}
	return o.stageTree, nil
}

func (o *GenerationOwner) OpenProjectRoot() (*os.Root, error) {
	if err := o.verify(o.stage != nil); err != nil {
		return nil, err
	}
	return CloneVerifiedRoot(o.project)
}

func (o *GenerationOwner) StageDigest(lock *LockFile) (string, error) {
	if err := o.verify(true); err != nil {
		return "", err
	}
	digest, err := generationDigestRoot(o.stage, lock)
	if err != nil {
		return "", err
	}
	if err := o.verify(true); err != nil {
		return "", err
	}
	return digest, nil
}

func (o *GenerationOwner) EnsureStageMarker(generation string) error {
	if !IsLowerHexDigest(generation) {
		return fmt.Errorf("invalid generation name")
	}
	if err := o.verify(true); err != nil {
		return err
	}
	expected := generation + "\n"
	before, err := o.stage.Lstat(generationMarkerName)
	if errors.Is(err, fs.ErrNotExist) {
		if err := o.stageTree.WriteFile(generationMarkerName, []byte(expected), 0644); err != nil {
			return fmt.Errorf("write generation marker: %w", err)
		}
	} else {
		if err != nil {
			return err
		}
		if !before.Mode().IsRegular() || before.Size() != int64(len(expected)) {
			return fmt.Errorf("generation marker is not the expected regular file")
		}
		content, err := readVerifiedRootFileInfo(o.stage, generationMarkerName, before)
		if err != nil {
			return err
		}
		if string(content) != expected {
			return fmt.Errorf("generation marker mismatch")
		}
	}
	return o.verify(true)
}

func (o *GenerationOwner) SyncStage() error {
	if err := o.verify(true); err != nil {
		return err
	}
	if err := o.stageTree.VerifyComplete(); err != nil {
		return err
	}
	if err := syncRootTree(o.stage); err != nil {
		return err
	}
	return o.verify(true)
}

// PublishStage returns true only when this owner performed the rename.
func (o *GenerationOwner) PublishStage(generation string) (_ bool, retErr error) {
	if !IsLowerHexDigest(generation) {
		return false, fmt.Errorf("invalid generation name")
	}
	if err := o.verify(true); err != nil {
		return false, err
	}
	if o.borrowed {
		oldPrefix := o.stagePrefix
		newPrefix := filepath.ToSlash(filepath.Join(".doggo", "generations", generation))
		err := o.projectTree.RenameRoot(oldPrefix, newPrefix)
		renamed := err == nil
		var atomicErr *AtomicWriteError
		if errors.As(err, &atomicErr) {
			renamed = atomicErr.Renamed
		}
		if renamed {
			o.stageName = generation
			o.stagePrefix = newPrefix
			o.stageTree.prefix = newPrefix
		}
		if err != nil {
			return renamed, err
		}
		parent, err := o.generations.Open(".")
		if err != nil {
			return true, &AtomicWriteError{Renamed: true, Err: err}
		}
		if err := errors.Join(atomicParentSync(parent), parent.Close(), o.verify(true)); err != nil {
			return true, &AtomicWriteError{Renamed: true, Err: err}
		}
		return true, nil
	}
	err := o.stageTree.PublishRoot(o.generations, o.stageName, generation)
	var atomicErr *AtomicWriteError
	renamed := err == nil || errors.As(err, &atomicErr) && atomicErr.Renamed
	if renamed {
		o.stageName = generation
		o.stagePrefix = filepath.ToSlash(filepath.Join(".doggo", "generations", generation))
	}
	err = errors.Join(err, o.verify(true))
	if err != nil {
		return renamed, &AtomicWriteError{Renamed: renamed, Err: err}
	}
	return true, nil
}

func (o *GenerationOwner) SyncPublished(generation string) (retErr error) {
	if !IsLowerHexDigest(generation) {
		return fmt.Errorf("invalid generation name")
	}
	if err := o.verify(o.stage != nil); err != nil {
		return err
	}
	entry, err := openVerifiedSubroot(o.generations, generation)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, entry.Close()) }()
	if o.stageName == generation {
		opened, statErr := entry.Stat(".")
		owned, ownedErr := o.stage.Stat(".")
		if statErr != nil || ownedErr != nil || !os.SameFile(opened, owned) {
			return fmt.Errorf("published generation identity changed")
		}
	}
	if err := syncRootTree(entry); err != nil {
		return err
	}
	parent, err := o.generations.Open(".")
	if err != nil {
		return err
	}
	if err := errors.Join(atomicParentSync(parent), parent.Close()); err != nil {
		return err
	}
	if err := revalidateSubroot(o.generations, generation, entry); err != nil {
		return err
	}
	return o.verify(o.stage != nil)
}

func (o *GenerationOwner) ValidatePublished(mod *ModFile, lock *LockFile) (retErr error) {
	if lock == nil || !IsLowerHexDigest(lock.Generation) {
		return fmt.Errorf("invalid generation lock")
	}
	if err := o.verify(o.stage != nil); err != nil {
		return err
	}
	entry, err := openVerifiedSubroot(o.generations, lock.Generation)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, entry.Close()) }()
	if o.stageName == lock.Generation {
		opened, statErr := entry.Stat(".")
		owned, ownedErr := o.stage.Stat(".")
		if statErr != nil || ownedErr != nil || !os.SameFile(opened, owned) {
			return fmt.Errorf("published generation identity changed")
		}
	}
	if err := validateGenerationRoot(entry, mod, lock); err != nil {
		return fmt.Errorf("%s is stale or corrupt: %w", LockFileName, err)
	}
	if err := revalidateSubroot(o.generations, lock.Generation, entry); err != nil {
		return err
	}
	return o.verify(o.stage != nil)
}

func (o *GenerationOwner) WriteLock(lock *LockFile) error {
	if lock == nil {
		return fmt.Errorf("lock file is nil")
	}
	if err := o.verify(o.stage != nil); err != nil {
		return err
	}
	var err error
	if o.borrowed {
		err = o.projectTree.WriteOwnedFile(LockFileName, []byte(canonicalLockString(lock)+"\n"), 0644)
	} else {
		err = WriteFileAtomicRoot(o.project, LockFileName, []byte(canonicalLockString(lock)+"\n"), 0644)
	}
	var atomicErr *AtomicWriteError
	if errors.As(err, &atomicErr) && atomicErr.Renamed {
		return &CommitUncertainError{Generation: lock.Generation, Cause: err}
	}
	if err != nil {
		return err
	}
	if err := o.verify(o.stage != nil); err != nil {
		return &CommitUncertainError{Generation: lock.Generation, Cause: err}
	}
	return nil
}

func (o *GenerationOwner) RemoveStage() error {
	if o == nil || o.stage == nil || o.stageName == "" {
		return nil
	}
	if err := o.verify(true); err != nil {
		return err
	}
	if afterGenerationCleanupVerifyForTest != nil {
		afterGenerationCleanupVerifyForTest(o.StagePath())
	}
	name := o.stageName
	if o.borrowed {
		if err := o.projectTree.RemoveSubtree(o.stagePrefix); err != nil {
			return err
		}
	} else {
		if err := o.stageTree.Cleanup(OwnedTreeCleanupHooks{}); err != nil {
			return err
		}
		if err := o.verify(true); err != nil {
			return err
		}
	}
	var removeErr error
	if !o.borrowed {
		removeErr = RemoveRetainedRoot(o.generations, name, o.stage)
	}
	closeErr := o.stage.Close()
	o.stage = nil
	o.stageTree = nil
	o.stageName = ""
	o.stagePrefix = ""
	if err := errors.Join(removeErr, closeErr); err != nil {
		return err
	}
	if o.borrowed {
		return o.verify(false)
	}
	return o.verify(false)
}

func (o *GenerationOwner) CopyGeneration(generation string) (retErr error) {
	if !IsLowerHexDigest(generation) {
		return fmt.Errorf("invalid generation name")
	}
	if err := o.verify(true); err != nil {
		return err
	}
	source, err := openVerifiedSubroot(o.generations, generation)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, source.Close()) }()
	sourceTree := NewOwnedTree(source)
	defer func() { retErr = errors.Join(retErr, sourceTree.Close()) }()
	if err := sourceTree.CaptureExisting(); err != nil {
		return err
	}
	skip := func(path string, _ fs.FileInfo) bool {
		return path == generationMarkerName || path == "bin"
	}
	if err := sourceTree.CopyTo(o.stageTree, skip); err != nil {
		return err
	}
	return errors.Join(revalidateSubroot(o.generations, generation, source), o.verify(true))
}

// AdoptPublishedGeneration replaces the temporary stage with an existing
// validated generation while retaining its exact directory identity chain.
func (o *GenerationOwner) AdoptPublishedGeneration(generation string) error {
	if o != nil && o.borrowed {
		return fmt.Errorf("borrowed generation cannot adopt an existing publication")
	}
	if !IsLowerHexDigest(generation) {
		return fmt.Errorf("invalid generation name")
	}
	if err := o.verify(true); err != nil {
		return err
	}
	existing, err := openVerifiedSubroot(o.generations, generation)
	if err != nil {
		return err
	}
	tree := NewOwnedTree(existing)
	if err := tree.CaptureExisting(); err != nil {
		return errors.Join(err, tree.Close(), existing.Close())
	}
	if err := o.RemoveStage(); err != nil {
		return errors.Join(err, tree.Close(), existing.Close())
	}
	if err := errors.Join(revalidateSubroot(o.generations, generation, existing), tree.VerifyComplete()); err != nil {
		return errors.Join(err, tree.Close(), existing.Close())
	}
	o.stage = existing
	o.stageName = generation
	o.stageTree = tree
	return nil
}

func (o *GenerationOwner) verify(stage bool) error {
	if o == nil || o.project == nil || o.doggo == nil || o.generations == nil {
		return fmt.Errorf("generation owner is closed")
	}
	if err := errors.Join(
		revalidateRootPath(o.projectPath, o.project),
		revalidateSubroot(o.project, ".doggo", o.doggo),
		revalidateSubroot(o.doggo, "generations", o.generations),
	); err != nil {
		return err
	}
	if o.borrowed {
		if o.projectTree == nil {
			return fmt.Errorf("borrowed project tree is not owned")
		}
		if err := o.projectTree.VerifyComplete(); err != nil {
			return err
		}
	}
	if stage {
		if o.stage == nil || filepath.Base(o.stageName) != o.stageName || (!IsLowerHexDigest(o.stageName) && (!strings.HasPrefix(o.stageName, ".stage-") || len(o.stageName) == len(".stage-"))) {
			return fmt.Errorf("generation stage is not owned")
		}
		if err := revalidateSubroot(o.generations, o.stageName, o.stage); err != nil {
			return err
		}
		if o.stageTree == nil {
			return fmt.Errorf("generation stage tree is not owned")
		}
		return o.stageTree.VerifyComplete()
	}
	return nil
}

func (o *GenerationOwner) Close() error {
	if o == nil {
		return nil
	}
	err := errors.Join(closeOwnedStageTree(&o.stageTree), closeRoot(&o.stage), closeRoot(&o.generations), closeRoot(&o.doggo), closeRoot(&o.project))
	o.projectTree = nil
	lease := o.lease
	o.lease = nil
	return errors.Join(err, lease.Close())
}

func closeOwnedStageTree(tree **OwnedTree) error {
	if tree == nil || *tree == nil {
		return nil
	}
	value := *tree
	*tree = nil
	return value.Close()
}

func closeRoot(root **os.Root) error {
	if root == nil || *root == nil {
		return nil
	}
	value := *root
	*root = nil
	return value.Close()
}
