package lsp

type Session struct {
	docs      *DocumentStore
	workspace *WorkspaceState
}

func NewSession(docs *DocumentStore, workspace *WorkspaceState) *Session {
	return &Session{docs: docs, workspace: workspace}
}
