package project

import (
	"fmt"
	"os"
	"path/filepath"
)

func ResolveRoot(scriptPath string) (string, bool, error) {
	if scriptPath != "" {
		absScript, err := filepath.Abs(scriptPath)
		if err != nil {
			return "", false, fmt.Errorf("resolve script path: %w", err)
		}
		if root, ok, err := findModRoot(filepath.Dir(absScript)); err != nil {
			return "", false, err
		} else if ok {
			return root, true, nil
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("getwd: %w", err)
	}
	if root, ok, err := findModRoot(wd); err != nil {
		return "", false, err
	} else if ok {
		return root, true, nil
	}
	return wd, false, nil
}

func findModRoot(start string) (string, bool, error) {
	absStart, err := filepath.Abs(start)
	if err != nil {
		return "", false, err
	}
	dir := absStart
	for {
		modPath := filepath.Join(dir, ModFileName)
		if st, err := os.Stat(modPath); err == nil && !st.IsDir() {
			return dir, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false, nil
}

func liveProjectLoadPathsRoot(root *os.Root, projectRoot string) ([]string, error) {
	loadPaths := make([]string, 0, 2)
	loadPaths = append(loadPaths, projectRoot)
	if info, err := root.Lstat("src"); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		loadPaths = append(loadPaths, filepath.Join(projectRoot, "src"))
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return loadPaths, nil
}
