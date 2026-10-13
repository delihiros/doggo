package newproject

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"doggo/internal/project"
)

var (
	afterNewProjectParentOpenForTest  func(string)
	afterNewProjectCommitForTest      func(string) error
	afterNewProjectGenerationForTest  func(string) error
	beforeNewProjectGenerationForTest func(string)
	beforeNewProjectRenameForTest     func(stage, target string) error
	publishProjectTree                = (*project.OwnedTree).PublishRoot
	beforeOpenNewProjectStageForTest  func(parent, name string)
	beforeRemoveProjectStageForTest   func(string)
	beforeRemoveProjectEntryForTest   func(string)
	afterProjectRetainedVerifyForTest func(string)
)

type PublishUncertainError struct {
	Target string
	Cause  error
}

func (e *PublishUncertainError) Error() string {
	return fmt.Sprintf("project %s publication durability is uncertain: %v", e.Target, e.Cause)
}

func (e *PublishUncertainError) Unwrap() error { return e.Cause }

type projectStage struct {
	diagnosticParent string
	parent           *os.Root
	root             *os.Root
	name             string
	published        bool
	preserve         bool
	tree             *project.OwnedTree
}

func beginProjectStage(parentPath string) (*projectStage, error) {
	parent, absoluteParent, err := project.OpenVerifiedRoot(parentPath)
	if err != nil {
		return nil, err
	}
	if afterNewProjectParentOpenForTest != nil {
		afterNewProjectParentOpenForTest(absoluteParent)
	}
	var name string
	for {
		name = ".doggo-new-" + rand.Text()
		if err := parent.Mkdir(name, 0755); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return nil, errors.Join(err, parent.Close())
		}
		break
	}
	created, err := parent.Lstat(name)
	if err != nil {
		return nil, errors.Join(err, parent.Close())
	}
	if beforeOpenNewProjectStageForTest != nil {
		beforeOpenNewProjectStageForTest(absoluteParent, name)
	}
	stage := &projectStage{
		diagnosticParent: absoluteParent,
		parent:           parent,
		name:             name,
	}
	opened, openErr := project.OpenVerifiedSubroot(parent, stage.name)
	if openErr == nil {
		openedInfo, statErr := opened.Stat(".")
		if statErr != nil || !os.SameFile(created, openedInfo) {
			openErr = fmt.Errorf("temporary project changed before opening")
		}
	}
	if openErr != nil {
		closeErr := closeProjectRoot(&opened)
		cleanupErr := project.RemoveCreatedEmptyRoot(parent, created)
		return nil, errors.Join(openErr, closeErr, cleanupErr, parent.Close())
	}
	stage.root = opened
	stage.tree = project.NewOwnedTree(opened)
	if err := project.VerifyRootPath(absoluteParent, parent); err != nil {
		return stage, fmt.Errorf("target parent changed: %w", err)
	}
	return stage, stage.verify()
}

func (s *projectStage) Path() string {
	if s == nil {
		return ""
	}
	return filepath.Join(s.diagnosticParent, s.name)
}

func (s *projectStage) verify() error {
	if s == nil || s.parent == nil || s.root == nil || s.name == "" || s.published {
		return fmt.Errorf("temporary project is not owned")
	}
	if err := project.VerifySubroot(s.parent, s.name, s.root); err != nil {
		return fmt.Errorf("temporary project changed: %w", err)
	}
	if s.tree == nil {
		return fmt.Errorf("temporary project tree is not owned")
	}
	if err := s.tree.VerifyComplete(); err != nil {
		return err
	}
	return nil
}

func publishNewProject(stage *projectStage, target string) (retErr error) {
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	targetParent := filepath.Dir(absoluteTarget)
	if filepath.Base(absoluteTarget) == "." {
		return fmt.Errorf("project publication requires a target name")
	}
	if err := project.VerifyRootPath(targetParent, stage.parent); err != nil {
		return fmt.Errorf("project publication must use the retained parent directory: %w", err)
	}
	if err := stage.verify(); err != nil {
		return err
	}
	if beforeNewProjectRenameForTest != nil {
		if err := beforeNewProjectRenameForTest(stage.Path(), absoluteTarget); err != nil {
			return err
		}
	}
	if err := stage.verify(); err != nil {
		return err
	}
	err = publishProjectTree(stage.tree, stage.parent, stage.name, filepath.Base(absoluteTarget))
	var atomicErr *project.AtomicWriteError
	renamed := err == nil || errors.As(err, &atomicErr) && atomicErr.Renamed
	if renamed {
		stage.published = true
		stage.name = filepath.Base(absoluteTarget)
	}
	if err != nil {
		if renamed {
			return &PublishUncertainError{Target: absoluteTarget, Cause: err}
		}
		return err
	}
	if err := stage.verifyPublished(targetParent); err != nil {
		return &PublishUncertainError{Target: absoluteTarget, Cause: err}
	}
	return nil
}

func (s *projectStage) Close() error {
	if s == nil {
		return nil
	}
	var cleanupErr error
	if !s.published && !s.preserve {
		if verifyErr := s.tree.VerifyComplete(); verifyErr != nil {
			cleanupErr = verifyErr
		} else {
			if afterProjectRetainedVerifyForTest != nil {
				afterProjectRetainedVerifyForTest(s.Path())
			}
			cleanupErr = s.removeOwned()
		}
	}
	closeErr := errors.Join(closeProjectTree(&s.tree), closeProjectRoot(&s.root), closeProjectRoot(&s.parent))
	if s.published && closeErr != nil {
		closeErr = &PublishUncertainError{Target: s.Path(), Cause: closeErr}
	}
	return errors.Join(cleanupErr, closeErr)
}

func (s *projectStage) verifyPublished(targetParent string) error {
	if err := project.VerifyRootPath(targetParent, s.parent); err != nil {
		return fmt.Errorf("published project parent changed: %w", err)
	}
	if err := project.VerifySubroot(s.parent, s.name, s.root); err != nil {
		return fmt.Errorf("published project changed: %w", err)
	}
	return s.tree.VerifyComplete()
}

func closeProjectTree(tree **project.OwnedTree) error {
	if tree == nil || *tree == nil {
		return nil
	}
	value := *tree
	*tree = nil
	return value.Close()
}

func (s *projectStage) removeOwned() error {
	if s.root == nil || s.parent == nil {
		return nil
	}
	hooks := project.OwnedTreeCleanupHooks{
		BeforeRemove: func(path string) {
			if beforeRemoveProjectEntryForTest != nil {
				beforeRemoveProjectEntryForTest(filepath.Base(filepath.FromSlash(path)))
			}
		},
	}
	if err := s.tree.Cleanup(hooks); err != nil {
		return err
	}
	owned, err := s.root.Stat(".")
	if err != nil {
		return err
	}
	name, err := findProjectStageName(s.parent, owned)
	if err != nil {
		return err
	}
	if beforeRemoveProjectStageForTest != nil {
		beforeRemoveProjectStageForTest(name)
	}
	removeErr := project.RemoveRetainedRoot(s.parent, name, s.root)
	closeErr := s.root.Close()
	s.root = nil
	return errors.Join(removeErr, closeErr)
}

func findProjectStageName(parent *os.Root, owned fs.FileInfo) (string, error) {
	directory, err := parent.Open(".")
	if err != nil {
		return "", err
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		return "", errors.Join(readErr, closeErr)
	}
	for _, name := range names {
		current, err := parent.Lstat(name)
		if err == nil && current.IsDir() && current.Mode()&os.ModeSymlink == 0 && os.SameFile(owned, current) {
			return name, nil
		}
	}
	return "", fmt.Errorf("owned temporary project entry is no longer in its parent")
}

func closeProjectRoot(root **os.Root) error {
	if root == nil || *root == nil {
		return nil
	}
	value := *root
	*root = nil
	return value.Close()
}
