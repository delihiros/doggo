package compiler

import (
	"strings"
)

const (
	// DefaultNamespace はデフォルトの名前空間です
	DefaultNamespace = "user"
	// CoreNamespace はコアライブラリの名前空間です（内部用）
	CoreNamespace = "core"
	// StdlibCoreNamespace は標準ライブラリのコア名前空間です（doggo.core）
	// どの名前空間からも非修飾で参照可能なシンボル（*ns* など）を定義します
	StdlibCoreNamespace = "doggo.core"
	// NamespaceSeparator は名前空間とシンボル名を区切る文字です
	NamespaceSeparator = "/"
)

// ParseQualifiedSymbol は修飾されたシンボル文字列を名前空間と名前に分解します。
// "foo/bar" -> ("foo", "bar")
// "bar" -> ("", "bar")
// "foo.baz/bar" -> ("foo.baz", "bar")
func ParseQualifiedSymbol(sym string) (namespace, name string) {
	idx := strings.LastIndex(sym, NamespaceSeparator)
	if idx == -1 {
		return "", sym
	}
	return sym[:idx], sym[idx+1:]
}

// QualifySymbol は名前空間と名前から修飾シンボル文字列を作成します。
// ("foo", "bar") -> "foo/bar"
// ("", "bar") -> "bar"
func QualifySymbol(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + NamespaceSeparator + name
}

// IsQualifiedSymbol はシンボルが名前空間修飾されているかどうかを判定します。
func IsQualifiedSymbol(sym string) bool {
	return strings.Contains(sym, NamespaceSeparator)
}
