package depsnapshot

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// canonicalRetainedPath consults directory metadata only. Missing live paths
// do not affect the copied snapshot; symlinks and changed metadata cannot
// establish an alias for a context proved by the old snapshot graph.
func canonicalRetainedPath(name string) (string, fs.FileInfo, bool, error) {
	before, err := retainedPathInfo(name)
	if os.IsNotExist(err) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	canonical, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", nil, false, fmt.Errorf("canonicalize retained path %q: %w", name, err)
	}
	after, err := retainedPathInfo(name)
	if err != nil {
		return "", nil, false, fmt.Errorf("retained path changed during canonicalization: %w", err)
	}
	canonicalInfo, err := os.Stat(canonical)
	if err != nil || !os.SameFile(before, after) || !os.SameFile(before, canonicalInfo) {
		return "", nil, false, fmt.Errorf("retained path %q changed during canonicalization: %v", name, err)
	}
	return filepath.Clean(canonical), before, true, nil
}

func retainedPathInfo(name string) (fs.FileInfo, error) {
	anchor := filepath.VolumeName(name) + string(filepath.Separator)
	relative, err := filepath.Rel(anchor, name)
	if err != nil {
		return nil, err
	}
	current := anchor
	var info fs.FileInfo
	for _, component := range append([]string{"."}, strings.Split(relative, string(filepath.Separator))...) {
		current = filepath.Join(current, component)
		info, err = os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("retained path contains symlink component: %s", current)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("retained path contains non-directory component: %s", current)
		}
	}
	return info, nil
}
