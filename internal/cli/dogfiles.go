package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type dogFileSearchOptions struct {
	ExcludeDirs                        map[string]struct{}
	MatchFile                          func(path string) bool
	IncludeExplicitFilesRegardlessRule bool
}

func collectDogFiles(paths []string, opts dogFileSearchOptions) ([]string, error) {
	targets := paths
	if len(targets) == 0 {
		targets = []string{"."}
	}

	found := make(map[string]struct{})
	for _, raw := range targets {
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			if filepath.Ext(abs) != ".dog" {
				return nil, fmt.Errorf("not a .dog file: %s", raw)
			}
			if opts.IncludeExplicitFilesRegardlessRule || opts.MatchFile == nil || opts.MatchFile(abs) {
				found[filepath.Clean(abs)] = struct{}{}
			}
			continue
		}
		if err := walkDogFiles(abs, opts, found); err != nil {
			return nil, err
		}
	}

	out := make([]string, 0, len(found))
	for path := range found {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func walkDogFiles(root string, opts dogFileSearchOptions, found map[string]struct{}) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(path, d.Name(), opts.ExcludeDirs) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".dog" {
			return nil
		}
		if opts.MatchFile != nil && !opts.MatchFile(path) {
			return nil
		}
		found[filepath.Clean(path)] = struct{}{}
		return nil
	})
}

func shouldSkipDir(path, name string, excludes map[string]struct{}) bool {
	if excludes == nil {
		return false
	}
	if _, ok := excludes[name]; ok {
		return true
	}
	// ルートディレクトリは除外判定しない。
	if path == "." || path == string(filepath.Separator) {
		return false
	}
	base := filepath.Base(path)
	_, ok := excludes[base]
	return ok
}

func toSlashPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func toDisplayPath(base, path string) string {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return toSlashPath(path)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return toSlashPath(path)
	}
	rel, err := filepath.Rel(baseAbs, pathAbs)
	if err != nil {
		return toSlashPath(pathAbs)
	}
	if rel == "." {
		return "."
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return toSlashPath(pathAbs)
	}
	return toSlashPath(rel)
}
