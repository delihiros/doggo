package vm

import (
	"context"
	"fmt"
	"sort"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// MacroExpander はマクロ展開を管理します。
type MacroExpander struct {
	macros      map[string]*compiler.Closure // マクロ名 → クロージャ
	metadata    map[string]*coll.Map
	symbolTable *compiler.SymbolTable // シンボルテーブル（名前空間解決用）
	executor    closureExecutor
}

// NewMacroExpander は新しい MacroExpander を作成します。
func NewMacroExpander() *MacroExpander {
	return &MacroExpander{
		macros:   make(map[string]*compiler.Closure),
		metadata: make(map[string]*coll.Map),
	}
}

// SetSymbolTable はシンボルテーブルを設定します。
// 名前空間解決でalias/macro referを解決するために使用します。
func (m *MacroExpander) SetSymbolTable(st *compiler.SymbolTable) {
	m.symbolTable = st
}

func (m *MacroExpander) SetClosureExecutor(executor closureExecutor) {
	m.executor = executor
}

// Macros は登録されているマクロのマップを返します。
func (m *MacroExpander) Macros() map[string]*compiler.Closure {
	return m.macros
}

func (m *MacroExpander) MacroMetadata() map[string]*coll.Map {
	out := make(map[string]*coll.Map, len(m.metadata))
	for k, v := range m.metadata {
		out[k] = v
	}
	return out
}

func (m *MacroExpander) ReplaceMacros(macros map[string]*compiler.Closure, metadata map[string]*coll.Map) {
	m.macros = make(map[string]*compiler.Closure, len(macros))
	for k, v := range macros {
		m.macros[k] = v
	}
	m.metadata = make(map[string]*coll.Map, len(metadata))
	for k, v := range metadata {
		m.metadata[k] = v
	}
}

// RegisterMacro はマクロを登録します。
func (m *MacroExpander) RegisterMacro(name string, macro *compiler.Closure, metadata *coll.Map) {
	m.macros[name] = macro
	if metadata != nil {
		m.metadata[name] = metadata
	} else if _, ok := m.metadata[name]; !ok {
		m.metadata[name] = nil
	}
}

// GetPublicMacrosInNamespace は指定した名前空間の公開マクロ名を返します。
// :refer :all でマクロを取り込む際に使用します。
func (m *MacroExpander) GetPublicMacrosInNamespace(ns string) []string {
	prefix := ns + "/"
	var macros []string
	for qualifiedName := range m.macros {
		if m.isPrivateMacroQualified(qualifiedName) {
			continue
		}
		if len(qualifiedName) > len(prefix) && qualifiedName[:len(prefix)] == prefix {
			macroName := qualifiedName[len(prefix):]
			macros = append(macros, macroName)
		}
	}
	return macros
}

func (m *MacroExpander) GetAllCompletionMacrosInNamespace(ns string) []string {
	prefix := ns + "/"
	var macros []string
	for qualifiedName := range m.macros {
		if len(qualifiedName) > len(prefix) && qualifiedName[:len(prefix)] == prefix {
			macroName := qualifiedName[len(prefix):]
			macros = append(macros, macroName)
		}
	}
	return macros
}

// GetAllNamespaces returns namespaces that currently define at least one macro.
func (m *MacroExpander) GetAllNamespaces() []string {
	set := make(map[string]struct{})
	for qualifiedName := range m.macros {
		ns, _ := compiler.ParseQualifiedSymbol(qualifiedName)
		if ns != "" {
			set[ns] = struct{}{}
		}
	}

	namespaces := make([]string, 0, len(set))
	for ns := range set {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)
	return namespaces
}

// HasMacro は指定した名前空間にマクロが存在するかどうかを返します。
func (m *MacroExpander) HasMacro(ns, name string) bool {
	qualifiedName := ns + "/" + name
	_, ok := m.macros[qualifiedName]
	return ok
}

func (m *MacroExpander) HasPublicMacro(ns, name string) bool {
	qualifiedName := ns + "/" + name
	if _, ok := m.macros[qualifiedName]; !ok {
		return false
	}
	return !m.isPrivateMacroQualified(qualifiedName)
}

func (m *MacroExpander) ResolveMacroTarget(symStr string) (string, *compiler.Closure, *coll.Map, bool) {
	qualified := m.resolveMacroQualified(symStr)
	if qualified == "" {
		return "", nil, nil, false
	}
	return qualified, m.macros[qualified], m.metadata[qualified], true
}

// EvaluateMacro はマクロ関数を実行します。
// これは compiler.MacroEvaluator の実装です。
func (m *MacroExpander) EvaluateMacro(fn *compiler.Closure, args []types.Value) (types.Value, error) {
	if m.executor == nil {
		return nil, fmt.Errorf("macro executor is not configured")
	}
	return m.executor.InvokeClosure(context.Background(), fn, args)
}

// MacroExpand1 はフォームを1回だけマクロ展開します。
// フォームがマクロ呼び出しでない場合はそのまま返します。
func (m *MacroExpander) MacroExpand1(form types.Value) (types.Value, error) {
	list, ok := form.(*coll.List)
	if !ok || len(list.Items) == 0 {
		// リストでないか空リストの場合はそのまま返す
		return form, nil
	}

	// 先頭要素がシンボルかチェック
	symStr, ok := types.SymbolName(list.Items[0])
	if !ok {
		return form, nil
	}

	if symStr == "quasiquote" {
		if len(list.Items) != 2 {
			return nil, fmt.Errorf("quasiquote requires exactly 1 argument")
		}
		return compiler.QuasiquoteExpand(list.Items[1], m.symbolTable, m.resolveMacroQualified)
	}

	// マクロを解決
	macro := m.resolveMacro(symStr)
	if macro == nil {
		return form, nil
	}

	// マクロを展開
	args := list.Items[1:]
	expanded, err := m.EvaluateMacro(macro, args)
	if err != nil {
		return nil, fmt.Errorf("macro expansion error: %w", err)
	}

	return expanded, nil
}

// resolveMacro はシンボル名からマクロを解決します。
// コンパイラと同じ解決順序:
//  1. 修飾シンボル: alias を解決して ns/name で検索
//  2. 非修飾シンボル:
//     a. currentNs/name
//     b. macro refer で取り込まれたマクロ
//     c. core/name（core名前空間のマクロ）
//     d. doggo.core/name
func (m *MacroExpander) resolveMacro(symStr string) *compiler.Closure {
	qualified := m.resolveMacroQualified(symStr)
	if qualified == "" {
		return nil
	}
	return m.macros[qualified]
}

func (m *MacroExpander) resolveMacroQualified(symStr string) string {
	qualified := m.lookupMacroQualified(symStr)
	if qualified == "" {
		return ""
	}
	if m.isPrivateMacroQualified(qualified) {
		ns, _ := compiler.ParseQualifiedSymbol(qualified)
		if ns != m.currentNamespace() {
			return ""
		}
	}
	if m.symbolTable != nil {
		if sym, ok := m.symbolTable.Resolve(symStr); ok && macroBindingShadows(symStr, sym, qualified, m.symbolTable) {
			return ""
		}
	}
	return qualified
}

func (m *MacroExpander) lookupMacroQualified(symStr string) string {
	ns, localName := compiler.ParseQualifiedSymbol(symStr)

	if ns != "" {
		// 修飾シンボル: alias を解決
		resolvedNs := ns
		if m.symbolTable != nil {
			resolvedNs = m.symbolTable.ResolveAlias(ns)
		}
		qualifiedName := compiler.QualifySymbol(resolvedNs, localName)
		if _, ok := m.macros[qualifiedName]; ok {
			return qualifiedName
		}
		return ""
	}

	// 非修飾シンボル: 複数の場所を検索
	var currentNs string
	if m.symbolTable != nil {
		currentNs = m.symbolTable.CurrentNamespace()
	} else {
		currentNs = compiler.DefaultNamespace
	}

	// 1. 現在の名前空間
	qualifiedName := compiler.QualifySymbol(currentNs, symStr)
	if _, ok := m.macros[qualifiedName]; ok {
		return qualifiedName
	}

	// 2. macro refer で取り込まれたマクロ
	if m.symbolTable != nil {
		if sourceQualified := m.symbolTable.ResolveMacroRefer(symStr); sourceQualified != "" {
			if _, ok := m.macros[sourceQualified]; ok {
				return sourceQualified
			}
		}
		if m.symbolTable.IsCoreSymbolExcluded(symStr) {
			return ""
		}
	}

	// 3. core 名前空間
	if currentNs != compiler.CoreNamespace {
		coreQualified := compiler.QualifySymbol(compiler.CoreNamespace, symStr)
		if _, ok := m.macros[coreQualified]; ok {
			return coreQualified
		}
	}

	// 4. doggo.core 名前空間も試す（StdlibCoreNamespace）
	if currentNs != compiler.StdlibCoreNamespace {
		doggoCoreName := compiler.QualifySymbol(compiler.StdlibCoreNamespace, symStr)
		if _, ok := m.macros[doggoCoreName]; ok {
			return doggoCoreName
		}
	}

	return ""
}

func (m *MacroExpander) currentNamespace() string {
	if m.symbolTable != nil {
		return m.symbolTable.CurrentNamespace()
	}
	return compiler.DefaultNamespace
}

func (m *MacroExpander) isPrivateMacroQualified(qualifiedName string) bool {
	meta, ok := m.metadata[qualifiedName]
	if !ok || meta == nil {
		return false
	}
	return meta.Get(atom.NewKeyword("private")) == atom.True
}

func macroBindingShadows(requested string, sym compiler.Symbol, macroQualified string, symbolTable *compiler.SymbolTable) bool {
	switch sym.Scope {
	case compiler.GlobalScope:
		return symbolTable.EffectiveQualifiedName(requested, sym) != macroQualified
	case compiler.BuiltinScope:
		return false
	default:
		return true
	}
}

// MacroExpand はフォームを完全にマクロ展開します。
// 展開結果が再びマクロ呼び出しである場合は、変化しなくなるまで繰り返し展開します。
func (m *MacroExpander) MacroExpand(form types.Value) (types.Value, error) {
	current := form

	for {
		expanded, err := m.MacroExpand1(current)
		if err != nil {
			return nil, err
		}

		// 変化がなければ終了
		if expanded.Equals(current) {
			return current, nil
		}

		current = expanded
	}
}
