package interop

import (
	"reflect"
	"sort"
	"strings"
	"sync"

	"doggo/internal/types"
)

//go:generate go run ../../cmd/interop_gen/main.go -o generated.go encoding/json fmt io math net/http os reflect strings time

// Registry はパッケージ名 -> (シンボル名 -> 値) のマップです。
// 値には関数、構造体(のコンストラクタ代わり)、定数などが含まれます。
var Registry = make(map[string]map[string]interface{})

// registryMu は Registry へのアクセスを保護する mutex です。
var registryMu sync.RWMutex

// ReifyFactory はGoインターフェースの実装を作成するファクトリ関数の型です。
// invoker: CallbackInvoker インターフェース (interface{}として受け取る)
// env: 環境
// impl: Doggoのマップ（メソッド実装を含む）
type ReifyFactory func(invoker interface{}, env types.Environment, impl types.Map) interface{}

// reifyRegistry はインターフェース型 -> reifyファクトリのマップです。
var reifyRegistry = map[reflect.Type]ReifyFactory{}

// reifyRegistryMu は reifyRegistry へのアクセスを保護する mutex です。
var reifyRegistryMu sync.RWMutex

// RegisterPackage は外部からパッケージを追加するための関数です。
// 生成されたコード（generated.go）の init() 関数から呼び出されます。
func RegisterPackage(name string, exports map[string]interface{}) {
	registryMu.Lock()
	defer registryMu.Unlock()
	Registry[name] = exports
}

// LookupPackage はパッケージを取得します。
func LookupPackage(name string) (map[string]interface{}, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	pkg, ok := Registry[name]
	return pkg, ok
}

// LookupSymbol はパッケージ内のシンボルを取得します。
func LookupSymbol(packageName, symbolName string) (interface{}, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	pkg, ok := Registry[packageName]
	if !ok {
		return nil, false
	}
	val, ok := pkg[symbolName]
	return val, ok
}

// ResolveInstantiationType resolves the Go type constructed by new.
// typeName must use the package.Type form.
func ResolveInstantiationType(typeName string) (reflect.Type, bool) {
	parts := strings.SplitN(typeName, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}

	packageName := parts[0]
	symbolName := parts[1]

	// レジストリから型を取得
	val, ok := LookupSymbol(packageName, symbolName)
	if !ok {
		return nil, false
	}

	// 値が reflect.Type の場合
	if typ, ok := val.(reflect.Type); ok {
		return typ, true
	}

	// 値が関数の場合、戻り値の型から推論
	fnVal := reflect.ValueOf(val)
	if fnVal.Kind() == reflect.Func {
		// コンストラクタ関数の場合、最初の戻り値の型を返す
		if fnVal.Type().NumOut() > 0 {
			return fnVal.Type().Out(0), true
		}
	}

	// 値の型を返す
	return reflect.TypeOf(val), true
}

// RegisterReifyFactory はインターフェースのreifyファクトリを登録します。
func RegisterReifyFactory(interfaceType reflect.Type, factory ReifyFactory) {
	if interfaceType == nil || interfaceType.Kind() != reflect.Interface || interfaceType.Name() == "" || interfaceType.PkgPath() == "" {
		panic("reify factory key must be a named package-owned interface type")
	}

	reifyRegistryMu.Lock()
	defer reifyRegistryMu.Unlock()
	if _, exists := reifyRegistry[interfaceType]; exists {
		panic("reify factory already registered for " + interfaceType.String())
	}
	reifyRegistry[interfaceType] = factory
}

// LookupReifyFactory はインターフェースのreifyファクトリを取得します。
func LookupReifyFactory(interfaceType reflect.Type) (ReifyFactory, bool) {
	reifyRegistryMu.RLock()
	defer reifyRegistryMu.RUnlock()
	factory, ok := reifyRegistry[interfaceType]
	return factory, ok
}

// ListReifyFactories は登録されているreifyファクトリの一覧を返します。
func ListReifyFactories() []string {
	reifyRegistryMu.RLock()
	defer reifyRegistryMu.RUnlock()
	names := make([]string, 0, len(reifyRegistry))
	for interfaceType := range reifyRegistry {
		names = append(names, interfaceType.String())
	}
	sort.Strings(names)
	return names
}
