package project

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type AtomicWriteError struct {
	Renamed bool
	Err     error
}

var (
	atomicTempWrite  = func(file *os.File, content []byte) (int, error) { return file.Write(content) }
	atomicTempSync   = func(file *os.File) error { return file.Sync() }
	atomicRootRename = renameAtomicRootStep
	atomicParentSync = syncAtomicParent
)

func (e *AtomicWriteError) Error() string {
	return fmt.Sprintf("atomic write failed: %v", e.Err)
}

func (e *AtomicWriteError) Unwrap() error { return e.Err }

func WriteFileAtomic(path string, content []byte, mode fs.FileMode) (retErr error) {
	if path == "" || os.IsPathSeparator(path[len(path)-1]) {
		return &AtomicWriteError{Err: fmt.Errorf("atomic target must be an ordinary file path")}
	}
	parent, _, err := OpenVerifiedRoot(filepath.Dir(path))
	if err != nil {
		return &AtomicWriteError{Err: err}
	}
	renamed := false
	defer func() { retErr = joinAtomicClose(retErr, parent.Close(), renamed) }()
	retErr = WriteFileAtomicRoot(parent, filepath.Base(path), content, mode)
	var atomicErr *AtomicWriteError
	renamed = retErr == nil || errors.As(retErr, &atomicErr) && atomicErr.Renamed
	return retErr
}

func joinAtomicClose(operationErr, closeErr error, renamed bool) error {
	if closeErr == nil {
		return operationErr
	}
	return errors.Join(operationErr, &AtomicWriteError{Renamed: renamed, Err: closeErr})
}

// WriteFileAtomicRoot atomically writes path through an already-open root, so
// later path replacement cannot redirect the write outside that root.
func WriteFileAtomicRoot(root *os.Root, path string, content []byte, mode fs.FileMode) (retErr error) {
	if root == nil {
		return &AtomicWriteError{Err: fmt.Errorf("root is nil")}
	}
	clean, err := validateRootPath(root, path)
	if err != nil {
		return &AtomicWriteError{Err: err}
	}
	parentRoot, err := openVerifiedSubroot(root, filepath.Dir(clean))
	if err != nil {
		return &AtomicWriteError{Err: err}
	}
	parent, err := parentRoot.Open(".")
	if err != nil {
		return errors.Join(&AtomicWriteError{Err: err}, parentRoot.Close())
	}
	if err := VerifyRootHandle(parentRoot, parent); err != nil {
		return errors.Join(&AtomicWriteError{Err: err}, parent.Close(), parentRoot.Close())
	}
	renamed := false
	defer func() {
		if closeErr := errors.Join(parent.Close(), parentRoot.Close()); closeErr != nil {
			retErr = errors.Join(retErr, &AtomicWriteError{Renamed: renamed, Err: closeErr})
		}
	}()
	tmp, tmpName, err := createRootTemp(parent, filepath.Base(clean))
	if err != nil {
		return &AtomicWriteError{Err: err}
	}
	created, statErr := tmp.Stat()
	if statErr != nil {
		return errors.Join(&AtomicWriteError{Err: statErr}, tmp.Close())
	}
	defer func() {
		var cleanupErr error
		if !renamed {
			cleanupErr = removeRetainedFile(parentRoot, tmpName, created)
		}
		closeErr := tmp.Close()
		if closeErr != nil && renamed {
			closeErr = &AtomicWriteError{Renamed: true, Err: closeErr}
		}
		retErr = errors.Join(retErr, cleanupErr, closeErr)
	}()

	if err := prepareAtomicTemp(tmp, content, mode); err != nil {
		return &AtomicWriteError{Err: err}
	}
	publishedInfo, err := tmp.Stat()
	if err != nil {
		return &AtomicWriteError{Err: err}
	}
	renamed, err = replaceFileAtomicRoot(parent, tmp, tmpName, filepath.Base(clean))
	if err != nil {
		return &AtomicWriteError{Renamed: renamed, Err: err}
	}
	current, err := parentRoot.Lstat(filepath.Base(clean))
	if err != nil || !sameRegularFile(publishedInfo, current) {
		return &AtomicWriteError{Renamed: true, Err: errors.Join(fmt.Errorf("%w: atomically written file changed", ErrOwnershipChanged), err)}
	}
	return nil
}

func prepareAtomicTemp(tmp *os.File, content []byte, mode fs.FileMode) error {
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if n, err := atomicTempWrite(tmp, content); err != nil {
		return err
	} else if n != len(content) {
		return io.ErrShortWrite
	}
	if err := atomicTempSync(tmp); err != nil {
		return err
	}
	return nil
}

func createRootTemp(parent *os.File, base string) (*os.File, string, error) {
	for {
		tmpName := "." + base + ".tmp-" + rand.Text()
		tmp, err := createFileAtomicAt(parent, tmpName)
		if err == nil {
			return tmp, tmpName, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", err
		}
	}
}

func publishFileNoReplaceRoot(parent, temp *os.File, oldName, newName string) (bool, error) {
	if err := renameAtomicRootNoReplaceStep(parent, temp, oldName, newName); err != nil {
		return false, err
	}
	if err := atomicParentSync(parent); err != nil {
		return true, err
	}
	return true, nil
}

func validateRootPath(root *os.Root, path string) (string, error) {
	clean := filepath.Clean(path)
	if filepath.IsAbs(path) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("candidate path is outside root")
	}
	parent := filepath.Dir(clean)
	current := "."
	if parent == "." {
		return clean, nil
	}
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("candidate path has symlink parent %s", current)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("candidate path parent %s is not a directory", current)
		}
	}
	return clean, nil
}

func ValidateContainedPath(root, candidate string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("candidate path is outside root")
	}

	parentRel, err := filepath.Rel(rootAbs, filepath.Dir(candidateAbs))
	if err != nil {
		return err
	}
	current := rootAbs
	parts := []string{"."}
	if parentRel != "." {
		parts = append(parts, strings.Split(parentRel, string(filepath.Separator))...)
	}
	for _, part := range parts {
		if part != "." {
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("candidate path has symlink parent %s", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("candidate path parent %s is not a directory", current)
		}
	}
	return nil
}
