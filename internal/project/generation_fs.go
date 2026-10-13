package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var generationRenameNoReplace = renameGenerationNoReplace

func renameGenerationNoReplace(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
	// Some platforms report a successful no-op for rename(name, name), but a
	// publication must never claim that an existing destination was created.
	if oldName == newName {
		return false, fs.ErrExist
	}
	return renameGenerationNoReplaceOS(parent, retained, oldName, newName)
}

type CommitUncertainError struct {
	Generation string
	Cause      error
}

func (e *CommitUncertainError) Error() string {
	return fmt.Sprintf("generation %s lock commit durability is uncertain: %v", e.Generation, e.Cause)
}

func (e *CommitUncertainError) Unwrap() error { return e.Cause }

func SyncTree(path string) error {
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("generation tree root is a symlink")
	}
	if !before.IsDir() {
		return fmt.Errorf("generation tree root is not a directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil {
		return err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return fmt.Errorf("generation tree root changed while opening")
	}
	return syncRootTree(root)
}

// SyncRootTree synchronizes a tree through an already-open project root.
func SyncRootTree(root *os.Root) error {
	if root == nil {
		return fmt.Errorf("generation tree root is nil")
	}
	return syncRootTree(root)
}

func syncRootTree(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	for _, name := range names {
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("generation tree entry %s is a symlink", name)
		case info.IsDir():
			child, err := root.OpenRoot(name)
			if err != nil {
				return err
			}
			opened, statErr := child.Stat(".")
			after, lstatErr := root.Lstat(name)
			if statErr != nil || lstatErr != nil || !os.SameFile(info, opened) || !os.SameFile(opened, after) {
				child.Close()
				return fmt.Errorf("generation tree directory %s changed while opening", name)
			}
			err = syncRootTree(child)
			closeErr := child.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		case info.Mode().IsRegular():
			if err := syncRootFile(root, name, info); err != nil {
				return err
			}
		default:
			return fmt.Errorf("generation tree entry %s is special (%s)", name, info.Mode().Type())
		}
	}
	directory, err = root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(syncDirectory(directory), directory.Close())
}

func syncRootFile(root *os.Root, name string, before fs.FileInfo) (retErr error) {
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, file.Close()) }()
	opened, statErr := file.Stat()
	after, lstatErr := root.Lstat(name)
	if statErr != nil || lstatErr != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return fmt.Errorf("generation tree file %s changed while opening", name)
	}
	return syncFile(file)
}

// PublishRetainedDirectory atomically publishes one retained child directory
// under its retained parent without replacing an existing destination.
func PublishRetainedDirectory(parent, stage *os.Root, stageName, destinationName string) error {
	if parent == nil || stage == nil {
		return fmt.Errorf("directory publisher root is nil")
	}
	if filepath.Base(stageName) != stageName || filepath.Base(destinationName) != destinationName || stageName == "." || destinationName == "." {
		return fmt.Errorf("directory publisher names must be single components")
	}
	parentFile, err := parent.Open(".")
	if err != nil {
		return err
	}
	if err := revalidateSubroot(parent, stageName, stage); err != nil {
		return errors.Join(err, parentFile.Close())
	}
	renamed, publishErr := generationRenameNoReplace(parentFile, stage, stageName, destinationName)
	if !renamed {
		if publishErr == nil {
			publishErr = fmt.Errorf("directory publisher did not rename stage")
		}
		return errors.Join(&AtomicWriteError{Err: publishErr}, parentFile.Close())
	}
	err = errors.Join(publishErr, revalidateSubroot(parent, destinationName, stage), atomicParentSync(parentFile), parentFile.Close())
	if err != nil {
		return &AtomicWriteError{Renamed: true, Err: err}
	}
	return nil
}

func PublishDirectory(stage, destination string) error {
	stage, err := filepath.Abs(stage)
	if err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	parent := filepath.Dir(stage)
	if filepath.Dir(destination) != parent || filepath.Base(parent) != "generations" || filepath.Base(filepath.Dir(parent)) != ".doggo" {
		return fmt.Errorf("generation publication must stay within one .doggo/generations directory")
	}
	projectRoot := filepath.Dir(filepath.Dir(parent))
	project, projectPath, err := openVerifiedRoot(projectRoot)
	if err != nil {
		return err
	}
	defer project.Close()
	doggo, err := openVerifiedSubroot(project, ".doggo")
	if err != nil {
		return err
	}
	defer doggo.Close()
	generations, err := openVerifiedSubroot(doggo, "generations")
	if err != nil {
		return err
	}
	defer generations.Close()
	stageName := filepath.Base(stage)
	destinationName := filepath.Base(destination)
	stageRoot, err := openVerifiedSubroot(generations, stageName)
	if err != nil {
		return fmt.Errorf("generation stage: %w", err)
	}
	defer stageRoot.Close()
	if err := revalidateSubroot(generations, stageName, stageRoot); err != nil {
		return err
	}
	if err := PublishRetainedDirectory(generations, stageRoot, stageName, destinationName); err != nil {
		return err
	}
	if err := errors.Join(
		revalidateRootPath(projectPath, project),
		revalidateSubroot(project, ".doggo", doggo),
		revalidateSubroot(doggo, "generations", generations),
	); err != nil {
		return &AtomicWriteError{Renamed: true, Err: err}
	}
	return nil
}

func WriteLockFileAtomic(path string, lock *LockFile) error {
	if lock == nil {
		return fmt.Errorf("lock file is nil")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, rootPath, err := openVerifiedRoot(filepath.Dir(absPath))
	if err != nil {
		return err
	}
	defer root.Close()
	content := []byte(canonicalLockString(lock) + "\n")
	err = WriteFileAtomicRoot(root, filepath.Base(absPath), content, 0644)
	var atomicErr *AtomicWriteError
	if errors.As(err, &atomicErr) && atomicErr.Renamed {
		return &CommitUncertainError{Generation: lock.Generation, Cause: err}
	}
	if err != nil {
		return err
	}
	if err := revalidateRootPath(rootPath, root); err != nil {
		return &CommitUncertainError{Generation: lock.Generation, Cause: err}
	}
	return nil
}

func IsLowerHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}
