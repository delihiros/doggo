// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"sync"

	"doggo/internal/compiler"
	"doggo/internal/opcode"
)

// Phase 10h: Frame プールによるメモリ割り当て削減
var framePool = sync.Pool{
	New: func() interface{} {
		return &Frame{
			catchStack: make([]CatchHandler, 0, 4), // 小さな初期容量
		}
	},
}

// acquireFrame はプールから Frame を取得して初期化します。
func acquireFrame(cl *compiler.Closure, basePointer int) *Frame {
	f := framePool.Get().(*Frame)
	f.cl = cl
	f.ip = -1
	f.basePointer = basePointer
	f.catchStack = f.catchStack[:0] // スライスをリセット（容量は維持）
	f.profileStartTime = 0
	return f
}

// releaseFrame は Frame をプールに返却します。
func releaseFrame(f *Frame) {
	// 参照をクリア（GC のため）
	f.cl = nil
	framePool.Put(f)
}

// CatchHandler は例外ハンドラの情報を保持します。
type CatchHandler struct {
	HandlerIP int // ハンドラの開始位置
	SavedSP   int // try 開始時のスタックポインタ
}

// Frame は関数呼び出しのコンテキストを表します。
type Frame struct {
	cl               *compiler.Closure // 実行中のクロージャ
	ip               int               // Instruction Pointer（現在の命令位置）
	basePointer      int               // ローカル変数領域の開始位置
	catchStack       []CatchHandler    // 例外ハンドラのスタック
	profileStartTime int64             // プロファイリング用開始時刻（ナノ秒）
}

// NewFrame は新しいFrameを作成します。
func NewFrame(cl *compiler.Closure, basePointer int) *Frame {
	return &Frame{
		cl:          cl,
		ip:          -1,
		basePointer: basePointer,
		catchStack:  []CatchHandler{},
	}
}

// Instructions はフレームの命令列を返します。
func (f *Frame) Instructions() opcode.Instructions {
	return f.cl.Fn.Instructions
}

// CurrentLine は現在の命令ポインタに対応する行番号を返します。
// 行番号情報がない場合や範囲外の場合は0を返します。
func (f *Frame) CurrentLine() int {
	if f.cl == nil || f.cl.Fn == nil || f.cl.Fn.Lines == nil || f.ip < 0 || f.ip >= len(f.cl.Fn.Lines) {
		return 0
	}
	return f.cl.Fn.Lines[f.ip]
}

// CurrentColumn は現在の命令ポインタに対応する列番号を返します。
// 列情報がない場合や範囲外の場合は0を返します。
func (f *Frame) CurrentColumn() int {
	if f.cl == nil || f.cl.Fn == nil || f.cl.Fn.Columns == nil || f.ip < 0 || f.ip >= len(f.cl.Fn.Columns) {
		return 0
	}
	return f.cl.Fn.Columns[f.ip]
}

// CurrentFile は現在のフレームのソースファイルを返します。
func (f *Frame) CurrentFile() string {
	if f.cl == nil || f.cl.Fn == nil {
		return ""
	}
	return f.cl.Fn.SourceFile
}

// CurrentNamespace returns the lexical namespace associated with the frame.
func (f *Frame) CurrentNamespace() string {
	if f.cl == nil || f.cl.Fn == nil {
		return ""
	}
	return f.cl.Fn.Namespace
}

// CurrentPosition は現在の file/line/column を返します。
func (f *Frame) CurrentPosition() (string, int, int) {
	return f.CurrentFile(), f.CurrentLine(), f.CurrentColumn()
}
