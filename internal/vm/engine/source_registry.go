package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"doggo/stdlib"
)

type projectRootProvider interface {
	ProjectRoot() string
}

// SourceRegistry keeps source text available for diagnostics and introspection.
type SourceRegistry struct {
	mu           sync.RWMutex
	lines        map[string][]string
	rootProvider projectRootProvider
}

func NewSourceRegistry(rootProvider projectRootProvider) *SourceRegistry {
	return &SourceRegistry{
		lines:        make(map[string][]string),
		rootProvider: rootProvider,
	}
}

func (r *SourceRegistry) Register(file, source string) {
	if r == nil {
		return
	}
	if file == "" {
		file = "<unknown>"
	}
	lines := strings.Split(source, "\n")
	r.mu.Lock()
	r.lines[file] = lines
	r.mu.Unlock()
}

func (r *SourceRegistry) RegisterPrecompiled(code *Program) {
	if r == nil || code == nil || code.Source == "" {
		return
	}
	r.Register(code.SourceFile, code.Source)
}

func (r *SourceRegistry) LookupLine(file string, line int) (string, bool) {
	if r == nil || line <= 0 {
		return "", false
	}
	if file == "" {
		file = "<unknown>"
	}
	r.mu.RLock()
	lines, ok := r.lines[file]
	r.mu.RUnlock()
	if !ok || line-1 >= len(lines) {
		return "", false
	}
	return lines[line-1], true
}

func (r *SourceRegistry) LookupContent(file string) (string, bool) {
	if r == nil || file == "" {
		return "", false
	}

	r.mu.RLock()
	lines, ok := r.lines[file]
	r.mu.RUnlock()
	if ok {
		return strings.Join(lines, "\n"), true
	}

	if strings.HasPrefix(file, "stdlib://") {
		embedPath := strings.TrimPrefix(file, "stdlib://")
		content, err := fs.ReadFile(stdlib.StdlibFS, embedPath)
		if err == nil {
			return string(content), true
		}
		return "", false
	}

	content, err := os.ReadFile(file)
	if err == nil {
		return string(content), true
	}

	if r.rootProvider != nil {
		projectRoot := strings.TrimSpace(r.rootProvider.ProjectRoot())
		if projectRoot != "" && !filepath.IsAbs(file) {
			fallbackPath := filepath.Join(projectRoot, file)
			content, err = os.ReadFile(fallbackPath)
			if err == nil {
				return string(content), true
			}
		}
	}

	return "", false
}
