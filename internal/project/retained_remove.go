package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var beforeRetainedRemoveForTest func(*os.Root, string)

type retainedRemovalCommittedError struct{ err error }

func (e *retainedRemovalCommittedError) Error() string { return e.err.Error() }
func (e *retainedRemovalCommittedError) Unwrap() error { return e.err }

func removeRetainedFile(parent *os.Root, name string, expected fs.FileInfo) error {
	if parent == nil || !validRetainedName(name) || expected == nil {
		return fmt.Errorf("%w: retained file is invalid", ErrOwnershipChanged)
	}
	if !expected.Mode().IsRegular() && expected.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%w: retained entry is not a regular file or symlink: %s", ErrOwnershipChanged, name)
	}
	return removeRetainedEntry(parent, name, expected, nil, false)
}

// RemoveRetainedRoot removes an empty ordinary directory while its retained
// root and parent remain open.
func RemoveRetainedRoot(parent *os.Root, name string, root *os.Root) error {
	if parent == nil || root == nil || !validRetainedName(name) {
		return fmt.Errorf("%w: retained directory is invalid", ErrOwnershipChanged)
	}
	expected, err := root.Stat(".")
	if err != nil || !expected.IsDir() || expected.Mode()&os.ModeSymlink != 0 {
		return errors.Join(fmt.Errorf("%w: retained entry is not an ordinary directory: %s", ErrOwnershipChanged, name), err)
	}
	contents, err := root.Open(".")
	if err != nil {
		return err
	}
	names, readErr := contents.Readdirnames(-1)
	closeErr := contents.Close()
	if readErr != nil || closeErr != nil || len(names) != 0 {
		if len(names) != 0 {
			readErr = fmt.Errorf("%w: retained directory is not empty: %s", ErrOwnershipChanged, name)
		}
		return errors.Join(readErr, closeErr)
	}
	return removeRetainedEntry(parent, name, expected, root, true)
}

func validRetainedName(name string) bool {
	return name != "." && filepath.IsLocal(name) && filepath.Base(name) == name
}

func sameRetainedEntry(before, after fs.FileInfo) bool {
	return before != nil && after != nil && before.Mode().Type() == after.Mode().Type() && os.SameFile(before, after)
}

func retainedRemoveOwnershipError(name string, err error) error {
	return errors.Join(fmt.Errorf("%w: retained entry changed before removal: %s", ErrOwnershipChanged, name), err)
}

func retainedDispositionResult(name string, changed bool, currentErr, dispositionErr error) error {
	if !changed {
		return dispositionErr
	}
	return errors.Join(retainedRemoveOwnershipError(name, currentErr), dispositionErr)
}

func joinRetainedRemovalClose(operationErr error, committed bool, closeErr error) error {
	err := errors.Join(operationErr, closeErr)
	if !committed || err == nil {
		return err
	}
	var marker *retainedRemovalCommittedError
	if errors.As(err, &marker) {
		return err
	}
	return &retainedRemovalCommittedError{err: err}
}

func retainedRemovalCommitted(err error) bool {
	if err == nil {
		return true
	}
	var marker *retainedRemovalCommittedError
	return errors.As(err, &marker)
}

func finishOwnedRemoval[T any](entries map[string]T, key string, err error) error {
	if retainedRemovalCommitted(err) {
		delete(entries, key)
	}
	return err
}
