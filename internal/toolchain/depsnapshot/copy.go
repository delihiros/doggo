package depsnapshot

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"doggo/internal/project"
)

type copyOverride struct {
	content []byte
	mode    os.FileMode
}

func validateSourceTree(root *os.Root) error {
	return walkRootTree(root, func(path string, info fs.FileInfo) (bool, error) {
		if info.Mode()&os.ModeType != 0 && !info.IsDir() {
			return false, fmt.Errorf("unsupported filesystem entry %s (%s)", path, info.Mode().Type())
		}
		name := filepath.Base(path)
		if name == ".git" || name == ".doggo" {
			return false, nil
		}
		return true, nil
	})
}

func walkRootTree(root *os.Root, visit func(string, fs.FileInfo) (bool, error)) error {
	var walk func(string) error
	walk = func(directory string) error {
		names, err := readRootNames(root, directory)
		if err != nil {
			return err
		}
		for _, name := range names {
			path := name
			if directory != "." {
				path = filepath.Join(directory, name)
			}
			info, err := root.Lstat(path)
			if err != nil {
				return err
			}
			descend, err := visit(path, info)
			if err != nil {
				return err
			}
			if descend && info.IsDir() {
				if err := walk(path); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(".")
}

func readRootNames(root *os.Root, directory string) ([]string, error) {
	opened, err := root.Open(directory)
	if err != nil {
		return nil, err
	}
	names, readErr := opened.Readdirnames(-1)
	closeErr := opened.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sort.Strings(names)
	return names, nil
}

func copyCanonicalTreeToOwnedTree(source, destination *os.Root, tree *project.OwnedTree, prefix string, overrides map[string]copyOverride) (string, error) {
	if err := validateSourceTree(source); err != nil {
		return "", err
	}
	if err := chmodRootPath(destination, ".", 0755); err != nil {
		return "", err
	}
	writtenOverrides := make(map[string]bool, len(overrides))
	err := walkRootTree(source, func(path string, info fs.FileInfo) (bool, error) {
		if info.Mode()&os.ModeType != 0 && !info.IsDir() {
			return false, fmt.Errorf("unsupported filesystem entry %s (%s)", path, info.Mode().Type())
		}
		name := filepath.Base(path)
		if name == ".git" || name == ".doggo" {
			return false, nil
		}
		target := filepath.ToSlash(filepath.Join(prefix, path))
		if info.IsDir() {
			return true, tree.CreateAll(target, 0755)
		}
		override, ok := overrides[path]
		var content []byte
		mode := os.FileMode(0644)
		if !ok {
			var readErr error
			content, _, readErr = readRootRegularFile(source, path, info)
			if readErr != nil {
				return false, readErr
			}
			if info.Mode()&0111 != 0 {
				mode = 0755
			}
		} else {
			content = override.content
			mode = override.mode
			writtenOverrides[path] = true
		}
		return false, tree.WriteFile(target, content, mode)
	})
	if err != nil {
		return "", err
	}
	for name, override := range overrides {
		if !writtenOverrides[name] {
			target := filepath.ToSlash(filepath.Join(prefix, name))
			parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(target)))
			if err := tree.CreateAll(parent, 0755); err != nil {
				return "", err
			}
			if err := tree.WriteFile(target, override.content, override.mode); err != nil {
				return "", err
			}
		}
	}
	return project.CanonicalRootDigest(destination)
}

func readRootRegularFile(root *os.Root, name string, before fs.FileInfo) ([]byte, fs.FileInfo, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	after, err := root.Lstat(name)
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		file.Close()
		return nil, nil, fmt.Errorf("regular file changed while opening: %s", name)
	}
	content, err := io.ReadAll(file)
	closeErr := file.Close()
	if err != nil {
		return nil, nil, err
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	return content, opened, nil
}
