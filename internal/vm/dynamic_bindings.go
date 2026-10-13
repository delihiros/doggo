package vm

import (
	"context"
	"sync"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type bindingContextKey struct{}

// BindingFrame は1レベルの動的束縛を保持します。
// スタック構造で管理され、pushでフレームを積み、popで外します。
type BindingFrame struct {
	bindings map[*atom.Var]types.Value
	parent   *BindingFrame
}

// NewBindingFrame は新しいBindingFrameを作成します。
func NewBindingFrame(bindings map[*atom.Var]types.Value, parent *BindingFrame) *BindingFrame {
	return &BindingFrame{
		bindings: bindings,
		parent:   parent,
	}
}

// bindingState は1つの論理実行に属するbindingフレームスタックです。
// contextの値として保持するため、contextが到達不能になれば状態も回収されます。
type bindingState struct {
	mu             sync.RWMutex
	frame          *BindingFrame
	transientOwner *coll.TransientOwner
	loaderOwner    *Loader
	namespace      string
	loadingLibs    map[*Loader]map[string]struct{}
}

func newBindingState(frame *BindingFrame) *bindingState {
	return &bindingState{frame: frame, transientOwner: coll.NewTransientOwner()}
}

func bindingStateFromContext(ctx context.Context) *bindingState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(bindingContextKey{}).(*bindingState)
	return state
}

// withBindingState は動的束縛を保持できるcontextを返します。
func withBindingState(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if bindingStateFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, bindingContextKey{}, newBindingState(nil))
}

// forkBindingContext は現在の束縛フレームを継承した独立contextを返します。
// フレームは不変なので共有し、以後のpush/popだけを別のstateで管理します。
func forkBindingContext(ctx context.Context) context.Context {
	return forkBindingContextFrom(ctx, ctx)
}

// forkBindingContextFrom はbaseのキャンセルや値を維持しながら、parentの束縛を継承します。
func forkBindingContextFrom(base, parent context.Context) context.Context {
	if base == nil {
		base = context.Background()
	}

	var frame *BindingFrame
	var namespace string
	var loadingLibs map[*Loader]map[string]struct{}
	if state := bindingStateFromContext(parent); state != nil {
		state.mu.RLock()
		frame = state.frame
		namespace = state.namespace
		loadingLibs = cloneLoadingLibraries(state.loadingLibs)
		state.mu.RUnlock()
	}

	state := newBindingState(frame)
	state.namespace = namespace
	state.loadingLibs = loadingLibs
	return context.WithValue(base, bindingContextKey{}, state)
}

func cloneLoadingLibraries(src map[*Loader]map[string]struct{}) map[*Loader]map[string]struct{} {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[*Loader]map[string]struct{}, len(src))
	for loader, libraries := range src {
		entries := make(map[string]struct{}, len(libraries))
		for library := range libraries {
			entries[library] = struct{}{}
		}
		cloned[loader] = entries
	}
	return cloned
}

func namespaceFromContext(ctx context.Context) string {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return ""
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.namespace
}

func setNamespaceInContext(ctx context.Context, namespace string) {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	state.namespace = namespace
	state.mu.Unlock()
}

func contextLoadsLibrary(ctx context.Context, loader *Loader, library string) bool {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return false
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	_, ok := state.loadingLibs[loader][library]
	return ok
}

func contextLoadingLibraries(ctx context.Context, loader *Loader) []string {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return nil
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	libraries := make([]string, 0, len(state.loadingLibs[loader]))
	for library := range state.loadingLibs[loader] {
		libraries = append(libraries, library)
	}
	return libraries
}

func pushContextLoadingLibrary(ctx context.Context, loader *Loader, library string) {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.loadingLibs == nil {
		state.loadingLibs = make(map[*Loader]map[string]struct{})
	}
	if state.loadingLibs[loader] == nil {
		state.loadingLibs[loader] = make(map[string]struct{})
	}
	state.loadingLibs[loader][library] = struct{}{}
}

func popContextLoadingLibrary(ctx context.Context, loader *Loader, library string) {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	delete(state.loadingLibs[loader], library)
	if len(state.loadingLibs[loader]) == 0 {
		delete(state.loadingLibs, loader)
	}
}

func transientOwnerFromContext(ctx context.Context) *coll.TransientOwner {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return nil
	}
	return state.transientOwner
}

// PushDynamicBindings は新しい束縛フレームをスタックにプッシュします。
// bindingsのキーはVarで、値はそのVarの一時的な束縛値です。
func PushDynamicBindings(ctx context.Context, bindings map[*atom.Var]types.Value) {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()

	newFrame := NewBindingFrame(bindings, state.frame)
	state.frame = newFrame
}

// PopDynamicBindings は最新の束縛フレームをスタックからポップします。
func PopDynamicBindings(ctx context.Context) {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.frame != nil {
		state.frame = state.frame.parent
	}
}

// LookupBinding は指定した Var の現在の動的束縛値を検索します。
// 束縛が見つかれば (value, true) を返し、なければ (nil, false) を返します。
func LookupBinding(ctx context.Context, v *atom.Var) (types.Value, bool) {
	state := bindingStateFromContext(ctx)
	if state == nil {
		return nil, false
	}
	state.mu.RLock()
	defer state.mu.RUnlock()

	// フレームスタックを上から順に検索
	for frame := state.frame; frame != nil; frame = frame.parent {
		if val, ok := frame.bindings[v]; ok {
			return val, true
		}
	}

	return nil, false
}

// IsDynamicVar はVarが^:dynamicメタデータを持つかを確認します。
func IsDynamicVar(v *atom.Var) bool {
	if v == nil || v.Metadata == nil {
		return false
	}
	dynamicKey := atom.NewKeyword("dynamic")
	val := v.Metadata.Get(dynamicKey)
	if val == nil {
		return false
	}
	// trueまたはtruthy値をチェック
	if b, ok := val.(atom.Boolean); ok {
		return bool(b)
	}
	return val != nil && val != atom.NilValue
}
