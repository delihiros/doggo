package project

import (
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type retainedDirectoryComponent struct {
	parent   *os.Root
	name     string
	root     *os.Root
	identity fs.FileInfo
	created  fs.FileInfo
}

// RetainedDirectory holds every directory component below a caller-owned root.
// A non-zero createMode creates missing components through their retained parent.
type RetainedDirectory struct {
	base       *os.Root
	components []retainedDirectoryComponent
	closed     bool
}

var afterRetainedDirectoryPrivateIdentityForTest func(name string)

func OpenRetainedDirectory(base *os.Root, path string, createMode fs.FileMode, forbidden ...fs.FileInfo) (*RetainedDirectory, error) {
	if base == nil {
		return nil, fmt.Errorf("retained directory base is nil")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.ToSlash(clean) != path {
		return nil, fmt.Errorf("retained directory path is not canonical: %q", path)
	}
	directory := &RetainedDirectory{base: base}
	if clean == "." {
		return directory, nil
	}
	parent := base
	for _, name := range strings.Split(clean, string(filepath.Separator)) {
		identity, err := parent.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) && createMode != 0 {
			component, committed, createErr := createRetainedDirectoryComponent(parent, name, createMode, forbidden)
			if committed {
				directory.components = append(directory.components, component)
				parent = component.root
			}
			if createErr != nil {
				return nil, errors.Join(createErr, directory.cleanup(true))
			}
			continue
		}
		if err != nil || identity.Mode()&os.ModeSymlink != 0 || !identity.IsDir() {
			if err == nil {
				err = fmt.Errorf("directory component %s is not ordinary", name)
			}
			return nil, errors.Join(err, directory.cleanup(true))
		}
		if sameAsAny(identity, forbidden) {
			return nil, errors.Join(fmt.Errorf("%w: directory component %s has forbidden identity", ErrOwnershipChanged, name), directory.cleanup(true))
		}
		child, err := OpenVerifiedSubroot(parent, name)
		if err == nil {
			opened, statErr := child.Stat(".")
			if statErr != nil || !os.SameFile(identity, opened) {
				err = errors.Join(fmt.Errorf("%w: directory component %s changed while opening", ErrOwnershipChanged, name), statErr)
			}
		}
		if err != nil {
			return nil, errors.Join(err, closeRetainedDirectoryRoot(child), directory.cleanup(true))
		}
		directory.components = append(directory.components, retainedDirectoryComponent{
			parent: parent, name: name, root: child, identity: identity,
		})
		parent = child
	}
	return directory, nil
}

func createRetainedDirectoryComponent(parent *os.Root, name string, mode fs.FileMode, forbidden []fs.FileInfo) (_ retainedDirectoryComponent, committed bool, retErr error) {
	privateName := ""
	for {
		privateName = ".doggo-directory-" + cryptorand.Text()
		if err := parent.Mkdir(privateName, mode); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return retainedDirectoryComponent{}, false, err
		}
		break
	}
	created, err := parent.Lstat(privateName)
	if err != nil || created.Mode()&os.ModeSymlink != 0 || !created.IsDir() {
		if err == nil {
			err = fmt.Errorf("private directory component %s is not ordinary", privateName)
		}
		return retainedDirectoryComponent{}, false, errors.Join(err, removeCreatedDirectory(parent, created))
	}
	if sameAsAny(created, forbidden) {
		return retainedDirectoryComponent{}, false, errors.Join(fmt.Errorf("%w: private directory component has forbidden identity", ErrOwnershipChanged), removeCreatedDirectory(parent, created))
	}
	if afterRetainedDirectoryPrivateIdentityForTest != nil {
		afterRetainedDirectoryPrivateIdentityForTest(privateName)
	}
	privateRoot, err := OpenVerifiedSubroot(parent, privateName)
	if err != nil {
		return retainedDirectoryComponent{}, false, errors.Join(err, removeCreatedDirectory(parent, created))
	}
	opened, statErr := privateRoot.Stat(".")
	if statErr != nil || !os.SameFile(created, opened) {
		return retainedDirectoryComponent{}, false, errors.Join(fmt.Errorf("%w: private directory component changed while opening", ErrOwnershipChanged), statErr, privateRoot.Close(), removeCreatedDirectory(parent, created))
	}
	publishErr := PublishRetainedDirectory(parent, privateRoot, privateName, name)
	var atomicErr *AtomicWriteError
	renamed := publishErr == nil || errors.As(publishErr, &atomicErr) && atomicErr.Renamed
	if renamed {
		component := retainedDirectoryComponent{parent: parent, name: name, root: privateRoot, identity: created, created: created}
		return component, true, publishErr
	}
	return retainedDirectoryComponent{}, false, errors.Join(
		publishErr,
		privateRoot.Close(),
		removeCreatedDirectory(parent, created),
	)
}

func (d *RetainedDirectory) Root() *os.Root {
	if d == nil || d.closed {
		return nil
	}
	if len(d.components) == 0 {
		return d.base
	}
	return d.components[len(d.components)-1].root
}

func (d *RetainedDirectory) Identity() fs.FileInfo {
	if d == nil || d.closed || len(d.components) == 0 {
		return nil
	}
	return d.components[len(d.components)-1].identity
}

func (d *RetainedDirectory) LeafCreated() bool {
	return d != nil && !d.closed && len(d.components) > 0 && d.components[len(d.components)-1].created != nil
}

func (d *RetainedDirectory) Verify() error {
	if d == nil {
		return fmt.Errorf("retained directory is nil")
	}
	if d.closed {
		return fmt.Errorf("retained directory is closed")
	}
	for _, component := range d.components {
		opened, statErr := component.root.Stat(".")
		current, lstatErr := component.parent.Lstat(component.name)
		if statErr != nil || lstatErr != nil || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(component.identity, opened) || !os.SameFile(opened, current) {
			return errors.Join(fmt.Errorf("%w: directory component %s changed", ErrOwnershipChanged, component.name), statErr, lstatErr)
		}
	}
	return nil
}

func (d *RetainedDirectory) Close() error {
	if d == nil || d.closed {
		return nil
	}
	d.closed = true
	return d.cleanup(false)
}

func (d *RetainedDirectory) cleanup(removeCreated bool) error {
	var err error
	for i := len(d.components) - 1; i >= 0; i-- {
		component := d.components[i]
		err = errors.Join(err, component.root.Close())
		if removeCreated && component.created != nil {
			err = errors.Join(err, RemoveCreatedEmptyRoot(component.parent, component.created))
		}
	}
	d.components = nil
	return err
}

func sameAsAny(identity fs.FileInfo, candidates []fs.FileInfo) bool {
	for _, candidate := range candidates {
		if candidate != nil && os.SameFile(identity, candidate) {
			return true
		}
	}
	return false
}

func removeCreatedDirectory(parent *os.Root, created fs.FileInfo) error {
	if created == nil {
		return nil
	}
	return RemoveCreatedEmptyRoot(parent, created)
}

func closeRetainedDirectoryRoot(root *os.Root) error {
	if root == nil {
		return nil
	}
	return root.Close()
}
