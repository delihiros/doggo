package vm

import (
	"context"
	"fmt"
	"strings"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

// vmContextKey はコンテキスト内のVM参照のためのキー型
type vmContextKey struct{}

// WithVM はコンテキストにVMインスタンスを追加します
func WithVM(ctx context.Context, vm *VM) context.Context {
	return context.WithValue(ctx, vmContextKey{}, vm)
}

// GetVM はコンテキストからVMインスタンスを取得します
func GetVM(ctx context.Context) *VM {
	if vm, ok := ctx.Value(vmContextKey{}).(*VM); ok {
		return vm
	}
	return nil
}

func bindVMContext(ctx context.Context, vm *VM) context.Context {
	ctx = withBindingState(ctx)
	if GetVM(ctx) == vm {
		return ctx
	}
	return WithVM(ctx, vm)
}

func applyWithVM(ctx context.Context, vm *VM, fn types.Value, args []types.Value) (types.Value, error) {
	return vm.Apply(ctx, fn, args)
}

// Builtin は組み込み関数を表します。
// Fn または CtxFn のいずれかを設定します。
type Builtin struct {
	Name          string // 関数名
	ErrorFunction string // 公開エラーで報告する完全修飾関数名
	Fn            func(args ...types.Value) types.Value
	Fn2           func(a, b types.Value) types.Value // Phase 10k: 2引数専用（variadic アロケーション回避）
	CtxFn         func(ctx context.Context, args ...types.Value) (types.Value, error)
}

// Invoke は types.Fn インターフェースを実装し、コンテキスト付きで関数を呼び出します。
func (b *Builtin) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	var result types.Value
	var err error
	if b.CtxFn != nil {
		result, err = b.CtxFn(ctx, args...)
	} else {
		result = b.Fn(args...)
	}
	if err != nil {
		return nil, b.adaptBuiltinCallError(err)
	}
	if result == nil {
		return atom.NilValue, nil
	}
	// エラーチェック - 構造化エラーに変換
	if errVal, ok := result.(*Error); ok {
		if errVal.Kind == "argument" || errVal.Kind == "type" {
			return nil, invalidArgumentBuiltinError(b.errorFunction(), errVal)
		}
		if errVal.Kind == "runtime" {
			return nil, runtimeBuiltinError(b.errorFunction(), errVal.Message)
		}
		return nil, vmErrorToDoggoError(errVal)
	}
	if err := coll.CollectionError(result); err != nil {
		return nil, err
	}
	return result, nil
}

func (b *Builtin) adaptBuiltinCallError(err error) error {
	if err == nil {
		return nil
	}
	if builtinErr, ok := err.(*Error); ok {
		if builtinErr.Kind == "argument" || builtinErr.Kind == "type" {
			return invalidArgumentBuiltinError(b.errorFunction(), builtinErr)
		}
		return runtimeBuiltinError(b.errorFunction(), builtinErr.Message)
	}
	if provider, ok := err.(interface {
		ExceptionData() *doggoerr.ExceptionData
	}); ok && provider.ExceptionData() != nil {
		return err
	}
	switch err.(type) {
	case *VMError, *RuntimeError:
		return err
	default:
		return err
	}
}

func (b *Builtin) errorFunction() string {
	if b.ErrorFunction != "" {
		return b.ErrorFunction
	}
	return "doggo.core/" + b.Name
}

// vmErrorToDoggoError は vm.Error を internal/errors.DoggoError に変換します。
func vmErrorToDoggoError(e *Error) error {
	if e.Kind == "" {
		// レガシーエラー - そのままランタイムエラーとして扱う
		return doggoerr.RuntimeError(e.Message)
	}
	switch e.Kind {
	case "type":
		return doggoerr.TypeError(e.Context, e.Expected, e.Got)
	case "argument":
		// Message はすでにコンテキストプレフィックスを含むので、除去して使用
		return doggoerr.Newf(doggoerr.KindArgument, "%s", stripContextPrefix(e.Message, e.Context))
	default:
		// Message はすでにコンテキストプレフィックスを含むので、除去して使用
		return doggoerr.EvalError(e.Context, stripContextPrefix(e.Message, e.Context))
	}
}

// stripContextPrefix は message から "context: " プレフィックスを除去します。
func stripContextPrefix(message, context string) string {
	prefix := context + ": "
	if len(message) > len(prefix) && message[:len(prefix)] == prefix {
		return message[len(prefix):]
	}
	return message
}

// String は組み込み関数の文字列表現を返します。
func (b *Builtin) String() string {
	return "#<builtin>"
}

// Type は型名を返します。
func (b *Builtin) Type() string {
	return "builtin"
}

// Hash はハッシュ値を返します。
func (b *Builtin) Hash() uint32 {
	return util.HashString(fmt.Sprintf("%p", b))
}

// Equals は等価性を判定します。
func (b *Builtin) Equals(other types.Value) bool {
	o, ok := other.(*Builtin)
	if !ok {
		return false
	}
	return b == o
}

// DirectApply implements DirectCallable interface for fast HOF operations.
// Phase 10b: direct apply fast path
// Only pure Fn builtins (not CtxFn) can use this fast path.
// Returns (result, true) on success, (nil, false) if fallback is needed.
func (b *Builtin) DirectApply(args []types.Value) (types.Value, bool) {
	// Phase 10k: 2引数の場合は Fn2 を使用（variadic アロケーション回避）
	if len(args) == 2 && b.Fn2 != nil {
		result := b.Fn2(args[0], args[1])
		if result == nil {
			return atom.NilValue, true
		}
		// エラーチェック - エラーの場合もフォールバック
		if _, ok := result.(*Error); ok {
			return nil, false
		}
		if coll.CollectionError(result) != nil {
			return nil, false
		}
		return result, true
	}

	if b.Fn == nil {
		// CtxFn builtins cannot use direct apply - need full VM dispatch
		return nil, false
	}
	result := b.Fn(args...)
	if result == nil {
		return atom.NilValue, true
	}
	// エラーチェック - エラーの場合もフォールバック
	if _, ok := result.(*Error); ok {
		return nil, false
	}
	if coll.CollectionError(result) != nil {
		return nil, false
	}
	return result, true
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (b *Builtin) IsSelfEvaluating() bool {
	return true
}

// BuiltinDefinition は組み込み関数の定義を表します。
type BuiltinVisibility string

const (
	BuiltinPublic  BuiltinVisibility = "public"
	BuiltinPrivate BuiltinVisibility = "private"
)

type BuiltinDefinition struct {
	Name          string
	ErrorFunction string
	Builtin       *Builtin
	Visibility    BuiltinVisibility
}

func (d BuiltinDefinition) IsPublic() bool {
	return d.Visibility == "" || d.Visibility == BuiltinPublic
}

// BuiltinCatalog is an explicit, validated builtin definition set.
// Runner/VM instances compose against a catalog instead of package init side effects.
type BuiltinCatalog struct {
	definitions []BuiltinDefinition
	byName      map[string]*Builtin
}

func cloneBuiltin(builtin *Builtin, name string) *Builtin {
	if builtin == nil {
		return nil
	}
	cloned := *builtin
	cloned.Name = name
	return &cloned
}

func cloneBuiltinDefinition(def BuiltinDefinition) BuiltinDefinition {
	def.Builtin = cloneBuiltin(def.Builtin, def.Name)
	return def
}

func NewBuiltinCatalog(groups ...[]BuiltinDefinition) (*BuiltinCatalog, error) {
	total := 0
	for _, group := range groups {
		total += len(group)
	}

	definitions := make([]BuiltinDefinition, 0, total)
	byName := make(map[string]*Builtin, total)
	for _, group := range groups {
		for _, def := range group {
			name := strings.TrimSpace(def.Name)
			if name == "" {
				return nil, fmt.Errorf("builtin name must not be empty")
			}
			if def.Builtin == nil {
				return nil, fmt.Errorf("builtin %q must not be nil", name)
			}
			if _, exists := byName[name]; exists {
				return nil, fmt.Errorf("duplicate builtin %q", name)
			}

			def.Name = name
			def.ErrorFunction = strings.TrimSpace(def.ErrorFunction)
			if def.ErrorFunction == "" && def.IsPublic() {
				def.ErrorFunction = "doggo.core/" + name
			}
			def = cloneBuiltinDefinition(def)
			def.Builtin.ErrorFunction = def.ErrorFunction
			definitions = append(definitions, def)
			byName[name] = def.Builtin
		}
	}

	return &BuiltinCatalog{
		definitions: definitions,
		byName:      byName,
	}, nil
}

func MustBuiltinCatalog(groups ...[]BuiltinDefinition) *BuiltinCatalog {
	catalog, err := NewBuiltinCatalog(groups...)
	if err != nil {
		panic(err)
	}
	return catalog
}

func (c *BuiltinCatalog) Definitions() []BuiltinDefinition {
	if c == nil || len(c.definitions) == 0 {
		return nil
	}
	out := make([]BuiltinDefinition, len(c.definitions))
	for i, def := range c.definitions {
		out[i] = cloneBuiltinDefinition(def)
	}
	return out
}

func (c *BuiltinCatalog) lookup(name string) (*Builtin, bool) {
	if c == nil {
		return nil, false
	}
	builtin, ok := c.byName[name]
	return builtin, ok
}

func (c *BuiltinCatalog) Lookup(name string) (*Builtin, bool) {
	builtin, ok := c.lookup(name)
	if !ok {
		return nil, false
	}
	return cloneBuiltin(builtin, builtin.Name), true
}

func (c *BuiltinCatalog) builtinAt(index int) (*Builtin, bool) {
	if c == nil || index < 0 || index >= len(c.definitions) {
		return nil, false
	}
	return c.definitions[index].Builtin, true
}

func (c *BuiltinCatalog) BuiltinAt(index int) (*Builtin, bool) {
	builtin, ok := c.builtinAt(index)
	if !ok {
		return nil, false
	}
	return cloneBuiltin(builtin, builtin.Name), true
}

func builtinCatalogOrDefault(catalog *BuiltinCatalog) *BuiltinCatalog {
	if catalog == nil {
		return DefaultBuiltinCatalog()
	}
	return catalog
}

// Error はVMエラーを表します。
// Kind, Context, Expected, Got は構造化エラー情報を保持します（オプション）。
type Error struct {
	Message  string
	Kind     string // "type", "argument", "runtime" 等（空の場合はレガシーエラー）
	Context  string // 関数名
	Expected string // 型エラー用：期待される型
	Got      string // 型エラー用：実際の型
}

func (e *Error) String() string         { return "ERROR: " + e.Message }
func (e *Error) Error() string          { return e.Message }
func (e *Error) Type() string           { return "error" }
func (e *Error) Hash() uint32           { return util.HashString(e.Message) }
func (e *Error) IsSelfEvaluating() bool { return true }
func (e *Error) Equals(other types.Value) bool {
	o, ok := other.(*Error)
	if !ok {
		return false
	}
	return e.Message == o.Message
}

func newArgumentError(format string, a ...interface{}) *Error {
	return &Error{Message: fmt.Sprintf(format, a...), Kind: "argument"}
}
