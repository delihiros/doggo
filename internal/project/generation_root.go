package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrOwnershipChanged = errors.New("owned directory identity changed")

var (
	afterVerifiedRootCloneForTest func(*os.Root)
	closeVerifiedRootClone        = (*os.Root).Close
)

// CloneVerifiedRoot retains the same directory with an independently owned
// handle. The caller must close the returned root.
func CloneVerifiedRoot(root *os.Root) (*os.Root, error) {
	before, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	clone, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	if afterVerifiedRootCloneForTest != nil {
		afterVerifiedRootCloneForTest(clone)
	}
	opened, statErr := clone.Stat(".")
	current, currentErr := root.Stat(".")
	if statErr != nil || currentErr != nil || !os.SameFile(before, opened) || !os.SameFile(opened, current) {
		return nil, errors.Join(fmt.Errorf("root changed while retaining it"), statErr, currentErr, closeVerifiedRootClone(clone))
	}
	return clone, nil
}

func openVerifiedRoot(path string) (*os.Root, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	before, err := os.Lstat(abs)
	if err != nil {
		return nil, "", err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, "", fmt.Errorf("root is a symlink")
	}
	if !before.IsDir() {
		return nil, "", fmt.Errorf("root is not a directory")
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, "", err
	}
	opened, statErr := root.Stat(".")
	after, lstatErr := os.Lstat(abs)
	if statErr != nil || lstatErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return nil, "", errors.Join(fmt.Errorf("%w: root changed while opening", ErrOwnershipChanged), statErr, lstatErr, root.Close())
	}
	return root, abs, nil
}

// OpenVerifiedRoot opens an ordinary directory and verifies its path identity.
func OpenVerifiedRoot(path string) (*os.Root, string, error) { return openVerifiedRoot(path) }

func openVerifiedSubroot(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is a symlink", name)
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", name)
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, statErr := root.Stat(".")
	after, lstatErr := parent.Lstat(name)
	if statErr != nil || lstatErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return nil, errors.Join(fmt.Errorf("%w: %s changed while opening", ErrOwnershipChanged, name), statErr, lstatErr, root.Close())
	}
	return root, nil
}

// OpenVerifiedSubroot opens a directory relative to a retained parent root.
func OpenVerifiedSubroot(parent *os.Root, name string) (*os.Root, error) {
	return openVerifiedSubroot(parent, name)
}

func revalidateRootPath(path string, root *os.Root) error {
	opened, statErr := root.Stat(".")
	current, lstatErr := os.Lstat(path)
	if statErr != nil || lstatErr != nil || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(opened, current) {
		return errors.Join(fmt.Errorf("%w: root path changed", ErrOwnershipChanged), statErr, lstatErr)
	}
	return nil
}

// VerifyRootPath verifies that path still names the retained root.
func VerifyRootPath(path string, root *os.Root) error { return revalidateRootPath(path, root) }

// VerifyRootHandle verifies that opened is the exact ordinary directory
// retained by root.
func VerifyRootHandle(root *os.Root, opened *os.File) error {
	if root == nil || opened == nil {
		return fmt.Errorf("%w: root handle is nil", ErrOwnershipChanged)
	}
	retained, retainedErr := root.Stat(".")
	current, currentErr := opened.Stat()
	if retainedErr != nil || currentErr != nil || !retained.IsDir() || !current.IsDir() || !os.SameFile(retained, current) {
		return errors.Join(fmt.Errorf("%w: opened root handle changed", ErrOwnershipChanged), retainedErr, currentErr)
	}
	return nil
}

func revalidateSubroot(parent *os.Root, name string, root *os.Root) error {
	opened, statErr := root.Stat(".")
	current, lstatErr := parent.Lstat(name)
	if statErr != nil || lstatErr != nil || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(opened, current) {
		return errors.Join(fmt.Errorf("%w: %s changed", ErrOwnershipChanged, name), statErr, lstatErr)
	}
	return nil
}

// VerifySubroot verifies that name still identifies the retained child root.
func VerifySubroot(parent *os.Root, name string, root *os.Root) error {
	return revalidateSubroot(parent, name, root)
}

func revalidateRootFile(parent *os.Root, name string, file *os.File) error {
	opened, statErr := file.Stat()
	current, lstatErr := parent.Lstat(name)
	if statErr != nil || lstatErr != nil || current.Mode()&os.ModeSymlink != 0 || !opened.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return fmt.Errorf("%s changed", name)
	}
	return nil
}
