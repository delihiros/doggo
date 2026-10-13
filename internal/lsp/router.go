package lsp

type Router struct {
	session *Session
}

func NewRouter(session *Session) *Router {
	return &Router{session: session}
}

func (r *Router) HandleRequest(s *Server, msg message) error {
	if !s.initialized && msg.Method != lspMethodInitialize {
		return s.respondError(msg.ID, lspErrorServerNotInitialized, "server is not initialized")
	}
	switch msg.Method {
	case lspMethodInitialize:
		return s.handleInitialize(msg)
	case lspMethodShutdown:
		return s.respond(msg.ID, map[string]any{})
	case lspMethodCompletion, lspMethodDefinition, lspMethodReferences, lspMethodRename,
		lspMethodFormatting, lspMethodHover, lspMethodDocumentSymbol, lspMethodWorkspaceSymbol,
		lspMethodSemanticTokensFull, lspMethodCodeAction:
		switch msg.Method {
		case lspMethodCompletion:
			return s.handleCompletion(msg)
		case lspMethodDefinition:
			return s.handleDefinition(msg)
		case lspMethodReferences:
			return s.handleReferences(msg)
		case lspMethodRename:
			return s.handleRename(msg)
		case lspMethodFormatting:
			return s.handleFormatting(msg)
		case lspMethodHover:
			return s.handleHover(msg)
		case lspMethodDocumentSymbol:
			return s.handleDocumentSymbol(msg)
		case lspMethodWorkspaceSymbol:
			return s.handleWorkspaceSymbol(msg)
		case lspMethodSemanticTokensFull:
			return s.handleSemanticTokensFull(msg)
		case lspMethodCodeAction:
			return s.handleCodeAction(msg)
		}
	default:
		return s.respondError(msg.ID, lspErrorMethodNotFound, "method not found")
	}
	return s.respondError(msg.ID, lspErrorMethodNotFound, "method not found")
}
