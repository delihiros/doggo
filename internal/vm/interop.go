// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"context"
	"fmt"
	"reflect"

	"doggo/internal/env"
	"doggo/internal/gohelpers"
	"doggo/internal/interop"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// ImportCallback はimport実行時に呼ばれるコールバック関数です。
// パッケージ名を受け取り、インポートされたシンボルのマップを返します。
type ImportCallback func(pkgName string) (map[string]types.Value, error)

// executeImport はGoパッケージをインポートします。
func (vm *VM) executeImport(pkgName string) error {
	if vm.importCallback == nil {
		// コールバックが設定されていない場合は、interopから直接取得を試みる
		exports, ok := interop.LookupPackage(pkgName)
		if !ok {
			return fmt.Errorf("import: package not found: %s", pkgName)
		}

		// エクスポートされたシンボルをマップに変換
		pkgMap := make(map[types.Value]types.Value)
		for name, val := range exports {
			doggoVal := vm.convertGoValueToDoggo(name, val)
			pkgMap[types.NewSymbol(name)] = doggoVal
		}

		// パッケージマップをスタックにプッシュ（結果として返す）
		return vm.push(coll.NewMap(pkgMap))
	}

	// コールバックが設定されている場合はそれを使用
	symbols, err := vm.importCallback(pkgName)
	if err != nil {
		return err
	}

	// インポートされたシンボルのマップを作成してプッシュ
	pkgMap := make(map[types.Value]types.Value)
	for name, val := range symbols {
		pkgMap[types.NewSymbol(name)] = val
	}
	return vm.push(coll.NewMap(pkgMap))
}

// executeDot はGoオブジェクトのメソッド呼び出しまたはフィールドアクセスを実行します。
// スタック配置: [..., obj, arg1, ..., argN]
func (vm *VM) executeDot(methodName string, numArgs int) error {
	// 引数をスタックから取得
	args := make([]types.Value, numArgs)
	for i := numArgs - 1; i >= 0; i-- {
		args[i] = vm.pop()
	}

	// オブジェクトをスタックから取得
	objValue := vm.pop()

	// GoObjectかどうか確認
	goObj, ok := objValue.(*atom.GoObject)
	if !ok {
		return vm.newRuntimeError(".: expected go-object, got %s", objValue.Type())
	}

	result, err := gohelpers.CallMethodOrAccessField(goObj, methodName, args)
	if err != nil {
		return vm.newRuntimeError(".: %s", err.Error())
	}

	return vm.push(result)
}

// executeNew はGoオブジェクトを生成します。
// スタック配置: [..., arg1, ..., argN]
func (vm *VM) executeNew(typeName string, numArgs int) error {
	// 引数をスタックから取得
	args := make([]types.Value, numArgs)
	for i := numArgs - 1; i >= 0; i-- {
		args[i] = vm.pop()
	}

	goType, ok := interop.ResolveInstantiationType(typeName)
	if !ok {
		return vm.newRuntimeError("new: type not found: %s", typeName)
	}

	instance, err := gohelpers.InstantiateGoType(goType, args)
	if err != nil {
		return vm.newRuntimeError("new: %s", err.Error())
	}

	// GoObjectとしてラップしてプッシュ
	return vm.push(atom.NewGoObject(instance))
}

// executeReify はGoインターフェースの実装を作成します。
// スタック配置: [..., interface-target, impl-map]
func (vm *VM) executeReify() error {
	implVal := vm.pop()
	interfaceVal := vm.pop()
	goObject, ok := interfaceVal.(*atom.GoObject)
	if !ok {
		return vm.newRuntimeError("reify: first argument must be a Go interface type, got %s", interfaceVal.Type())
	}
	interfaceType, ok := goObject.Unwrap().(reflect.Type)
	if !ok {
		return vm.newRuntimeError("reify: first argument must be a Go interface type, got %T", goObject.Unwrap())
	}
	if interfaceType.Kind() != reflect.Interface || interfaceType.PkgPath() == "" || interfaceType.Name() == "" {
		return vm.newRuntimeError("reify: first argument must be a named Go interface type, got %s", interfaceType)
	}

	// 実装マップをスタックから取得
	implMap, ok := implVal.(types.Map)
	if !ok {
		return vm.newRuntimeError("reify: second argument must be a map, got %s", implVal.Type())
	}

	// reifyファクトリを取得
	factory, ok := interop.LookupReifyFactory(interfaceType)
	if !ok {
		available := interop.ListReifyFactories()
		return vm.newRuntimeError("reify: no reify factory registered for interface %q, available: %v", interfaceType.String(), available)
	}

	// VMCallbackInvokerを作成（生成した実装がDoggo関数を呼び出せるようにする）
	invoker := NewVMCallbackInvoker(vm)
	if invoker.runtime == nil {
		return vm.newRuntimeError("reify: callable runtime not initialized")
	}

	// ダミーの環境を作成（VMは環境を使用しないが、ファクトリのシグネチャに必要）
	dummyEnv := env.NewEnvironment()

	// インターフェース実装を作成
	implementation := factory(invoker, dummyEnv, implMap)

	// GoObjectとしてラップしてプッシュ
	return vm.push(atom.NewGoObject(implementation))
}

// convertGoValueToDoggo はGo値をDoggo値に変換します。
func (vm *VM) convertGoValueToDoggo(name string, val interface{}) types.Value {
	// types.Fn インターフェースを実装している場合
	if fn, ok := val.(types.Fn); ok {
		return fn
	}

	// types.Value の場合
	if value, ok := val.(types.Value); ok {
		return value
	}

	// Goの関数の場合
	fnVal := reflect.ValueOf(val)
	if fnVal.IsValid() && fnVal.Kind() == reflect.Func {
		// GoFunctionWrapper を作成
		return &goFunctionWrapper{name: name, fn: val, vm: vm}
	}

	// reflect.Type の場合
	if rt, isReflectType := val.(reflect.Type); isReflectType {
		return atom.NewGoObject(rt)
	}

	// その他の値は GoObject としてラップ
	converted, err := interop.ConvertReflectValueToDoggo(fnVal)
	if err != nil {
		return atom.NewGoObject(val)
	}
	return converted
}

// goFunctionWrapper はGo関数をVM用にラップします。
type goFunctionWrapper struct {
	name string
	fn   interface{}
	vm   *VM
}

func (g *goFunctionWrapper) String() string {
	return fmt.Sprintf("#<go-function:%s>", g.name)
}

func (g *goFunctionWrapper) Type() string {
	return "function"
}

func (g *goFunctionWrapper) Hash() uint32 {
	return 0
}

func (g *goFunctionWrapper) Equals(other types.Value) bool {
	o, ok := other.(*goFunctionWrapper)
	if !ok {
		return false
	}
	return g == o
}

func (g *goFunctionWrapper) IsSelfEvaluating() bool {
	return true
}

func (g *goFunctionWrapper) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	return callGoFunctionVM(g.fn, args)
}

// callGoFunctionVM はGo関数を呼び出し、結果をDoggo値に変換します。
func callGoFunctionVM(fn interface{}, args []types.Value) (types.Value, error) {
	fnVal := reflect.ValueOf(fn)
	if fnVal.Kind() != reflect.Func {
		return nil, fmt.Errorf("not a function: %T", fn)
	}

	fnType := fnVal.Type()

	// 引数の変換
	reflectArgs, err := interop.ConvertArgsToReflectValues(args, fnType)
	if err != nil {
		return nil, err
	}

	// 関数の呼び出し
	results, err := interop.CallReflect(fnVal, reflectArgs)
	if err != nil {
		return nil, err
	}

	// 結果の変換
	if len(results) == 0 {
		return atom.NilValue, nil
	}

	// 最後の戻り値がerror型かどうかチェック
	lastResult := results[len(results)-1]
	if lastResult.IsValid() && lastResult.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		if !lastResult.IsNil() {
			return nil, lastResult.Interface().(error)
		}
		// エラーがnilなら結果からエラーを除外
		results = results[:len(results)-1]
	}

	if len(results) == 0 {
		return atom.NilValue, nil
	}

	if len(results) == 1 {
		return interop.ConvertReflectValueToDoggo(results[0])
	}

	// 複数の戻り値はベクターにする
	items := make([]types.Value, len(results))
	for i, r := range results {
		val, err := interop.ConvertReflectValueToDoggo(r)
		if err != nil {
			return nil, err
		}
		items[i] = val
	}
	return coll.NewVector(items...), nil
}
