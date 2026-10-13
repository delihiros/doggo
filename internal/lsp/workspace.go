package lsp

import (
	"time"

	toolindex "doggo/internal/tooling/index"
)

type WorkspaceState struct {
	index      *toolindex.WorkspaceIndex
	fileStamps map[string]fileStamp
	dirtyFiles map[string]struct{}
}

type fileStamp struct {
	modTime time.Time
	size    int64
}

func NewWorkspaceState() *WorkspaceState {
	return &WorkspaceState{
		fileStamps: make(map[string]fileStamp),
		dirtyFiles: make(map[string]struct{}),
	}
}

func (ws *WorkspaceState) MarkDirty(path string) {
	if ws == nil || path == "" {
		return
	}
	ws.dirtyFiles[path] = struct{}{}
}

func (ws *WorkspaceState) IsDirty(path string) bool {
	if ws == nil {
		return false
	}
	_, ok := ws.dirtyFiles[path]
	return ok
}
