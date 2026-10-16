// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"doggo/internal/compiler"
	"doggo/internal/opcode"
	"doggo/internal/stm"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

const (
	StackSize   = 2048 // オペランドスタックのサイズ
	GlobalsSize = 1024 // グローバル変数の最大数
	MaxFrames   = 256  // コールスタックの最大深度
)

// vmPool は子 VM インスタンスをプールし、アロケーションを削減します。
// applyClosure 等でユーザー定義関数を呼び出す際に使用されます。
var vmPool = sync.Pool{
	New: func() interface{} {
		return &VM{
			stack:  make([]types.Value, StackSize),
			frames: make([]*Frame, MaxFrames),
		}
	},
}

// acquireVM はプールから VM を取得します。
func acquireVM() *VM {
	return vmPool.Get().(*VM)
}

func acquireVMWithBuiltins(catalog *BuiltinCatalog, builtins []*Builtin) *VM {
	vm := acquireVM()
	vm.builtinCatalog = catalog
	vm.builtins = builtins
	return vm
}

// releaseVM は VM をプールに返却します。
func releaseVM(vm *VM) {
	vm.resetForPool()
	vmPool.Put(vm)
}

// resetForPool はプール返却前に VM の状態をリセットします。
// GC が参照を回収できるようにスタック上の値をクリアします。
func (vm *VM) resetForPool() {
	// スタック上の参照をクリア（GC のため）
	for i := 0; i < vm.sp; i++ {
		vm.stack[i] = nil
	}
	for i := range vm.frames {
		vm.frames[i] = nil
	}
	vm.sp = 0
	vm.frameIndex = 0
	// 共有データへの参照をクリア
	vm.constants = nil
	vm.globals = nil
	vm.ctx = nil
	vm.cachedVMCtx = nil // Phase 10f
	vm.stmStore = nil
	vm.profiler = nil
	vm.importCallback = nil
	vm.sourceLineProvider = nil
	vm.observability = nil
	vm.callableRuntime = nil
	vm.session = nil
	vm.builtinCatalog = nil
	vm.builtins = nil
	vm.execStats = nil
	vm.collectStats = false
}

// ExecStats はVM実行統計を保持します。
type ExecStats struct {
	TotalInstructions uint64            // 総命令実行数
	OpcodeStats       map[string]uint64 // opcode別実行数
}

func resolveCatalogBuiltins(catalog *BuiltinCatalog) []*Builtin {
	runtimeCatalog := builtinCatalogOrDefault(catalog)
	builtins := make([]*Builtin, len(runtimeCatalog.definitions))
	for i, def := range runtimeCatalog.definitions {
		builtins[i] = def.Builtin
	}
	return builtins
}

// NewExecStats は新しいExecStatsを作成します。
func NewExecStats() *ExecStats {
	return &ExecStats{
		OpcodeStats: make(map[string]uint64),
	}
}

// VM はDoggo仮想マシンを表します。
type VM struct {
	constants []types.Value // 定数プール

	stack []types.Value // オペランドスタック
	sp    int           // スタックポインタ（次にプッシュする位置）

	globals        []types.Value // グローバル変数
	builtinCatalog *BuiltinCatalog
	builtins       []*Builtin

	frames     []*Frame // コールスタック
	frameIndex int      // 現在のフレームインデックス

	ctx context.Context // 実行コンテキスト（types.Fn呼び出し用）

	importCallback ImportCallback // インポート用コールバック

	stmStore *stm.Store // STM（Software Transactional Memory）ストア

	profiler *Profiler // プロファイラ（nilの場合は無効）

	// 実行統計（nilの場合は収集しない）
	execStats    *ExecStats
	collectStats bool

	// Phase 10f: Builtin 呼び出し用キャッシュ context
	// context.WithValue の呼び出しを削減するため、VM ごとにキャッシュする
	cachedVMCtx context.Context

	session *executionSession

	callableRuntime CallableRuntime

	sourceLineProvider func(file string, line int) (string, bool)

	observability *ObservabilityState
}

func resolveBytecodeBuiltins(bytecode *compiler.Bytecode, catalog *BuiltinCatalog) ([]*Builtin, error) {
	runtimeCatalog := builtinCatalogOrDefault(catalog)
	if len(bytecode.BuiltinNames) == 0 {
		return resolveCatalogBuiltins(runtimeCatalog), nil
	}

	builtins := make([]*Builtin, len(bytecode.BuiltinNames))
	for i, name := range bytecode.BuiltinNames {
		if name == "" {
			return nil, fmt.Errorf("bytecode builtin layout[%d] is empty", i)
		}
		builtin, ok := runtimeCatalog.lookup(name)
		if !ok {
			return nil, fmt.Errorf("runtime builtin catalog missing %q", name)
		}
		builtins[i] = builtin
	}
	return builtins, nil
}

// New は新しいVMを作成します。
func New(bytecode *compiler.Bytecode) (*VM, error) {
	return NewWithBuiltinCatalog(bytecode, nil)
}

func NewWithBuiltinCatalog(bytecode *compiler.Bytecode, catalog *BuiltinCatalog) (*VM, error) {
	builtins, err := resolveBytecodeBuiltins(bytecode, catalog)
	if err != nil {
		return nil, err
	}

	mainFn := &compiler.CompiledFunction{
		Instructions: bytecode.Instructions,
		Lines:        bytecode.Lines,
		Columns:      bytecode.Columns,
		SourceFile:   bytecode.SourceFile,
		Namespace:    bytecode.Namespace,
		NumLocals:    bytecode.NumLocals,
	}
	mainClosure := &compiler.Closure{Fn: mainFn}
	mainFrame := NewFrame(mainClosure, 0)

	frames := make([]*Frame, MaxFrames)
	frames[0] = mainFrame

	vm := &VM{
		constants:      bytecode.Constants,
		stack:          make([]types.Value, StackSize),
		globals:        make([]types.Value, GlobalsSize),
		frames:         frames,
		frameIndex:     1,
		ctx:            withBindingState(context.Background()),
		builtinCatalog: builtinCatalogOrDefault(catalog),
		builtins:       builtins,
	}
	vm.prepareMainFrameLocals(mainFn.NumLocals)
	return vm, nil
}

// SetContext はVMの実行コンテキストを設定します。
func (vm *VM) SetContext(ctx context.Context) {
	vm.ctx = ctx
	vm.cachedVMCtx = nil // Phase 10f: context 変更時はキャッシュをクリア
	vm.callableRuntime = nil
}

// SetImportCallback はインポート用のコールバックを設定します。
func (vm *VM) SetImportCallback(cb ImportCallback) {
	vm.importCallback = cb
	vm.callableRuntime = nil
}

// Reset はVMを初期状態にリセットします（メモリを再利用）。
func (vm *VM) Reset(bytecode *compiler.Bytecode) error {
	builtins, err := resolveBytecodeBuiltins(bytecode, vm.builtinCatalog)
	if err != nil {
		return err
	}

	mainFn := &compiler.CompiledFunction{
		Instructions: bytecode.Instructions,
		Lines:        bytecode.Lines,
		Columns:      bytecode.Columns,
		SourceFile:   bytecode.SourceFile,
		Namespace:    bytecode.Namespace,
		NumLocals:    bytecode.NumLocals,
	}
	mainClosure := &compiler.Closure{Fn: mainFn}
	mainFrame := NewFrame(mainClosure, 0)

	vm.constants = bytecode.Constants
	vm.builtins = builtins
	vm.frames[0] = mainFrame
	vm.frameIndex = 1
	vm.callableRuntime = nil
	vm.prepareMainFrameLocals(mainFn.NumLocals)
	return nil
}

func (vm *VM) prepareMainFrameLocals(numLocals int) {
	if numLocals < 0 {
		numLocals = 0
	}
	if numLocals > len(vm.stack) {
		panic(fmt.Sprintf("invalid bytecode: top-level locals %d exceed stack size %d", numLocals, len(vm.stack)))
	}

	clearCount := vm.sp
	if numLocals > clearCount {
		clearCount = numLocals
	}
	if clearCount > len(vm.stack) {
		clearCount = len(vm.stack)
	}

	for i := 0; i < clearCount; i++ {
		if i < numLocals {
			vm.stack[i] = atom.NilValue
			continue
		}
		vm.stack[i] = nil
	}

	vm.sp = numLocals
}

func (vm *VM) executeReturn(withValue bool) (bool, error) {
	return newVMExecutionMachine(vm).executeReturn(withValue)
}

func (vm *VM) pushClosure(constIndex, numFree int) error {
	return newVMExecutionMachine(vm).pushClosure(constIndex, numFree)
}

// SetSourceLineProvider は file/line からソース行を取得するコールバックを設定します。
func (vm *VM) SetSourceLineProvider(provider func(file string, line int) (string, bool)) {
	vm.sourceLineProvider = provider
	vm.callableRuntime = nil
}

func NewWithGlobalsStoreAndBuiltins(bytecode *compiler.Bytecode, globals []types.Value, catalog *BuiltinCatalog) (*VM, error) {
	vm, err := NewWithBuiltinCatalog(bytecode, catalog)
	if err != nil {
		return nil, err
	}
	vm.globals = globals
	return vm, nil
}

// SetProfiler はプロファイラを設定します。
func (vm *VM) SetProfiler(p *Profiler) {
	vm.profiler = p
	vm.callableRuntime = nil
	if vm.session != nil {
		vm.session.profiler = p
	}
}

// GetProfiler はプロファイラを返します。
func (vm *VM) GetProfiler() *Profiler {
	return vm.profiler
}

// ResetAndEnableExecStats は実行統計を初期化して収集を有効にします。
func (vm *VM) ResetAndEnableExecStats() {
	vm.collectStats = true
	vm.execStats = NewExecStats()
}

// DisableExecStats は実行統計の収集を無効にします。
func (vm *VM) DisableExecStats() {
	vm.collectStats = false
}

// GetExecStats は収集した実行統計を返します。
func (vm *VM) GetExecStats() *ExecStats {
	return vm.execStats
}

// recordOpcode は命令実行を記録します（統計収集が有効な場合）。
func (vm *VM) recordOpcode(op opcode.Opcode) {
	if !vm.collectStats || vm.execStats == nil {
		return
	}
	vm.execStats.TotalInstructions++
	def, err := opcode.Lookup(op)
	if err == nil && def != nil {
		vm.execStats.OpcodeStats[def.Name]++
	}
}

// recordCompletedFrameProfile は正常終了したフレームのプロファイルデータを記録します。
func (vm *VM) recordCompletedFrameProfile(frame *Frame) {
	if vm.profiler == nil || !vm.profiler.IsEnabled() {
		return
	}
	if frame.profileStartTime == 0 {
		return
	}

	duration := time.Duration(time.Now().UnixNano() - frame.profileStartTime)
	fnName := vm.getFrameFunctionName(frame)
	vm.profiler.RecordCompletedFrame(fnName, duration)
}

// getFrameFunctionName はフレームの関数名を取得します。
func (vm *VM) getFrameFunctionName(frame *Frame) string {
	if frame.cl == nil || frame.cl.Fn == nil {
		return "<unknown>"
	}
	if frame.cl.Fn.Name != "" {
		return frame.cl.Fn.Name
	}
	// 関数名がない場合は行番号を使用
	if len(frame.cl.Fn.Lines) > 0 {
		return fmt.Sprintf("<fn:line%d>", frame.cl.Fn.Lines[0])
	}
	return "<anonymous>"
}

// Run はバイトコードを実行します。
// フレームアクセスをキャッシュしてパフォーマンスを最適化しています。
func (vm *VM) Run() error {
	vm.ctx = withBindingState(vm.ctx)
	vm.cachedVMCtx = nil

	var ip int
	var ins opcode.Instructions
	var op opcode.Opcode
	var frame *Frame

	for {
		frame = vm.currentFrame()
		if frame.ip >= len(frame.Instructions())-1 {
			break
		}
		frame.ip++

		ip = frame.ip
		ins = frame.Instructions()
		op = opcode.Opcode(ins[ip])

		// 命令実行統計を記録（有効な場合）
		vm.recordOpcode(op)

		var err error

		switch op {
		// リテラル値
		case opcode.OpConst:
			constIndex := opcode.ReadUint16(ins[ip+1:])
			frame.ip += 2
			err = vm.push(vm.constants[constIndex])

		case opcode.OpTrue:
			err = vm.push(atom.True)

		case opcode.OpFalse:
			err = vm.push(atom.False)

		case opcode.OpNil:
			err = vm.push(atom.NilValue)

		// スタック操作
		case opcode.OpPop:
			vm.pop()

		case opcode.OpDup:
			err = vm.push(vm.stack[vm.sp-1])

		// 算術演算
		case opcode.OpAdd, opcode.OpSub, opcode.OpMul, opcode.OpDiv, opcode.OpMod:
			err = vm.executeBinaryOperation(op)

		case opcode.OpNeg:
			err = vm.executeNegation()

		// 比較演算
		case opcode.OpEq, opcode.OpNotEq, opcode.OpGT, opcode.OpGTE, opcode.OpLT, opcode.OpLTE:
			err = vm.executeComparison(op)

		// 論理演算
		case opcode.OpNot:
			err = vm.executeNot()

			// ジャンプ命令
		case opcode.OpJump:
			pos := int(opcode.ReadUint16(ins[ip+1:]))
			frame.ip = pos - 1

		case opcode.OpJumpNotTruthy:
			pos := int(opcode.ReadUint16(ins[ip+1:]))
			frame.ip += 2
			if !isTruthy(vm.pop()) {
				frame.ip = pos - 1
			}

		case opcode.OpJumpTruthy:
			pos := int(opcode.ReadUint16(ins[ip+1:]))
			frame.ip += 2
			if isTruthy(vm.pop()) {
				frame.ip = pos - 1
			}

		// 変数アクセス
		case opcode.OpGetGlobal:
			globalIndex := opcode.ReadUint16(ins[ip+1:])
			frame.ip += 2
			globalVal := vm.globals[globalIndex]
			// Var の場合: 動的束縛を優先し、なければ root 値を読む
			if v, ok := globalVal.(*atom.Var); ok {
				if boundVal, found := LookupBinding(vm.ctx, v); found {
					err = vm.push(boundVal)
				} else {
					err = vm.push(v.Value)
				}
			} else {
				// Cell は unwrap しない（クロージャキャプチャで Cell 自体を捕捉するため）
				// executeCall で Cell は unwrap される
				err = vm.push(globalVal)
			}

		case opcode.OpSetGlobal:
			globalIndex := opcode.ReadUint16(ins[ip+1:])
			frame.ip += 2
			newVal := vm.pop()
			slot := vm.globals[globalIndex]
			// Cell透過: スロットがCellなら中身を更新（letfn で使用）
			if cell, ok := slot.(*Cell); ok {
				cell.Value = newVal
			} else if v, ok := slot.(*atom.Var); ok {
				// Var の場合: root 値を更新
				v.Value = newVal
			} else {
				vm.globals[globalIndex] = newVal
			}

		case opcode.OpGetLocal:
			localIndex := opcode.ReadUint8(ins[ip+1:])
			frame.ip += 1
			// Cell は unwrap しない（クロージャキャプチャで Cell 自体を捕捉するため）
			// executeCall で Cell は unwrap される
			err = vm.push(vm.stack[frame.basePointer+int(localIndex)])

		case opcode.OpInitLocal:
			localIndex := opcode.ReadUint8(ins[ip+1:])
			frame.ip += 1
			vm.stack[frame.basePointer+int(localIndex)] = vm.pop()

		case opcode.OpSetLocal:
			localIndex := opcode.ReadUint8(ins[ip+1:])
			frame.ip += 1
			newVal := vm.pop()
			slot := &vm.stack[frame.basePointer+int(localIndex)]
			// Cell透過: スロットがCellなら中身を更新（letfn で使用）
			if cell, ok := (*slot).(*Cell); ok {
				cell.Value = newVal
			} else {
				*slot = newVal
			}

		case opcode.OpGetFree:
			freeIndex := opcode.ReadUint8(ins[ip+1:])
			frame.ip += 1
			val := frame.cl.Free[freeIndex]
			// Cell透過: Cellなら中身を取り出す
			if cell, ok := val.(*Cell); ok {
				val = cell.Value
			}
			err = vm.push(val)

		case opcode.OpBox:
			// スタックトップの値をCellに包んで置き換える
			val := vm.pop()
			cell := NewCell(val)
			err = vm.push(cell)

		case opcode.OpGetGlobalRaw:
			// グローバル変数のVar自体を取得（#' var-quote 用）
			// Varを値に展開せず、Var自体をスタックにプッシュする
			globalIndex := opcode.ReadUint16(ins[ip+1:])
			frame.ip += 2
			err = vm.push(vm.globals[globalIndex])

		case opcode.OpDefVar:
			globalIndex := opcode.ReadUint16(ins[ip+1:])
			metaIndex := opcode.ReadUint16(ins[ip+3:])
			frame.ip += 4
			vm.executeDefVar(int(globalIndex), int(metaIndex), vm.pop())

		// コレクション構築
		case opcode.OpArray:
			numElements := int(opcode.ReadUint16(ins[ip+1:]))
			frame.ip += 2
			err = vm.executeArray(numElements)

		case opcode.OpHash:
			numElements := int(opcode.ReadUint16(ins[ip+1:]))
			frame.ip += 2
			err = vm.executeHash(numElements)

		case opcode.OpSet:
			numElements := int(opcode.ReadUint16(ins[ip+1:]))
			frame.ip += 2
			err = vm.executeSet(numElements)

		case opcode.OpIndex:
			index := vm.pop()
			left := vm.pop()
			err = vm.executeIndexExpression(left, index)

		// 関数呼び出し
		case opcode.OpCall:
			numArgs := opcode.ReadUint8(ins[ip+1:])
			frame.ip += 1
			err = vm.executeCall(int(numArgs))

		case opcode.OpReturnValue:
			mainFrameEnded, returnErr := vm.executeReturn(true)
			if mainFrameEnded {
				return nil
			}
			err = returnErr

		case opcode.OpReturn:
			mainFrameEnded, returnErr := vm.executeReturn(false)
			if mainFrameEnded {
				return nil
			}
			err = returnErr

		case opcode.OpClosure:
			constIndex := opcode.ReadUint16(ins[ip+1:])
			numFree := opcode.ReadUint8(ins[ip+3:])
			frame.ip += 3
			err = vm.pushClosure(int(constIndex), int(numFree))

		// 組み込み関数
		case opcode.OpGetBuiltin:
			builtinIndex := opcode.ReadUint16(ins[ip+1:])
			frame.ip += 2
			if int(builtinIndex) < 0 || int(builtinIndex) >= len(vm.builtins) {
				err = fmt.Errorf("builtin index out of range: %d", builtinIndex)
			} else {
				err = vm.push(vm.builtins[builtinIndex])
			}

		// Go相互運用
		case opcode.OpImport:
			constIndex := opcode.ReadUint16(ins[ip+1:])
			frame.ip += 2
			pkgName, ok := vm.constants[constIndex].(atom.String)
			if !ok {
				err = fmt.Errorf("import: expected string, got %T", vm.constants[constIndex])
			} else {
				err = vm.executeImport(string(pkgName))
			}

		case opcode.OpDot:
			methodNameIndex := opcode.ReadUint16(ins[ip+1:])
			numArgs := int(opcode.ReadUint8(ins[ip+3:]))
			frame.ip += 3
			methodName, ok := vm.constants[methodNameIndex].(atom.String)
			if !ok {
				err = fmt.Errorf(".: expected string for method name, got %T", vm.constants[methodNameIndex])
			} else {
				err = vm.executeDot(string(methodName), numArgs)
			}

		case opcode.OpNew:
			typeNameIndex := opcode.ReadUint16(ins[ip+1:])
			numArgs := int(opcode.ReadUint8(ins[ip+3:]))
			frame.ip += 3
			typeName, ok := vm.constants[typeNameIndex].(atom.String)
			if !ok {
				err = fmt.Errorf("new: expected string for type name, got %T", vm.constants[typeNameIndex])
			} else {
				err = vm.executeNew(string(typeName), numArgs)
			}

		case opcode.OpReify:
			err = vm.executeReify()

		// 並行処理
		case opcode.OpGo:
			err = vm.executeGo()

		case opcode.OpDosync:
			err = vm.executeDosync()

		// 例外処理
		case opcode.OpThrow:
			value := vm.pop()
			if canonicalExceptionDataFromValue(value) == nil {
				value = invalidThrowValue(value)
			}
			err = &VMError{Value: value}

		case opcode.OpTry:
			offset := opcode.ReadUint16(ins[ip+1:])
			frame.ip += 2
			// ハンドラIPと現在のスタックポインタを保存
			frame.catchStack = append(frame.catchStack, CatchHandler{
				HandlerIP: int(offset),
				SavedSP:   vm.sp,
			})

		case opcode.OpEndTry:
			if len(frame.catchStack) > 0 {
				frame.catchStack = frame.catchStack[:len(frame.catchStack)-1]
			}

		default:
			err = fmt.Errorf("unknown opcode: %d", op)
		}

		if err != nil {
			if handleErr := vm.handleError(err); handleErr != nil {
				return handleErr
			}
		}
	}

	return nil
}
