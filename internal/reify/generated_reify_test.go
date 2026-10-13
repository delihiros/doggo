package reify

import (
	"context"
	"net/http/httptest"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type recordingCallbackInvoker struct {
	ctx context.Context
}

func (e *recordingCallbackInvoker) Invoke(ctx context.Context, _ types.Value, _ []types.Value, _ types.Environment) (types.Value, error) {
	e.ctx = ctx
	return atom.NilValue, nil
}

func TestHTTPHandlerImplUsesRequestContext(t *testing.T) {
	type contextKey struct{}
	requestCtx := context.WithValue(context.Background(), contextKey{}, "request")
	request := httptest.NewRequest("GET", "/", nil).WithContext(requestCtx)
	invoker := &recordingCallbackInvoker{}
	impl := coll.NewEmptyMap().Assoc(atom.NewKeyword("servehttp"), types.NewSymbol("handler"))
	implementation := NewHttpHandlerImpl(invoker, nil, impl)

	implementation.ServeHTTP(httptest.NewRecorder(), request)

	if invoker.ctx == nil || invoker.ctx.Value(contextKey{}) != "request" {
		t.Fatal("expected request context to reach callback invoker")
	}
}

func TestWrapGoValueRejectsMalformedUTF8(t *testing.T) {
	_, err := wrapGoValue(string([]byte{0xff}))
	if err == nil {
		t.Fatal("expected malformed string to return an error")
	}
}
