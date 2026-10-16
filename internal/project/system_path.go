package project

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// NormalizeSystemPath recognizes only Darwin's standard /var system alias.
// User-controlled suffix components are left intact for retained path checks.
func NormalizeSystemPath(candidate string) (string, error) {
	if runtime.GOOS != "darwin" {
		return candidate, nil
	}
	return normalizeSystemAlias(candidate, "/var", "/private/var")
}

func normalizeSystemAlias(candidate, alias, target string) (string, error) {
	if candidate != alias && !strings.HasPrefix(candidate, alias+string(filepath.Separator)) {
		return candidate, nil
	}
	before, err := os.Lstat(alias)
	if err != nil {
		return "", err
	}
	if before.IsDir() && before.Mode()&os.ModeSymlink == 0 {
		return candidate, nil
	}
	link, err := os.Readlink(alias)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(alias), link)
	}
	if filepath.Clean(link) != target {
		return "", fmt.Errorf("system alias %s has an unexpected target", alias)
	}
	for _, directory := range []string{filepath.Dir(target), target} {
		info, err := os.Lstat(directory)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("system alias target %s is not an ordinary directory", directory)
		}
	}
	resolved, err := os.Stat(alias)
	if err != nil {
		return "", err
	}
	expected, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	after, err := os.Lstat(alias)
	if err != nil {
		return "", err
	}
	if !os.SameFile(before, after) || !os.SameFile(resolved, expected) {
		return "", fmt.Errorf("%w: system alias %s changed", ErrOwnershipChanged, alias)
	}
	return target + strings.TrimPrefix(candidate, alias), nil
}
