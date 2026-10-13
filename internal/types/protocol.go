// Package types はDoggo言語の基本的な型を定義します。
package types

import (
	"fmt"
	"reflect"
	"sync"

	"doggo/internal/types/util"
)

// Protocol はClojure風のプロトコルを表現します。
// プロトコルは型に依存しないポリモーフィズムを実現するための機構です。
type Protocol struct {
	id      int64
	Name    string                      // プロトコル名
	Methods []string                    // メソッド名のリスト（定義順）
	mu      sync.RWMutex                // 実装マップへの並行アクセス保護
	impls   map[string]map[string]Value // 型名 -> メソッド名 -> 実装関数
}

// NewProtocol は新しいプロトコルを作成します。
func NewProtocol(name string, methods []string) *Protocol {
	return &Protocol{
		id:      util.NextID(),
		Name:    name,
		Methods: methods,
		impls:   make(map[string]map[string]Value),
	}
}

// String は文字列表現を返します。
func (p *Protocol) String() string {
	return fmt.Sprintf("#<Protocol:%s>", p.Name)
}

// Type は型名を返します。
func (p *Protocol) Type() string {
	return "protocol"
}

// Hash はハッシュ値を返します。
func (p *Protocol) Hash() uint32 {
	return uint32(p.id)
}

// Equals は等価性を判定します。
func (p *Protocol) Equals(other Value) bool {
	if o, ok := other.(*Protocol); ok {
		return p == o
	}
	return false
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (p *Protocol) IsSelfEvaluating() bool {
	return true
}

// Extend は指定された型にプロトコルの実装を追加します。
// impls は メソッド名 -> 実装関数 のマップです。
func (p *Protocol) Extend(typeName string, impls map[string]Value) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.impls[typeName] == nil {
		p.impls[typeName] = make(map[string]Value)
	}
	for methodName, fn := range impls {
		p.impls[typeName][methodName] = fn
	}
}

// FindImpl は指定された値とメソッド名に対する実装を検索します。
// ルックアップ順序:
// 1. nil チェック（"nil" 型）
// 2. Doggo の具体型名
// 3. GoObject の場合、内部の Go 型名
// 4. デフォルト実装 ("Object")
func (p *Protocol) FindImpl(target Value, methodName string) Value {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// nil の場合
	if target == nil || IsNil(target) {
		if impl := p.getImpl("nil", methodName); impl != nil {
			return impl
		}
	}

	// Doggo の型名で検索
	if target != nil {
		typeName := protocolTypeName(target)
		if impl := p.getImpl(typeName, methodName); impl != nil {
			return impl
		}
	}

	// GoObject の場合、内部の Go 型名で検索
	if goObj, ok := target.(GoObjectInterface); ok {
		goTypeName := GetGoTypeName(goObj.Unwrap())
		if impl := p.getImpl(goTypeName, methodName); impl != nil {
			return impl
		}
	}

	// デフォルト実装 ("Object") で検索
	if impl := p.getImpl("Object", methodName); impl != nil {
		return impl
	}

	return nil
}

// Satisfies は指定された値がこのプロトコルを実装しているかを判定します。
func (p *Protocol) Satisfies(target Value) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 必要なメソッドが全て実装されているかチェック
	for _, methodName := range p.Methods {
		if p.findImplUnlocked(target, methodName) == nil {
			return false
		}
	}
	return true
}

// HasImplForType は指定された型名に対して実装が登録されているかを判定します。
func (p *Protocol) HasImplForType(typeName string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	typeImpls, ok := p.impls[typeName]
	if !ok {
		return false
	}

	// 全てのメソッドが実装されているかチェック
	for _, methodName := range p.Methods {
		if _, ok := typeImpls[methodName]; !ok {
			return false
		}
	}
	return true
}

// findImplUnlocked はロックなしで実装を検索します（内部用）。
// Satisfies から呼ばれる場合は既にロックを取得しているため。
func (p *Protocol) findImplUnlocked(target Value, methodName string) Value {
	// nil の場合
	if target == nil || IsNil(target) {
		if impl := p.getImpl("nil", methodName); impl != nil {
			return impl
		}
	}

	// Doggo の型名で検索
	if target != nil {
		typeName := protocolTypeName(target)
		if impl := p.getImpl(typeName, methodName); impl != nil {
			return impl
		}
	}

	// GoObject の場合、内部の Go 型名で検索
	if goObj, ok := target.(GoObjectInterface); ok {
		goTypeName := GetGoTypeName(goObj.Unwrap())
		if impl := p.getImpl(goTypeName, methodName); impl != nil {
			return impl
		}
	}

	// デフォルト実装 ("Object") で検索
	if impl := p.getImpl("Object", methodName); impl != nil {
		return impl
	}

	return nil
}

type protocolTypeNamer interface {
	ProtocolTypeName() string
}

func protocolTypeName(value Value) string {
	if named, ok := value.(protocolTypeNamer); ok {
		return named.ProtocolTypeName()
	}
	return value.Type()
}

// getImpl は型名とメソッド名から実装を取得します（内部用）。
func (p *Protocol) getImpl(typeName, methodName string) Value {
	if typeImpls, ok := p.impls[typeName]; ok {
		if impl, ok := typeImpls[methodName]; ok {
			return impl
		}
	}
	return nil
}

// GoObjectInterface は GoObject のインターフェースです。
// 循環参照を避けるために定義します。
type GoObjectInterface interface {
	Value
	Unwrap() interface{}
}

// GetGoTypeName は Go の値から型名を取得します。
// extend-type で Go の型を指定する際に使用できます。
func GetGoTypeName(v interface{}) string {
	if v == nil {
		return "nil"
	}
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		return "*" + t.Elem().String()
	}
	return t.String()
}

// IsNil は値が nil かどうかを判定します。
// atom.NilValue との比較のために使用します。
func IsNil(v Value) bool {
	if v == nil {
		return true
	}
	// "nil" 型として定義されている場合
	return v.Type() == "nil"
}
