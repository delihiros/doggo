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

var beforeOwnedTempRootOpenForTest func(parent, name string)

// OwnedTempRoot is a private temporary directory retained together with its
// parent identity so cleanup never follows a replaced pathname.
type OwnedTempRoot struct {
	parentPath string
	parent     *os.Root
	root       *os.Root
	tree       *OwnedTree
	name       string
	closed     bool
	outcome    error
}

func NewOwnedTempRoot(prefix string) (*OwnedTempRoot, error) {
	if prefix == "" || filepath.Base(prefix) != prefix || strings.ContainsAny(prefix, `/\\`) {
		return nil, fmt.Errorf("invalid temporary root prefix")
	}
	parent, parentPath, err := OpenVerifiedRoot(os.TempDir())
	if err != nil {
		return nil, err
	}
	var name string
	for {
		name = prefix + rand.Text()
		if err := parent.Mkdir(name, 0700); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return nil, errors.Join(err, parent.Close())
		}
		break
	}
	created, err := parent.Lstat(name)
	if err != nil || created.Mode()&os.ModeSymlink != 0 || !created.IsDir() {
		if err == nil {
			err = fmt.Errorf("created temporary root is not an ordinary directory")
		}
		return nil, errors.Join(err, parent.Close())
	}
	if beforeOwnedTempRootOpenForTest != nil {
		beforeOwnedTempRootOpenForTest(parentPath, name)
	}
	root, err := OpenVerifiedSubroot(parent, name)
	if err == nil {
		opened, statErr := root.Stat(".")
		if statErr != nil || !os.SameFile(created, opened) {
			err = errors.Join(fmt.Errorf("temporary root changed before opening"), statErr)
		}
	}
	if err != nil {
		return nil, errors.Join(err, closeOwnedTempRoot(root), RemoveCreatedEmptyRoot(parent, created), parent.Close())
	}
	t := &OwnedTempRoot{parentPath: parentPath, parent: parent, root: root, tree: NewOwnedTree(root), name: name}
	if err := t.Verify(); err != nil {
		return nil, errors.Join(err, t.Close())
	}
	return t, nil
}

// RemoveCreatedEmptyRoot removes the retained-parent child matching created
// only while its identity and emptiness remain provable.
func RemoveCreatedEmptyRoot(parent *os.Root, created fs.FileInfo) error {
	directory, err := parent.Open(".")
	if err != nil {
		return err
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		return errors.Join(readErr, closeErr)
	}
	for _, name := range names {
		current, err := parent.Lstat(name)
		if err != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(created, current) {
			continue
		}
		root, err := OpenVerifiedSubroot(parent, name)
		if err != nil {
			return err
		}
		opened, statErr := root.Stat(".")
		if statErr != nil || !os.SameFile(created, opened) {
			return errors.Join(fmt.Errorf("%w: created temporary root changed", ErrOwnershipChanged), statErr, root.Close())
		}
		removeErr := RemoveRetainedRoot(parent, name, root)
		rootCloseErr := root.Close()
		return errors.Join(removeErr, rootCloseErr)
	}
	return fmt.Errorf("%w: created temporary root identity is no longer present", ErrOwnershipChanged)
}

func closeOwnedTempRoot(root *os.Root) error {
	if root == nil {
		return nil
	}
	return root.Close()
}

func (t *OwnedTempRoot) Path() string {
	if t == nil {
		return ""
	}
	return filepath.Join(t.parentPath, t.name)
}

func (t *OwnedTempRoot) Tree() *OwnedTree {
	if t == nil || t.closed {
		return nil
	}
	return t.tree
}

func (t *OwnedTempRoot) Verify() error {
	if t == nil || t.closed || t.parent == nil || t.root == nil || t.name == "" {
		return fmt.Errorf("temporary root is closed")
	}
	return errors.Join(
		revalidateRootPath(t.parentPath, t.parent),
		revalidateSubroot(t.parent, t.name, t.root),
	)
}

// CaptureSubprocessOutput adopts the current contents of this private root for
// cleanup. Unlike immutable-tree adoption, symlinks are retained as leaves.
func (t *OwnedTempRoot) CaptureSubprocessOutput() error {
	if err := t.Verify(); err != nil {
		return err
	}
	return t.tree.captureExisting(t.root, "", true)
}

func (t *OwnedTempRoot) Close() error {
	if t == nil {
		return nil
	}
	if t.closed {
		return t.outcome
	}
	cleanupErr := t.Verify()
	if cleanupErr == nil {
		cleanupErr = t.tree.Cleanup(OwnedTreeCleanupHooks{})
	}
	if cleanupErr == nil {
		cleanupErr = RemoveRetainedRoot(t.parent, t.name, t.root)
	}
	rootCloseErr := errors.Join(t.tree.Close(), t.root.Close())
	parentCloseErr := t.parent.Close()
	t.closed = true
	t.root = nil
	t.tree = nil
	t.parent = nil
	t.outcome = errors.Join(cleanupErr, rootCloseErr, parentCloseErr)
	return t.outcome
}
