//go:build windows

package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type suspendedOwnedTreeDirectory struct {
	path        string
	identity    fs.FileInfo
	entry       *ownedTreeDirectory
	parentEntry **os.Root
	closed      bool
}

func suspendOwnedTreeRenameDescendants(tree *OwnedTree) (func() error, error) {
	paths := tree.directoryPaths(false) // Parents precede children for reopening.
	suspended := make([]suspendedOwnedTreeDirectory, 0, len(paths))
	for _, path := range paths {
		entry := tree.dirs[tree.key(path)]
		parentPath := filepath.ToSlash(filepath.Dir(filepath.FromSlash(path)))
		parentEntry := &tree.root
		if parentPath != "." {
			parentDirectory := tree.dirs[tree.key(parentPath)]
			if parentDirectory == nil {
				return nil, fmt.Errorf("%w: owned tree parent is not retained: %s", ErrOwnershipChanged, path)
			}
			parentEntry = &parentDirectory.root
		}
		parent := *parentEntry
		if entry == nil || entry.root == nil || parent == nil || entry.parent != parent {
			return nil, fmt.Errorf("%w: owned tree directory is not retained: %s", ErrOwnershipChanged, path)
		}
		identity, statErr := entry.root.Stat(".")
		if err := errors.Join(revalidateSubroot(parent, entry.name, entry.root), statErr); err != nil {
			return nil, errors.Join(fmt.Errorf("%w: cannot capture owned tree directory: %s", ErrOwnershipChanged, path), err)
		}
		suspended = append(suspended, suspendedOwnedTreeDirectory{
			path:        path,
			identity:    identity,
			entry:       entry,
			parentEntry: parentEntry,
		})
	}

	reopen := func() error {
		var retErr error
		for i := range suspended {
			item := &suspended[i]
			if !item.closed {
				continue
			}
			parent := *item.parentEntry
			if parent == nil {
				retErr = errors.Join(retErr, fmt.Errorf("%w: cannot reopen owned tree directory without retained parent: %s", ErrOwnershipChanged, item.path))
				continue
			}
			root, err := openVerifiedSubroot(parent, item.entry.name)
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("%w: cannot reopen owned tree directory: %s", ErrOwnershipChanged, item.path), err)
				continue
			}
			opened, statErr := root.Stat(".")
			if statErr != nil || !os.SameFile(item.identity, opened) {
				retErr = errors.Join(retErr, fmt.Errorf("%w: reopened owned tree directory changed: %s", ErrOwnershipChanged, item.path), statErr, root.Close())
				continue
			}
			item.entry.parent = parent
			item.entry.root = root
			item.closed = false
			for j := range suspended {
				if suspended[j].parentEntry == &item.entry.root {
					suspended[j].entry.parent = root
				}
			}
		}
		return retErr
	}

	for i := len(suspended) - 1; i >= 0; i-- {
		item := &suspended[i]
		closeErr := item.entry.root.Close()
		item.entry.root = nil
		item.closed = true
		if closeErr != nil {
			return nil, errors.Join(fmt.Errorf("close owned tree directory %s: %w", item.path, closeErr), reopen())
		}
	}
	return reopen, nil
}
