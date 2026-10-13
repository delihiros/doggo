package project

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ownedTreeDirectory struct {
	name   string
	parent *os.Root
	root   *os.Root
}

// OwnedTree retains every owned directory and leaf identity below root. Leaves
// are regular files, except that private subprocess adoption may record symlinks.
type OwnedTree struct {
	root     *os.Root
	dirs     map[string]*ownedTreeDirectory
	files    map[string]fs.FileInfo
	prefix   string
	base     *ownedTreeDirectory
	borrowed bool
}

func NewOwnedTree(root *os.Root) *OwnedTree {
	return &OwnedTree{root: root, dirs: make(map[string]*ownedTreeDirectory), files: make(map[string]fs.FileInfo)}
}

func (t *OwnedTree) key(path string) string {
	if t.prefix == "" {
		return path
	}
	return filepath.ToSlash(filepath.Join(t.prefix, path))
}

func (t *OwnedTree) subtree(path string) (*OwnedTree, error) {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return nil, err
	}
	key := t.key(path)
	entry := t.dirs[key]
	if entry == nil || entry.root == nil {
		return nil, fmt.Errorf("owned tree directory is not retained: %s", path)
	}
	if err := revalidateSubroot(entry.parent, entry.name, entry.root); err != nil {
		return nil, err
	}
	return &OwnedTree{root: entry.root, dirs: t.dirs, files: t.files, prefix: key, base: entry, borrowed: true}, nil
}

func cleanOwnedTreePath(path string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || filepath.ToSlash(clean) != path || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("owned tree path is not canonical: %q", path)
	}
	return filepath.ToSlash(clean), nil
}

func (t *OwnedTree) parent(path string) (*os.Root, string, error) {
	clean, err := cleanOwnedTreePath(path)
	if err != nil {
		return nil, "", err
	}
	parentPath := filepath.ToSlash(filepath.Dir(filepath.FromSlash(clean)))
	if parentPath == "." {
		if t == nil || t.root == nil {
			return nil, "", fmt.Errorf("owned tree is closed")
		}
		return t.root, filepath.Base(filepath.FromSlash(clean)), nil
	}
	parent := t.dirs[t.key(parentPath)]
	if parent == nil || parent.root == nil {
		return nil, "", fmt.Errorf("owned tree parent is not retained: %s", parentPath)
	}
	return parent.root, filepath.Base(filepath.FromSlash(clean)), nil
}

// retainRoot consumes a directory handle after matching it to its retained
// parent name. Creation APIs should call CreateRoot instead so the mkdir
// identity is carried directly into this ledger.
func (t *OwnedTree) retainRoot(retained RetainedRoot) error {
	consume := func(err error) error {
		if retained.Root != nil {
			err = errors.Join(err, retained.Root.Close())
		}
		return err
	}
	if t == nil || t.root == nil || retained.Root == nil {
		return consume(fmt.Errorf("owned tree is closed"))
	}
	path, err := cleanOwnedTreePath(retained.Path)
	if err != nil {
		return consume(err)
	}
	key := t.key(path)
	if t.dirs[key] != nil || t.files[key] != nil {
		return consume(fmt.Errorf("owned tree path is already recorded: %s", path))
	}
	parent, name, err := t.parent(path)
	if err != nil {
		return consume(err)
	}
	if err := revalidateSubroot(parent, name, retained.Root); err != nil {
		return consume(err)
	}
	t.dirs[key] = &ownedTreeDirectory{name: name, parent: parent, root: retained.Root}
	retained.Root = nil
	return nil
}

// CreateRoot creates and retains one child, carrying the identity captured
// immediately after mkdir through the open/retain handoff.
func (t *OwnedTree) CreateRoot(path string, perm fs.FileMode, afterCreate func()) (_ *os.Root, retErr error) {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return nil, err
	}
	key := t.key(path)
	if t.dirs[key] != nil || t.files[key] != nil {
		return nil, fmt.Errorf("owned tree path is already recorded: %s", path)
	}
	parent, name, err := t.parent(path)
	if err != nil {
		return nil, err
	}
	if _, err := parent.Lstat(name); err == nil {
		return nil, fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := parent.Mkdir(name, perm); err != nil {
		return nil, err
	}
	created, err := parent.Lstat(name)
	if err != nil || created.Mode()&os.ModeSymlink != 0 || !created.IsDir() {
		if err == nil {
			err = fmt.Errorf("created owned tree entry is not a directory: %s", path)
		}
		if created != nil {
			err = errors.Join(err, RemoveCreatedEmptyRoot(parent, created))
		}
		return nil, err
	}
	if afterCreate != nil {
		afterCreate()
	}
	root, err := openVerifiedSubroot(parent, name)
	if err == nil {
		opened, statErr := root.Stat(".")
		if statErr != nil || !os.SameFile(created, opened) {
			err = errors.Join(fmt.Errorf("created owned tree directory changed: %s", path), statErr)
		}
	}
	if err != nil {
		return nil, errors.Join(err, closeOwnedTreeRoot(root), RemoveCreatedEmptyRoot(parent, created))
	}
	if err := t.retainRoot(RetainedRoot{Path: path, Root: root}); err != nil {
		return nil, err
	}
	clone, err := CloneVerifiedRoot(t.dirs[key].root)
	if err != nil {
		return nil, err
	}
	return clone, nil
}

func closeOwnedTreeRoot(root *os.Root) error {
	if root == nil {
		return nil
	}
	return root.Close()
}

func (t *OwnedTree) CreateAll(path string, perm fs.FileMode) error {
	clean := filepath.Clean(filepath.FromSlash(filepath.ToSlash(path)))
	if clean == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		canonical := filepath.ToSlash(current)
		if t.dirs[t.key(canonical)] != nil {
			continue
		}
		root, err := t.CreateRoot(canonical, perm, nil)
		if err != nil {
			return err
		}
		if err := root.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (t *OwnedTree) recordFile(path string, created fs.FileInfo) error {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return err
	}
	parent, name, err := t.parent(path)
	if err != nil {
		return err
	}
	current, err := parent.Lstat(name)
	if err != nil || !sameOwnedFile(created, current) {
		return errors.Join(fmt.Errorf("%w: owned tree file changed: %s", ErrOwnershipChanged, path), err)
	}
	key := t.key(path)
	delete(t.dirs, key)
	t.files[key] = created
	return nil
}

// RefreshFile records the current exact file identity after an isolated
// subprocess has completed. The subprocess workspace is private and retained;
// transaction-stage writers must use WriteFile or WriteOwnedFile instead.
func (t *OwnedTree) RefreshFile(path string) error {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return err
	}
	if err := rejectOwnedSymlink(path, t.files[t.key(path)]); err != nil {
		return err
	}
	parent, name, err := t.parent(path)
	if err != nil {
		return err
	}
	file, err := parent.Open(name)
	if err != nil {
		return err
	}
	opened, statErr := file.Stat()
	current, currentErr := parent.Lstat(name)
	closeErr := file.Close()
	if statErr != nil || currentErr != nil || !opened.Mode().IsRegular() || !sameRegularFile(opened, current) || closeErr != nil {
		return errors.Join(fmt.Errorf("owned tree subprocess file changed: %s", path), statErr, currentErr, closeErr)
	}
	t.files[t.key(path)] = opened
	return nil
}

func (t *OwnedTree) RefreshOptionalFile(path string) error {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return err
	}
	parent, name, err := t.parent(path)
	if err != nil {
		return err
	}
	if _, err := parent.Lstat(name); errors.Is(err, fs.ErrNotExist) {
		delete(t.files, t.key(path))
		return nil
	} else if err != nil {
		return err
	}
	return t.RefreshFile(path)
}

func (t *OwnedTree) WriteFile(path string, content []byte, mode fs.FileMode) (retErr error) {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return err
	}
	key := t.key(path)
	if t.files[key] != nil || t.dirs[key] != nil {
		return fs.ErrExist
	}
	parent, name, err := t.parent(path)
	if err != nil {
		return err
	}
	if _, err := parent.Lstat(name); err == nil {
		return fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	file, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, file.Close()) }()
	created, err := file.Stat()
	if err != nil {
		return err
	}
	if err := t.recordFile(path, created); err != nil {
		return errors.Join(err, removeRetainedFile(parent, name, created))
	}
	written, err := atomicTempWrite(file, content)
	if err == nil && written != len(content) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return err
	}
	after, statErr := file.Stat()
	current, currentErr := parent.Lstat(name)
	if statErr != nil || currentErr != nil || !sameOwnedFile(created, after) || !sameRegularFile(after, current) {
		return errors.Join(fmt.Errorf("%w: owned tree file changed after write: %s", ErrOwnershipChanged, path), statErr, currentErr)
	}
	t.files[key] = after
	return nil
}

// WriteOwnedFile mutates a recorded file through its exact retained handle.
// An absent file is prepared privately and published with no-replace semantics.
func (t *OwnedTree) WriteOwnedFile(path string, content []byte, mode fs.FileMode) (retErr error) {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return err
	}
	parentRoot, name, err := t.parent(path)
	if err != nil {
		return err
	}
	key := t.key(path)
	if err := rejectOwnedSymlink(path, t.files[key]); err != nil {
		return err
	}
	if expected := t.files[key]; expected != nil {
		return t.writeExistingFile(path, content, mode, parentRoot, name, key, expected)
	}
	if _, err := parentRoot.Lstat(name); err == nil {
		return fmt.Errorf("%w: unrecorded destination exists: %s", ErrOwnershipChanged, path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent, err := parentRoot.Open(".")
	if err != nil {
		return err
	}
	tmp, tmpName, err := createRootTemp(parent, name)
	if err != nil {
		return errors.Join(&AtomicWriteError{Err: err}, parent.Close())
	}
	created, statErr := tmp.Stat()
	if statErr != nil {
		return errors.Join(&AtomicWriteError{Err: statErr}, tmp.Close(), parent.Close())
	}
	renamed := false
	defer func() {
		var cleanupErr error
		if !renamed {
			cleanupErr = removeRetainedFile(parentRoot, tmpName, created)
		}
		closeErr := tmp.Close()
		if closeErr != nil && renamed {
			closeErr = &AtomicWriteError{Renamed: true, Err: closeErr}
		}
		parentCloseErr := parent.Close()
		if parentCloseErr != nil {
			parentCloseErr = &AtomicWriteError{Renamed: renamed, Err: parentCloseErr}
		}
		retErr = errors.Join(retErr, cleanupErr, closeErr, parentCloseErr)
	}()
	if err := prepareAtomicTemp(tmp, content, mode); err != nil {
		return &AtomicWriteError{Err: err}
	}
	publishedInfo, err := tmp.Stat()
	if err != nil {
		return &AtomicWriteError{Err: err}
	}
	renamed, err = publishFileNoReplaceRoot(parent, tmp, tmpName, name)
	if renamed {
		// The exact temporary-file identity crossed the rename commit point.
		// Retain it before reporting any publisher or verification uncertainty.
		t.files[key] = publishedInfo
		current, verifyErr := parentRoot.Lstat(name)
		if verifyErr == nil && !sameRegularFile(publishedInfo, current) {
			verifyErr = fmt.Errorf("%w: atomically written file changed: %s", ErrOwnershipChanged, path)
		}
		err = errors.Join(err, verifyErr)
	}
	if err != nil {
		return &AtomicWriteError{Renamed: renamed, Err: err}
	}
	return nil
}

func (t *OwnedTree) writeExistingFile(path string, content []byte, mode fs.FileMode, parent *os.Root, name, key string, expected fs.FileInfo) (retErr error) {
	file, err := parent.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return &AtomicWriteError{Err: err}
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			retErr = errors.Join(retErr, &AtomicWriteError{Err: closeErr})
		}
	}()
	opened, statErr := file.Stat()
	current, currentErr := parent.Lstat(name)
	if statErr != nil || currentErr != nil || !sameOwnedFile(expected, opened) || !sameRegularFile(opened, current) {
		return &AtomicWriteError{Err: errors.Join(fmt.Errorf("%w: owned tree file changed before write: %s", ErrOwnershipChanged, path), statErr, currentErr)}
	}
	if err := file.Chmod(mode); err != nil {
		return &AtomicWriteError{Err: err}
	}
	if err := file.Truncate(0); err != nil {
		return &AtomicWriteError{Err: err}
	}
	written, err := file.Write(content)
	if err == nil && written != len(content) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return &AtomicWriteError{Err: err}
	}
	if err := atomicTempSync(file); err != nil {
		return &AtomicWriteError{Err: err}
	}
	updated, statErr := file.Stat()
	current, currentErr = parent.Lstat(name)
	if statErr != nil || currentErr != nil || !sameOwnedFile(expected, updated) || !sameRegularFile(updated, current) {
		return &AtomicWriteError{Err: errors.Join(fmt.Errorf("%w: owned tree file changed after write: %s", ErrOwnershipChanged, path), statErr, currentErr)}
	}
	t.files[key] = updated
	return nil
}

// RemoveFile removes only the recorded file identity.
func (t *OwnedTree) RemoveFile(path string) error {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return err
	}
	key := t.key(path)
	expected := t.files[key]
	parent, name, err := t.parent(path)
	if err != nil {
		return err
	}
	if expected == nil {
		if _, err := parent.Lstat(name); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%w: cannot remove unrecorded file: %s", ErrOwnershipChanged, path)
	}
	return finishOwnedRemoval(t.files, key, removeRetainedFile(parent, name, expected))
}

// PublishRoot publishes a standalone owned tree under its retained parent. It
// restores and verifies retained descendants even if publication fails. A
// renamed AtomicWriteError means the caller must retain the published target.
func (t *OwnedTree) PublishRoot(parent *os.Root, oldName, newName string) error {
	if t != nil && t.borrowed {
		return &AtomicWriteError{Err: fmt.Errorf("borrowed tree must be renamed through its owner")}
	}
	if err := t.VerifyComplete(); err != nil {
		return &AtomicWriteError{Err: err}
	}
	reopen, err := suspendOwnedTreeRenameDescendants(t)
	if err != nil {
		return &AtomicWriteError{Err: errors.Join(err, t.VerifyComplete())}
	}
	publishErr := PublishRetainedDirectory(parent, t.root, oldName, newName)
	var atomicErr *AtomicWriteError
	renamed := publishErr == nil || errors.As(publishErr, &atomicErr) && atomicErr.Renamed
	if err := errors.Join(publishErr, reopen(), t.VerifyComplete()); err != nil {
		return &AtomicWriteError{Renamed: renamed, Err: err}
	}
	return nil
}

// RenameRoot carries a retained directory and every descendant ledger entry
// across one descriptor-relative rename.
func (t *OwnedTree) RenameRoot(oldPath, newPath string) (retErr error) {
	oldPath, err := cleanOwnedTreePath(filepath.ToSlash(oldPath))
	if err != nil {
		return err
	}
	newPath, err = cleanOwnedTreePath(filepath.ToSlash(newPath))
	if err != nil {
		return err
	}
	oldKey, newKey := t.key(oldPath), t.key(newPath)
	entry := t.dirs[oldKey]
	if entry == nil || entry.root == nil {
		return fmt.Errorf("owned tree directory is not retained: %s", oldPath)
	}
	oldParent, oldName, err := t.parent(oldPath)
	if err != nil {
		return err
	}
	newParent, newName, err := t.parent(newPath)
	if err != nil {
		return err
	}
	if oldParent != newParent {
		return fmt.Errorf("owned tree rename must stay within one parent")
	}
	if _, err := newParent.Lstat(newName); err == nil {
		return fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := revalidateSubroot(oldParent, oldName, entry.root); err != nil {
		return err
	}
	subtree, err := t.subtree(oldPath)
	if err != nil {
		return &AtomicWriteError{Err: err}
	}
	if err := subtree.VerifyComplete(); err != nil {
		return &AtomicWriteError{Err: err}
	}
	reopen, err := suspendOwnedTreeRenameDescendants(subtree)
	if err != nil {
		return &AtomicWriteError{Err: errors.Join(err, subtree.VerifyComplete())}
	}
	parent, err := oldParent.Open(".")
	if err != nil {
		return &AtomicWriteError{Err: errors.Join(err, reopen(), subtree.VerifyComplete())}
	}
	renamed, publishErr := generationRenameNoReplace(parent, entry.root, oldName, newName)
	if renamed {
		// The directory identity has crossed the rename commit point even when
		// the publisher reports a later durability or handle-close error.
		entry.name = newName
		t.renamePrefix(oldKey, newKey)
		subtree.prefix = newKey
	}
	if !renamed && publishErr == nil {
		publishErr = fmt.Errorf("generation publisher did not rename %s", oldPath)
	}
	err = errors.Join(publishErr, reopen(), subtree.VerifyComplete(), parent.Close())
	if err != nil {
		return &AtomicWriteError{Renamed: renamed, Err: err}
	}
	return nil
}

func (t *OwnedTree) renamePrefix(oldPrefix, newPrefix string) {
	type movedDirectory struct {
		oldPath string
		newPath string
		entry   *ownedTreeDirectory
	}
	var movedDirs []movedDirectory
	for path, entry := range t.dirs {
		newPath := replaceOwnedTreePrefix(path, oldPrefix, newPrefix)
		if newPath != path {
			movedDirs = append(movedDirs, movedDirectory{oldPath: path, newPath: newPath, entry: entry})
		}
	}
	type movedFile struct {
		oldPath string
		newPath string
		info    fs.FileInfo
	}
	var movedFiles []movedFile
	for path, info := range t.files {
		newPath := replaceOwnedTreePrefix(path, oldPrefix, newPrefix)
		if newPath != path {
			movedFiles = append(movedFiles, movedFile{oldPath: path, newPath: newPath, info: info})
		}
	}
	for _, moved := range movedDirs {
		delete(t.dirs, moved.oldPath)
		t.dirs[moved.newPath] = moved.entry
	}
	for _, moved := range movedFiles {
		delete(t.files, moved.oldPath)
		t.files[moved.newPath] = moved.info
	}
}

func replaceOwnedTreePrefix(path, oldPrefix, newPrefix string) string {
	if path == oldPrefix {
		return newPrefix
	}
	if strings.HasPrefix(path, oldPrefix+"/") {
		return newPrefix + strings.TrimPrefix(path, oldPrefix)
	}
	return path
}

// CaptureExisting retains a complete existing tree while every opened entry is
// still matched against its parent. It is for immutable source/adoption trees,
// never for handing off newly-created output after a pathname-only writer.
func (t *OwnedTree) CaptureExisting() error { return t.captureExisting(t.root, "", false) }

func (t *OwnedTree) captureExisting(root *os.Root, prefix string, allowSymlinks bool) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		return errors.Join(readErr, closeErr)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.ToSlash(filepath.Join(prefix, name))
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			if !allowSymlinks {
				return fmt.Errorf("owned tree entry is a symlink: %s", path)
			}
			current, currentErr := root.Lstat(name)
			if currentErr != nil || !sameRetainedEntry(info, current) {
				return errors.Join(fmt.Errorf("%w: owned tree symlink changed while retaining: %s", ErrOwnershipChanged, path), currentErr)
			}
			t.files[t.key(path)] = current
		case info.IsDir():
			child, err := openVerifiedSubroot(root, name)
			if err != nil {
				return err
			}
			if err := t.retainRoot(RetainedRoot{Path: path, Root: child}); err != nil {
				return err
			}
			if afterOwnedTreeSourceOpenForTest != nil {
				afterOwnedTreeSourceOpenForTest(path)
			}
			if err := t.captureExisting(t.dirs[t.key(path)].root, path, allowSymlinks); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			file, err := root.Open(name)
			if err != nil {
				return err
			}
			opened, statErr := file.Stat()
			if afterOwnedTreeSourceOpenForTest != nil {
				afterOwnedTreeSourceOpenForTest(path)
			}
			current, currentErr := root.Lstat(name)
			closeErr := file.Close()
			if statErr != nil || currentErr != nil || !sameRegularFile(info, opened) || !sameRegularFile(opened, current) || closeErr != nil {
				return errors.Join(fmt.Errorf("owned tree file changed while retaining: %s", path), statErr, currentErr, closeErr)
			}
			t.files[t.key(path)] = opened
		default:
			if allowSymlinks {
				return fmt.Errorf("%w: owned tree entry is special: %s", ErrOwnershipChanged, path)
			}
			return fmt.Errorf("owned tree entry is special: %s", path)
		}
	}
	return nil
}

func (t *OwnedTree) verify() error {
	if t == nil || t.root == nil {
		return fmt.Errorf("owned tree is closed")
	}
	if t.base != nil {
		if err := revalidateSubroot(t.base.parent, t.base.name, t.base.root); err != nil {
			return fmt.Errorf("%w: owned tree root changed: %v", ErrOwnershipChanged, err)
		}
	}
	for _, path := range t.directoryPaths(false) {
		entry := t.dirs[t.key(path)]
		if entry.root == nil {
			return fmt.Errorf("owned tree directory is closed: %s", path)
		}
		if err := revalidateSubroot(entry.parent, entry.name, entry.root); err != nil {
			return fmt.Errorf("%w: owned tree directory %s: %v", ErrOwnershipChanged, path, err)
		}
	}
	for _, path := range t.filePaths(false) {
		parent, name, err := t.parent(path)
		if err != nil {
			return err
		}
		current, err := parent.Lstat(name)
		expected := t.files[t.key(path)]
		if err != nil || (!expected.Mode().IsRegular() && expected.Mode()&os.ModeSymlink == 0) || !sameRetainedEntry(expected, current) {
			return errors.Join(fmt.Errorf("%w: owned tree file changed: %s", ErrOwnershipChanged, path), err)
		}
	}
	return nil
}

func (t *OwnedTree) VerifyComplete() error {
	if err := t.verify(); err != nil {
		return err
	}
	parents := append([]string{""}, t.directoryPaths(false)...)
	for _, parentPath := range parents {
		root := t.root
		if parentPath != "" {
			root = t.dirs[t.key(parentPath)].root
		}
		directory, err := root.Open(".")
		if err != nil {
			return err
		}
		names, readErr := directory.Readdirnames(-1)
		closeErr := directory.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		for _, name := range names {
			path := filepath.ToSlash(filepath.Join(parentPath, name))
			key := t.key(path)
			if t.dirs[key] == nil && t.files[key] == nil {
				return fmt.Errorf("%w: owned tree contains unrecorded entry: %s", ErrOwnershipChanged, path)
			}
		}
	}
	return nil
}

func (t *OwnedTree) OpenRoot(path string) (*os.Root, error) {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return nil, err
	}
	if err := t.verify(); err != nil {
		return nil, err
	}
	entry := t.dirs[t.key(path)]
	if entry == nil || entry.root == nil {
		return nil, fmt.Errorf("owned tree directory is not retained: %s", path)
	}
	return CloneVerifiedRoot(entry.root)
}

// CopyTo copies only exact retained directory/file identities. No registered
// directory is reopened from its parent name.
func (t *OwnedTree) CopyTo(destination *OwnedTree, skip func(path string, info fs.FileInfo) bool) error {
	if err := t.VerifyComplete(); err != nil {
		return err
	}
	for _, path := range t.filePaths(false) {
		if t.files[t.key(path)].Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("owned tree entry is a symlink and cannot be copied: %s", path)
		}
	}
	for _, path := range t.directoryPaths(false) {
		entry := t.dirs[t.key(path)]
		info, err := entry.root.Stat(".")
		if err != nil {
			return err
		}
		if skip != nil && skip(path, info) {
			continue
		}
		if parentSkipped(path, skip, t) {
			continue
		}
		root, err := destination.CreateRoot(path, info.Mode().Perm(), nil)
		if err != nil {
			return err
		}
		if err := root.Close(); err != nil {
			return err
		}
	}
	for _, path := range t.filePaths(false) {
		if parentSkipped(path, skip, t) {
			continue
		}
		info := t.files[t.key(path)]
		if skip != nil && skip(path, info) {
			continue
		}
		content, err := t.readFile(path, info)
		if err != nil {
			return err
		}
		if err := destination.WriteFile(path, content, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return errors.Join(t.VerifyComplete(), destination.VerifyComplete())
}

func parentSkipped(path string, skip func(string, fs.FileInfo) bool, t *OwnedTree) bool {
	if skip == nil {
		return false
	}
	for parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(path))); parent != "."; parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(parent))) {
		entry := t.dirs[t.key(parent)]
		if entry != nil {
			info, err := entry.root.Stat(".")
			if err == nil && skip(parent, info) {
				return true
			}
		}
	}
	return false
}

func (t *OwnedTree) readFile(path string, expected fs.FileInfo) ([]byte, error) {
	parent, name, err := t.parent(path)
	if err != nil {
		return nil, err
	}
	file, err := parent.Open(name)
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	current, currentErr := parent.Lstat(name)
	if statErr != nil || currentErr != nil || !sameOwnedFile(expected, opened) || !sameRegularFile(opened, current) {
		return nil, errors.Join(fmt.Errorf("%w: owned tree file changed while opening: %s", ErrOwnershipChanged, path), statErr, currentErr, file.Close())
	}
	if afterOwnedTreeSourceOpenForTest != nil {
		afterOwnedTreeSourceOpenForTest(path)
	}
	content, readErr := io.ReadAll(file)
	after, afterErr := parent.Lstat(name)
	return content, errors.Join(readErr, file.Close(), func() error {
		if afterErr != nil || !sameRegularFile(opened, after) {
			return errors.Join(fmt.Errorf("%w: owned tree file changed while reading: %s", ErrOwnershipChanged, path), afterErr)
		}
		return nil
	}())
}

func (t *OwnedTree) ReadFile(path string) ([]byte, error) {
	path, err := cleanOwnedTreePath(filepath.ToSlash(path))
	if err != nil {
		return nil, err
	}
	expected := t.files[t.key(path)]
	if expected == nil {
		return nil, fmt.Errorf("owned tree file is not recorded: %s", path)
	}
	if err := rejectOwnedSymlink(path, expected); err != nil {
		return nil, err
	}
	return t.readFile(path, expected)
}

func rejectOwnedSymlink(path string, info fs.FileInfo) error {
	if info != nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("owned tree symlink is cleanup-only: %s", path)
	}
	return nil
}

type OwnedTreeCleanupHooks struct {
	BeforeRemove func(string)
}

// Cleanup removes only the recorded identities. It first proves that the tree
// contains no unrecorded entries, then rechecks each entry immediately before
// descriptor-relative removal.
func (t *OwnedTree) Cleanup(hooks OwnedTreeCleanupHooks) error {
	if err := t.VerifyComplete(); err != nil {
		return err
	}
	return t.cleanupPaths(t.filePaths(true), t.directoryPaths(true), hooks)
}

// RemoveSubtree removes one fully-recorded subtree while retaining the rest of
// the transaction ledger.
func (t *OwnedTree) RemoveSubtree(prefix string) error {
	prefix, err := cleanOwnedTreePath(filepath.ToSlash(prefix))
	if err != nil {
		return err
	}
	if t.dirs[t.key(prefix)] == nil {
		return fmt.Errorf("owned tree directory is not retained: %s", prefix)
	}
	if err := t.VerifyComplete(); err != nil {
		return err
	}
	var files, dirs []string
	for _, path := range t.filePaths(true) {
		if strings.HasPrefix(path, prefix+"/") {
			files = append(files, path)
		}
	}
	for _, path := range t.directoryPaths(true) {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			dirs = append(dirs, path)
		}
	}
	return t.cleanupPaths(files, dirs, OwnedTreeCleanupHooks{})
}

func (t *OwnedTree) cleanupPaths(files, dirs []string, hooks OwnedTreeCleanupHooks) error {
	for _, path := range files {
		parent, name, err := t.parent(path)
		if err != nil {
			return err
		}
		key := t.key(path)
		expected := t.files[key]
		if hooks.BeforeRemove != nil {
			hooks.BeforeRemove(path)
		}
		if err := finishOwnedRemoval(t.files, key, removeRetainedFile(parent, name, expected)); err != nil {
			return err
		}
	}
	for _, path := range dirs {
		key := t.key(path)
		entry := t.dirs[key]
		if entry == nil || entry.root == nil {
			return fmt.Errorf("owned tree directory is closed: %s", path)
		}
		if hooks.BeforeRemove != nil {
			hooks.BeforeRemove(path)
		}
		removeErr := RemoveRetainedRoot(entry.parent, entry.name, entry.root)
		closeErr := entry.root.Close()
		entry.root = nil
		removeErr = finishOwnedRemoval(t.dirs, key, removeErr)
		if err := errors.Join(removeErr, closeErr); err != nil {
			return err
		}
	}
	return nil
}

func (t *OwnedTree) directoryPaths(reverse bool) []string {
	paths := make([]string, 0, len(t.dirs))
	for path := range t.dirs {
		if t.prefix == "" {
			paths = append(paths, path)
			continue
		}
		if strings.HasPrefix(path, t.prefix+"/") {
			paths = append(paths, strings.TrimPrefix(path, t.prefix+"/"))
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		di, dj := strings.Count(paths[i], "/"), strings.Count(paths[j], "/")
		if di == dj {
			if reverse {
				return paths[i] > paths[j]
			}
			return paths[i] < paths[j]
		}
		if reverse {
			return di > dj
		}
		return di < dj
	})
	return paths
}

func (t *OwnedTree) filePaths(reverse bool) []string {
	paths := make([]string, 0, len(t.files))
	for path := range t.files {
		if t.prefix == "" {
			paths = append(paths, path)
			continue
		}
		if strings.HasPrefix(path, t.prefix+"/") {
			paths = append(paths, strings.TrimPrefix(path, t.prefix+"/"))
		}
	}
	sort.Strings(paths)
	if reverse {
		sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	}
	return paths
}

func (t *OwnedTree) Close() error {
	if t == nil {
		return nil
	}
	if t.borrowed {
		t.root = nil
		t.base = nil
		return nil
	}
	var err error
	for _, path := range t.directoryPaths(true) {
		key := t.key(path)
		if root := t.dirs[key].root; root != nil {
			err = errors.Join(err, root.Close())
			t.dirs[key].root = nil
		}
	}
	return err
}

func sameOwnedFile(before, after fs.FileInfo) bool {
	return before != nil && after != nil && before.Mode().IsRegular() && after.Mode().IsRegular() && os.SameFile(before, after)
}
