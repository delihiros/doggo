// builtins_runner.go はマクロ展開とファイルロード関連の組み込み関数を定義します。
//
// Runner が提供するコード実行機能と連携する関数を定義します。
//
// 提供する関数:
//   - マクロ展開: macroexpand-1, macroexpand
//   - ファイル操作: load-file, loaded-libs
//   - 評価: eval
package vm

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// macroExpandFn は macroexpand-1/macroexpand 関数を実装するための types.Fn ラッパーです。
type macroExpandFn struct {
	expander  *MacroExpander
	expandAll bool // true: macroexpand, false: macroexpand-1
}

func (m *macroExpandFn) String() string {
	if m.expandAll {
		return "#<builtin:macroexpand>"
	}
	return "#<builtin:macroexpand-1>"
}

func (m *macroExpandFn) Type() string {
	return "function"
}

func (m *macroExpandFn) Hash() uint32 {
	return 0 // ハッシュは重要ではない
}

func (m *macroExpandFn) Equals(other types.Value) bool {
	o, ok := other.(*macroExpandFn)
	if !ok {
		return false
	}
	return m == o
}

func (m *macroExpandFn) IsSelfEvaluating() bool {
	return true
}

func (m *macroExpandFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s: expected 1 argument, got %d", m.String(), len(args))
	}
	form := args[0]

	if m.expandAll {
		return m.expander.MacroExpand(form)
	}
	return m.expander.MacroExpand1(form)
}

// loadFileFn は load-file 関数を実装するための types.Fn ラッパーです。
type loadFileFn struct {
	namespaces *runnerNamespaceServices
}

func (l *loadFileFn) String() string {
	return "#<builtin:load-file>"
}

func (l *loadFileFn) Type() string {
	return "function"
}

func (l *loadFileFn) Hash() uint32 {
	return 0
}

func (l *loadFileFn) Equals(other types.Value) bool {
	o, ok := other.(*loadFileFn)
	if !ok {
		return false
	}
	return l == o
}

func (l *loadFileFn) IsSelfEvaluating() bool {
	return true
}

func (l *loadFileFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("load-file: expected 1 argument, got %d", len(args))
	}
	path, ok := args[0].(atom.String)
	if !ok {
		return nil, fmt.Errorf("load-file: argument must be a string, got %s", args[0].Type())
	}
	return l.namespaces.LoadFileFromContext(ctx, string(path))
}

// requireFn は require 関数を実装するための types.Fn ラッパーです。
type requireFn struct {
	namespaces *runnerNamespaceServices
}

func (r *requireFn) String() string {
	return "#<builtin:require>"
}

func (r *requireFn) Type() string {
	return "function"
}

func (r *requireFn) Hash() uint32 {
	return 0
}

func (r *requireFn) Equals(other types.Value) bool {
	o, ok := other.(*requireFn)
	if !ok {
		return false
	}
	return r == o
}

func (r *requireFn) IsSelfEvaluating() bool {
	return true
}

func (r *requireFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("require: expected at least 1 argument")
	}
	for _, arg := range args {
		if err := r.namespaces.RequireValueFromContext(ctx, arg); err != nil {
			return nil, err
		}
	}
	return atom.NilValue, nil
}

// newLazySeqFn は new-lazy-seq 関数を実装するための types.Fn ラッパーです。
type newLazySeqFn struct {
	runtime LazySeqRuntime
}

func (n *newLazySeqFn) String() string {
	return "#<builtin:new-lazy-seq>"
}

func (n *newLazySeqFn) Type() string {
	return "function"
}

func (n *newLazySeqFn) Hash() uint32 {
	return 0
}

func (n *newLazySeqFn) Equals(other types.Value) bool {
	o, ok := other.(*newLazySeqFn)
	if !ok {
		return false
	}
	return n == o
}

func (n *newLazySeqFn) IsSelfEvaluating() bool {
	return true
}

func (n *newLazySeqFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("new-lazy-seq: expected at least 1 argument (a thunk), got %d", len(args))
	}
	knownInfinite := false
	if len(args) >= 2 {
		marker, ok := args[1].(atom.Boolean)
		if !ok {
			return nil, fmt.Errorf("new-lazy-seq: infinite marker must be boolean, got %s", args[1].Type())
		}
		knownInfinite = bool(marker)
	}
	preservesInfinite := false
	if len(args) >= 3 {
		marker, ok := args[2].(atom.Boolean)
		if !ok {
			return nil, fmt.Errorf("new-lazy-seq: preserves-infinite marker must be boolean, got %s", args[2].Type())
		}
		preservesInfinite = bool(marker)
	}
	boundedSourceWorkPerNode := false
	sourceStart := 3
	if len(args) >= 4 {
		if marker, ok := args[3].(atom.Boolean); ok {
			boundedSourceWorkPerNode = bool(marker)
			sourceStart = 4
		}
	}
	seq := n.runtime.NewLazySeq(forkBindingContext(ctx), args[0])
	if seq != nil {
		seq.infinite = knownInfinite
		seq.preservesInfinite = preservesInfinite
		seq.boundedSourceWorkPerNode = boundedSourceWorkPerNode
		if len(args) > sourceStart {
			seq.sources = append([]types.Value(nil), args[sourceStart:]...)
		}
	}
	return seq, nil
}

// docIntrospectionFn は doc* を実装する types.Fn ラッパーです。
type docIntrospectionFn struct {
	runner *Runner
}

func (d *docIntrospectionFn) String() string {
	return "#<builtin:doc*>"
}

func (d *docIntrospectionFn) Type() string {
	return "function"
}

func (d *docIntrospectionFn) Hash() uint32 {
	return 0
}

func (d *docIntrospectionFn) Equals(other types.Value) bool {
	o, ok := other.(*docIntrospectionFn)
	if !ok {
		return false
	}
	return d == o
}

func (d *docIntrospectionFn) IsSelfEvaluating() bool {
	return true
}

func (d *docIntrospectionFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("doc*: expected 1 argument, got %d", len(args))
	}
	text, err := d.runner.docText(ctx, args[0])
	if err != nil {
		return nil, err
	}
	return atom.NewString(text), nil
}

// sourceIntrospectionFn は source-str* を実装する types.Fn ラッパーです。
type sourceIntrospectionFn struct {
	runner *Runner
}

func (s *sourceIntrospectionFn) String() string {
	return "#<builtin:source-str*>"
}

func (s *sourceIntrospectionFn) Type() string {
	return "function"
}

func (s *sourceIntrospectionFn) Hash() uint32 {
	return 0
}

func (s *sourceIntrospectionFn) Equals(other types.Value) bool {
	o, ok := other.(*sourceIntrospectionFn)
	if !ok {
		return false
	}
	return s == o
}

func (s *sourceIntrospectionFn) IsSelfEvaluating() bool {
	return true
}

func (s *sourceIntrospectionFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("source-str*: expected 1 argument, got %d", len(args))
	}
	text, err := s.runner.sourceText(ctx, args[0])
	if err != nil {
		return nil, err
	}
	return atom.NewString(text), nil
}

type resourcePathFn struct {
	namespaces *runnerNamespaceServices
}

func (r *resourcePathFn) String() string { return "#<builtin:resource-path*>" }
func (r *resourcePathFn) Type() string   { return "function" }
func (r *resourcePathFn) Hash() uint32   { return 0 }
func (r *resourcePathFn) Equals(other types.Value) bool {
	o, ok := other.(*resourcePathFn)
	return ok && r == o
}
func (r *resourcePathFn) IsSelfEvaluating() bool { return true }
func (r *resourcePathFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("resource-path*: expected 1 argument, got %d", len(args))
	}
	name, ok := args[0].(atom.String)
	if !ok {
		return nil, fmt.Errorf("resource-path*: argument must be a string, got %s", args[0].Type())
	}
	if resolved, found := r.namespaces.ResolveResourcePath(string(name)); found {
		return atom.NewString(resolved), nil
	}
	return atom.NilValue, nil
}

type dirIntrospectionFn struct {
	runner *Runner
}

func (d *dirIntrospectionFn) String() string { return "#<builtin:dir*>" }
func (d *dirIntrospectionFn) Type() string   { return "function" }
func (d *dirIntrospectionFn) Hash() uint32   { return 0 }
func (d *dirIntrospectionFn) Equals(other types.Value) bool {
	o, ok := other.(*dirIntrospectionFn)
	return ok && d == o
}
func (d *dirIntrospectionFn) IsSelfEvaluating() bool { return true }
func (d *dirIntrospectionFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("dir*: expected 1 argument, got %d", len(args))
	}
	items, err := d.runner.dirSymbols(args[0])
	if err != nil {
		return nil, err
	}
	return coll.NewVector(items...), nil
}

type aproposIntrospectionFn struct {
	runner *Runner
}

func (a *aproposIntrospectionFn) String() string { return "#<builtin:apropos*>" }
func (a *aproposIntrospectionFn) Type() string   { return "function" }
func (a *aproposIntrospectionFn) Hash() uint32   { return 0 }
func (a *aproposIntrospectionFn) Equals(other types.Value) bool {
	o, ok := other.(*aproposIntrospectionFn)
	return ok && a == o
}
func (a *aproposIntrospectionFn) IsSelfEvaluating() bool { return true }
func (a *aproposIntrospectionFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("apropos*: expected 1 argument, got %d", len(args))
	}
	return coll.NewVector(a.runner.aproposSymbols(args[0])...), nil
}

type findDocIntrospectionFn struct {
	runner *Runner
}

func (f *findDocIntrospectionFn) String() string { return "#<builtin:find-doc*>" }
func (f *findDocIntrospectionFn) Type() string   { return "function" }
func (f *findDocIntrospectionFn) Hash() uint32   { return 0 }
func (f *findDocIntrospectionFn) Equals(other types.Value) bool {
	o, ok := other.(*findDocIntrospectionFn)
	return ok && f == o
}
func (f *findDocIntrospectionFn) IsSelfEvaluating() bool { return true }
func (f *findDocIntrospectionFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("find-doc*: expected 1 argument, got %d", len(args))
	}
	return coll.NewVector(f.runner.findDocEntries(args[0])...), nil
}

type lastExceptionIntrospectionFn struct {
	runner *Runner
}

func (l *lastExceptionIntrospectionFn) String() string { return "#<builtin:last-exception*>" }
func (l *lastExceptionIntrospectionFn) Type() string   { return "function" }
func (l *lastExceptionIntrospectionFn) Hash() uint32   { return 0 }
func (l *lastExceptionIntrospectionFn) Equals(other types.Value) bool {
	o, ok := other.(*lastExceptionIntrospectionFn)
	return ok && l == o
}
func (l *lastExceptionIntrospectionFn) IsSelfEvaluating() bool { return true }
func (l *lastExceptionIntrospectionFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("last-exception*: expected 0 arguments, got %d", len(args))
	}
	return l.runner.lastExceptionValue(), nil
}
