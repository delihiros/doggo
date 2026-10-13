package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var afterGenerationSourceFileOpenForTest func(string)

type generationFile struct {
	info       fs.FileInfo
	directory  bool
	size       int64
	executable bool
	sha256     string
}

type generationManifest map[string]generationFile

type GenerationSource struct {
	mu      sync.Mutex
	root    *os.Root
	prefix  string
	display string
	files   generationManifest
}

// Files enumerates only regular files in the validated, retained snapshot.
func (s *GenerationSource) Files() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return nil, fmt.Errorf("generation source integrity error: source is closed")
	}
	files := make([]string, 0)
	prefix := s.prefix + "/"
	for name, entry := range s.files {
		if !entry.directory && strings.HasPrefix(name, prefix) {
			files = append(files, strings.TrimPrefix(name, prefix))
		}
	}
	sort.Strings(files)
	return files, nil
}

// FilePath returns an editor location; reads must still use ReadFile.
func (s *GenerationSource) FilePath(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	canonical, err := canonicalGenerationSourcePath(name)
	if err != nil {
		return "", err
	}
	entry, ok := s.files[path.Join(s.prefix, canonical)]
	if !ok || entry.directory {
		return "", fs.ErrNotExist
	}
	if s.root == nil {
		return "", fmt.Errorf("generation source integrity error: source is closed")
	}
	if err := VerifyRootPath(s.root.Name(), s.root); err != nil {
		return "", fmt.Errorf("generation source integrity error for %s: editor path identity changed: %w", canonical, err)
	}
	return filepath.Join(s.root.Name(), filepath.FromSlash(canonical)), nil
}

func (s *GenerationSource) ReadFile(name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	canonical, err := canonicalGenerationSourcePath(name)
	if err != nil {
		return nil, err
	}
	generationPath := path.Join(s.prefix, canonical)
	entry, ok := s.files[generationPath]
	if !ok || entry.directory {
		return nil, fmt.Errorf("%s: %w", generationPath, fs.ErrNotExist)
	}
	if s.root == nil {
		return nil, fmt.Errorf("generation source integrity error for %s: source is closed", generationPath)
	}
	file, err := s.root.Open(filepath.FromSlash(canonical))
	if err != nil {
		return nil, fmt.Errorf("generation source integrity error for %s: %v", generationPath, err)
	}
	opened, statErr := file.Stat()
	if statErr != nil || !sameRegularFile(entry.info, opened) || opened.Size() != entry.size || (opened.Mode()&0111 != 0) != entry.executable {
		_ = file.Close()
		return nil, fmt.Errorf("generation source integrity error for %s: file identity changed", generationPath)
	}
	if afterGenerationSourceFileOpenForTest != nil {
		afterGenerationSourceFileOpenForTest(canonical)
	}
	content, readErr := io.ReadAll(io.LimitReader(file, entry.size+1))
	closeErr := file.Close()
	sum := sha256.Sum256(content)
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("generation source integrity error for %s: %v", generationPath, err)
	}
	if int64(len(content)) != entry.size || hex.EncodeToString(sum[:]) != entry.sha256 {
		return nil, fmt.Errorf("generation source integrity error for %s: content changed", generationPath)
	}
	return content, nil
}

func (s *GenerationSource) DisplayPath(name string) string {
	canonical, err := canonicalGenerationSourcePath(name)
	if err != nil {
		return s.display + "/" + name
	}
	return s.display + "/" + canonical
}

func (s *GenerationSource) close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return nil
	}
	root := s.root
	s.root = nil
	return root.Close()
}

func canonicalGenerationSourcePath(name string) (string, error) {
	if name == "" || strings.Contains(name, `\`) || path.IsAbs(name) || filepath.IsAbs(filepath.FromSlash(name)) {
		return "", fmt.Errorf("invalid generation source path %q", name)
	}
	canonical := path.Clean(name)
	if canonical == "." || canonical == ".." || strings.HasPrefix(canonical, "../") {
		return "", fmt.Errorf("invalid generation source path %q", name)
	}
	return canonical, nil
}
