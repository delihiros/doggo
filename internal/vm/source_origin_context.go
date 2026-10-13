package vm

import (
	"context"
	"doggo/internal/vm/engine"
)

type sourceOriginContextKey struct{}

func withSourceOrigin(ctx context.Context, origin engine.SourceOrigin) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	origin.File = origin.FileOrUnknown()
	return context.WithValue(ctx, sourceOriginContextKey{}, origin)
}

func sourceOriginFromContext(ctx context.Context) (engine.SourceOrigin, bool) {
	if ctx == nil {
		return engine.SourceOrigin{}, false
	}
	origin, ok := ctx.Value(sourceOriginContextKey{}).(engine.SourceOrigin)
	if !ok {
		return engine.SourceOrigin{}, false
	}
	origin.File = origin.FileOrUnknown()
	return origin, true
}
