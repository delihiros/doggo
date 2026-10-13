package compiler

import (
	"fmt"
	"sort"

	"doggo/internal/types/coll"
)

// SymbolScope はシンボルのスコープを表します。
type SymbolScope string

const (
	GlobalScope          SymbolScope = "GLOBAL"
	ProtectedGlobalScope SymbolScope = "PROTECTED_GLOBAL"
	LocalScope           SymbolScope = "LOCAL"
	BuiltinScope         SymbolScope = "BUILTIN"
	FreeScope            SymbolScope = "FREE"
)

// Symbol はシンボルの情報を保持します。
type Symbol struct {
	Name      string
	Namespace string // グローバルシンボルの場合の名前空間
	Scope     SymbolScope
	Index     int
}

// SymbolTable はシンボルテーブルを表します。
type SymbolTable struct {
	Outer                *SymbolTable
	store                map[string]Symbol // ローカル/組み込み用
	globalStore          map[string]Symbol // 名前空間付きグローバル用 (キーは "ns/name")
	numDefinitions       int
	numLocals            int
	maxLocals            int
	FreeSymbols          []Symbol
	currentNs            string            // 現在の名前空間
	isBlockScope         bool              // ブロックスコープかどうか（catch などで使用）
	aliases              map[string]string // 名前空間 alias（alias -> 実ns）
	macroRefers          map[string]string // マクロ refer（currentNs/localName -> sourceNs/name）
	varRefers            map[string]string // var refer（currentNs/localName -> sourceNs/name）
	namespaceMeta        map[string]*coll.Map
	coreExcludes         map[string]map[string]struct{} // ns -> excluded core symbol set
	builtinAliases       map[string]map[string]int      // ns -> localName -> builtin index
	builtinNames         map[int]string                 // builtin index -> canonical runtime name
	privateGlobals       map[string]struct{}
	privateBuiltins      map[string]int
	allowPrivateBuiltins bool
}

const MaxLocalSlots = 256

// NewSymbolTable は新しいシンボルテーブルを作成します。
func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		store:           make(map[string]Symbol),
		globalStore:     make(map[string]Symbol),
		FreeSymbols:     []Symbol{},
		currentNs:       DefaultNamespace,
		aliases:         make(map[string]string),
		macroRefers:     make(map[string]string),
		varRefers:       make(map[string]string),
		namespaceMeta:   make(map[string]*coll.Map),
		coreExcludes:    make(map[string]map[string]struct{}),
		builtinAliases:  make(map[string]map[string]int),
		builtinNames:    make(map[int]string),
		privateGlobals:  make(map[string]struct{}),
		privateBuiltins: make(map[string]int),
	}
}

// NewEnclosedSymbolTable は囲まれたシンボルテーブルを作成します。
func NewEnclosedSymbolTable(outer *SymbolTable) *SymbolTable {
	s := NewSymbolTable()
	s.Outer = outer
	// 内側のスコープは親の名前空間を継承
	if outer != nil {
		s.currentNs = outer.currentNs
	}
	return s
}

// NewBlockSymbolTable はブロックスコープ用のシンボルテーブルを作成します。
// ブロックスコープは catch や let などで使用され、以下の特性を持ちます:
// - 親スコープの current local slot 数を継承（Local インデックスが連番になる）
// - 親の Local を Resolve しても Free 変換しない（同一関数内なので）
// - leave 時に max local slot 数だけを親に伝播し、current slot 数は巻き戻す
func NewBlockSymbolTable(outer *SymbolTable) *SymbolTable {
	s := NewSymbolTable()
	s.Outer = outer
	s.isBlockScope = true
	if outer != nil {
		s.currentNs = outer.currentNs
		s.numLocals = outer.numLocals
		s.maxLocals = outer.numLocals
	}
	return s
}

// LeaveBlockScope はブロックスコープを抜ける際に呼び出し、
// そのブロック内で必要だった最大 local slot 数だけを親へ伝播させます。
func (s *SymbolTable) LeaveBlockScope() {
	if s.Outer != nil && s.isBlockScope {
		if s.maxLocals > s.Outer.maxLocals {
			s.Outer.maxLocals = s.maxLocals
		}
	}
}

// SetCurrentNamespace は現在の名前空間を設定します。
func (s *SymbolTable) SetCurrentNamespace(ns string) {
	s.currentNs = ns
}

// CurrentNamespace は現在の名前空間を返します。
func (s *SymbolTable) CurrentNamespace() string {
	return s.currentNs
}

// SetNamespaceMetadata は名前空間メタデータを保存します。
func (s *SymbolTable) SetNamespaceMetadata(ns string, metadata *coll.Map) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	if metadata == nil {
		delete(root.namespaceMeta, ns)
		return
	}
	root.namespaceMeta[ns] = metadata
}

// GetNamespaceMetadata は名前空間メタデータを取得します。
func (s *SymbolTable) GetNamespaceMetadata(ns string) *coll.Map {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	return root.namespaceMeta[ns]
}

// SetCoreExcludes は指定名前空間の core シンボル除外セットを置換します。
func (s *SymbolTable) SetCoreExcludes(ns string, symbols []string) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	if len(symbols) == 0 {
		delete(root.coreExcludes, ns)
		return
	}
	set := make(map[string]struct{}, len(symbols))
	for _, sym := range symbols {
		set[sym] = struct{}{}
	}
	root.coreExcludes[ns] = set
}

// ExcludeCoreSymbol は指定名前空間で core シンボルを除外します。
func (s *SymbolTable) ExcludeCoreSymbol(ns, symbol string) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	set, ok := root.coreExcludes[ns]
	if !ok {
		set = make(map[string]struct{})
		root.coreExcludes[ns] = set
	}
	set[symbol] = struct{}{}
}

// IncludeCoreSymbol は指定名前空間の core シンボル除外を解除します。
func (s *SymbolTable) IncludeCoreSymbol(ns, symbol string) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	set, ok := root.coreExcludes[ns]
	if !ok {
		return
	}
	delete(set, symbol)
	if len(set) == 0 {
		delete(root.coreExcludes, ns)
	}
}

// AddBuiltinAlias は指定名前空間に builtin の別名を登録します。
// 既存の名前と衝突する場合はエラーを返します。
func (s *SymbolTable) AddBuiltinAlias(ns, localName string, index int) error {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	if ns == "" {
		ns = root.currentNs
	}
	if localName == "" {
		return &ReferError{Namespace: ns, Symbol: localName, Message: "builtin alias name is empty"}
	}

	if sym, ok := root.store[localName]; ok && sym.Scope == BuiltinScope {
		if sym.Index != index {
			return &ReferError{Namespace: ns, Symbol: localName, Message: "builtin already exists"}
		}
		return nil
	}

	if err := root.validateVisibleNameAvailability(ns, localName, visibleNameAllowance{builtinIndex: &index}); err != nil {
		return err
	}
	byNS := root.builtinAliases[ns]
	if byNS == nil {
		byNS = make(map[string]int)
		root.builtinAliases[ns] = byNS
	}
	byNS[localName] = index
	return nil
}

type symbolTableSnapshot struct {
	currentNs      string
	store          map[string]Symbol
	globalStore    map[string]Symbol
	numDefinitions int
	numLocals      int
	maxLocals      int
	freeSymbols    []Symbol
	aliases        map[string]string
	macroRefers    map[string]string
	varRefers      map[string]string
	namespaceMeta  map[string]*coll.Map
	coreExcludes   map[string]map[string]struct{}
	builtinAliases map[string]map[string]int
	builtinNames   map[int]string
	privateGlobals map[string]struct{}
}

func (s *SymbolTable) Snapshot() *symbolTableSnapshot {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	return &symbolTableSnapshot{
		currentNs:      root.currentNs,
		store:          cloneSymbolMap(root.store),
		globalStore:    cloneSymbolMap(root.globalStore),
		numDefinitions: root.numDefinitions,
		numLocals:      root.numLocals,
		maxLocals:      root.maxLocals,
		freeSymbols:    cloneSymbolSlice(root.FreeSymbols),
		aliases:        cloneStringMap(root.aliases),
		macroRefers:    cloneStringMap(root.macroRefers),
		varRefers:      cloneStringMap(root.varRefers),
		namespaceMeta:  cloneNamespaceMetaMap(root.namespaceMeta),
		coreExcludes:   cloneStringSetMap(root.coreExcludes),
		builtinAliases: cloneIntMapMap(root.builtinAliases),
		builtinNames:   cloneIntStringMap(root.builtinNames),
		privateGlobals: cloneStringSet(root.privateGlobals),
	}
}

func (s *SymbolTable) Restore(snapshot *symbolTableSnapshot) {
	if snapshot == nil {
		return
	}
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	root.currentNs = snapshot.currentNs
	root.store = cloneSymbolMap(snapshot.store)
	root.globalStore = cloneSymbolMap(snapshot.globalStore)
	root.numDefinitions = snapshot.numDefinitions
	root.numLocals = snapshot.numLocals
	root.maxLocals = snapshot.maxLocals
	root.FreeSymbols = cloneSymbolSlice(snapshot.freeSymbols)
	root.aliases = cloneStringMap(snapshot.aliases)
	root.macroRefers = cloneStringMap(snapshot.macroRefers)
	root.varRefers = cloneStringMap(snapshot.varRefers)
	root.namespaceMeta = cloneNamespaceMetaMap(snapshot.namespaceMeta)
	root.coreExcludes = cloneStringSetMap(snapshot.coreExcludes)
	root.builtinAliases = cloneIntMapMap(snapshot.builtinAliases)
	root.builtinNames = cloneIntStringMap(snapshot.builtinNames)
	root.privateGlobals = cloneStringSet(snapshot.privateGlobals)
}

// IsCoreSymbolExcluded は現在名前空間で core シンボルが除外されているかを返します。
func (s *SymbolTable) IsCoreSymbolExcluded(symbol string) bool {
	return s.IsCoreSymbolExcludedInNamespace(s.currentNs, symbol)
}

// IsCoreSymbolExcludedInNamespace は指定名前空間で core シンボルが除外されているかを返します。
func (s *SymbolTable) IsCoreSymbolExcludedInNamespace(ns, symbol string) bool {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	set, ok := root.coreExcludes[ns]
	if !ok {
		return false
	}
	_, exists := set[symbol]
	return exists
}

// AddAlias は名前空間 alias を登録します。
// 例: AddAlias("fb", "foo.bar") で fb/x を foo.bar/x として解決できるようになる
func (s *SymbolTable) AddAlias(alias, namespace string) {
	// ルートシンボルテーブルに登録
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	root.aliases[alias] = namespace
}

// ResolveAlias は alias を実際の名前空間に解決します。
// alias が登録されていなければ元の値をそのまま返します。
func (s *SymbolTable) ResolveAlias(ns string) string {
	// ルートシンボルテーブルから検索
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	if realNs, ok := root.aliases[ns]; ok {
		return realNs
	}
	return ns
}

type visibleNameAllowance struct {
	varQualified   string
	macroQualified string
	builtinIndex   *int
}

func (s *SymbolTable) validateVisibleNameAvailability(ns, localName string, allow visibleNameAllowance) error {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	currentQualified := QualifySymbol(ns, localName)
	if _, exists := root.globalStore[currentQualified]; exists {
		return &ReferError{Namespace: ns, Symbol: localName, Message: "symbol already exists"}
	}
	if existingVar, exists := root.varRefers[currentQualified]; exists && existingVar != allow.varQualified {
		return &ReferError{Namespace: ns, Symbol: localName, Message: "symbol already exists"}
	}
	if existingMacro, exists := root.macroRefers[currentQualified]; exists && existingMacro != allow.macroQualified {
		return &ReferError{Namespace: ns, Symbol: localName, Message: "symbol already exists"}
	}
	if byNS := root.builtinAliases[ns]; byNS != nil {
		if existingIdx, exists := byNS[localName]; exists {
			if allow.builtinIndex != nil && existingIdx == *allow.builtinIndex {
				return nil
			}
			return &ReferError{Namespace: ns, Symbol: localName, Message: "symbol already exists"}
		}
	}
	return nil
}

func (s *SymbolTable) resolveBuiltinSymbolInNamespace(ns, name string) (int, bool) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	if byNS := root.builtinAliases[ns]; byNS != nil {
		if idx, ok := byNS[name]; ok {
			return idx, true
		}
	}
	if (ns == CoreNamespace || ns == StdlibCoreNamespace) && name != "" {
		if sym, ok := root.store[name]; ok && sym.Scope == BuiltinScope {
			return sym.Index, true
		}
	}
	return 0, false
}

// AddRefer は他の名前空間のシンボルを現在の名前空間で使えるようにします。
// 例: AddRefer("foo.bar", "x") で foo.bar/x を x として参照可能に
// localName は現在nsに追加する名前（rename で変更可能）
func (s *SymbolTable) AddRefer(sourceNs, symbolName, localName string) error {
	// ルートシンボルテーブルを取得
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	// 元のシンボルを検索
	qualifiedName := QualifySymbol(sourceNs, symbolName)
	sourceSym, ok := root.globalStore[qualifiedName]
	if !ok {
		return &ReferError{Namespace: sourceNs, Symbol: symbolName, Message: "symbol not found"}
	}
	if sourceSym.Scope == ProtectedGlobalScope {
		return &ReferError{Namespace: sourceNs, Symbol: symbolName, Message: "symbol not found"}
	}
	if root.isPrivateGlobalQualified(qualifiedName) {
		return &ReferError{Namespace: sourceNs, Symbol: symbolName, Message: "symbol not found"}
	}

	currentQualified := QualifySymbol(root.currentNs, localName)
	allowance := visibleNameAllowance{
		varQualified:   qualifiedName,
		macroQualified: qualifiedName,
	}
	if builtinIdx, ok := root.resolveBuiltinSymbolInNamespace(sourceNs, symbolName); ok {
		allowance.builtinIndex = &builtinIdx
	}
	if err := root.validateVisibleNameAvailability(root.currentNs, localName, allowance); err != nil {
		return err
	}

	// refer は public global を現在名前空間に複製せず、非修飾解決 provenance のみを記録する。
	// これにより app/foo は未定義のまま保たれ、(def foo ...) が source namespace を壊さない。
	root.varRefers[currentQualified] = qualifiedName

	return nil
}

// GetPublicSymbolsInNamespace は指定した名前空間の公開シンボル名を返します。
// :refer :all で使用
func (s *SymbolTable) GetPublicSymbolsInNamespace(ns string) []string {
	// ルートシンボルテーブルを取得
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	prefix := ns + "/"
	var symbols []string
	for qualifiedName, sym := range root.globalStore {
		if sym.Scope == ProtectedGlobalScope {
			continue
		}
		if root.isPrivateGlobalQualified(qualifiedName) {
			continue
		}
		if len(qualifiedName) > len(prefix) && qualifiedName[:len(prefix)] == prefix {
			symbolName := qualifiedName[len(prefix):]
			symbols = append(symbols, symbolName)
		}
	}
	return symbols
}

// GetAllCompletionSymbolsInNamespace returns names directly visible from the
// namespace's own global store. Unlike GetPublicSymbolsInNamespace, this includes
// protected globals because they are visible for resolution within the owning
// namespace and useful for REPL completion.
func (s *SymbolTable) GetAllCompletionSymbolsInNamespace(ns string) []string {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	prefix := ns + "/"
	set := make(map[string]struct{})
	for qualifiedName := range root.globalStore {
		if len(qualifiedName) <= len(prefix) || qualifiedName[:len(prefix)] != prefix {
			continue
		}
		localName := qualifiedName[len(prefix):]
		if localName == "" {
			continue
		}
		set[localName] = struct{}{}
	}

	symbols := make([]string, 0, len(set))
	for name := range set {
		symbols = append(symbols, name)
	}
	sort.Strings(symbols)
	return symbols
}

// GetAllVarRefersInNamespace returns unqualified var names that are currently
// referred into the given namespace.
func (s *SymbolTable) GetAllVarRefersInNamespace(ns string) []string {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	return localNamesWithNamespacePrefix(ns, root.varRefers)
}

// GetAllMacroRefersInNamespace returns unqualified macro names that are currently
// referred into the given namespace.
func (s *SymbolTable) GetAllMacroRefersInNamespace(ns string) []string {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	return localNamesWithNamespacePrefix(ns, root.macroRefers)
}

func localNamesWithNamespacePrefix(ns string, qualifiedNames map[string]string) []string {
	prefix := ns + "/"
	set := make(map[string]struct{})
	for localQualified := range qualifiedNames {
		if len(localQualified) <= len(prefix) || localQualified[:len(prefix)] != prefix {
			continue
		}
		localName := localQualified[len(prefix):]
		if localName == "" {
			continue
		}
		set[localName] = struct{}{}
	}

	symbols := make([]string, 0, len(set))
	for name := range set {
		symbols = append(symbols, name)
	}
	sort.Strings(symbols)
	return symbols
}

// GetAllBuiltinAliasesInNamespace returns builtin alias names visible in the namespace.
func (s *SymbolTable) GetAllBuiltinAliasesInNamespace(ns string) []string {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	byNS := root.builtinAliases[ns]
	if byNS == nil {
		return nil
	}

	symbols := make([]string, 0, len(byNS))
	for name := range byNS {
		if name == "" {
			continue
		}
		symbols = append(symbols, name)
	}
	sort.Strings(symbols)
	return symbols
}

// GetAllNamespaces returns all namespaces that currently have global symbols.
func (s *SymbolTable) GetAllNamespaces() []string {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	set := make(map[string]struct{})
	for qualifiedName := range root.globalStore {
		ns, _ := ParseQualifiedSymbol(qualifiedName)
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

// AddMacroRefer は他の名前空間のマクロを現在の名前空間で使えるようにします。
// 例: AddMacroRefer("foo.bar", "deftest", "deftest") で foo.bar/deftest を deftest として参照可能に
func (s *SymbolTable) AddMacroRefer(sourceNs, macroName, localName string) error {
	// ルートシンボルテーブルを取得
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	// currentNs/localName -> sourceNs/macroName のマッピングを登録
	localQualified := QualifySymbol(root.currentNs, localName)
	sourceQualified := QualifySymbol(sourceNs, macroName)
	if err := root.validateVisibleNameAvailability(root.currentNs, localName, visibleNameAllowance{
		varQualified:   sourceQualified,
		macroQualified: sourceQualified,
	}); err != nil {
		return err
	}
	root.macroRefers[localQualified] = sourceQualified
	return nil
}

// AddVarRefer は他の名前空間の var を現在の名前空間で使えるようにした provenance を記録します。
func (s *SymbolTable) AddVarRefer(sourceNs, symbolName, localName string) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	localQualified := QualifySymbol(root.currentNs, localName)
	sourceQualified := QualifySymbol(sourceNs, symbolName)
	root.varRefers[localQualified] = sourceQualified
}

// ResolveMacroRefer は非修飾マクロ名を解決します。
// 現在の名前空間でマクロ refer が登録されている場合、元の修飾名を返します。
// 見つからない場合は空文字列を返します。
func (s *SymbolTable) ResolveMacroRefer(localName string) string {
	// ルートシンボルテーブルを取得
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	localQualified := QualifySymbol(root.currentNs, localName)
	if sourceQualified, ok := root.macroRefers[localQualified]; ok {
		return sourceQualified
	}
	return ""
}

// ResolveVarRefer は非修飾 var 名に対応する refer 元の修飾名を返します。
func (s *SymbolTable) ResolveVarRefer(localName string) string {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	localQualified := QualifySymbol(root.currentNs, localName)
	if sourceQualified, ok := root.varRefers[localQualified]; ok {
		return sourceQualified
	}
	return ""
}

// EffectiveQualifiedName returns the canonical qualified symbol that a resolved
// global binding represents. Referred vars are mapped back to their source
// namespace, while ordinary globals keep their own namespace.
func (s *SymbolTable) EffectiveQualifiedName(requested string, sym Symbol) string {
	if !isGlobalLikeScope(sym.Scope) {
		return QualifySymbol(sym.Namespace, sym.Name)
	}

	ns, localName := ParseQualifiedSymbol(requested)
	if ns == "" {
		if sourceQualified := s.ResolveVarRefer(sym.Name); sourceQualified != "" {
			return sourceQualified
		}
	}
	if ns != "" {
		return QualifySymbol(s.ResolveAlias(ns), localName)
	}

	return QualifySymbol(sym.Namespace, sym.Name)
}

func isGlobalLikeScope(scope SymbolScope) bool {
	return scope == GlobalScope || scope == ProtectedGlobalScope
}

func (s *SymbolTable) SetGlobalPrivateInNamespace(ns, name string, private bool) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	qualifiedName := QualifySymbol(ns, name)
	if private {
		root.privateGlobals[qualifiedName] = struct{}{}
		return
	}
	delete(root.privateGlobals, qualifiedName)
}

func (s *SymbolTable) isPrivateGlobalQualified(qualifiedName string) bool {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	_, ok := root.privateGlobals[qualifiedName]
	return ok
}

// GetMacroRefers は現在の名前空間で参照可能なマクロ refer のマップを返します。
func (s *SymbolTable) GetMacroRefers() map[string]string {
	// ルートシンボルテーブルを取得
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	return root.macroRefers
}

// ReferError は refer 操作のエラーを表します。
type ReferError struct {
	Namespace string
	Symbol    string
	Message   string
}

func (e *ReferError) Error() string {
	return "refer " + e.Namespace + "/" + e.Symbol + ": " + e.Message
}

// Define はローカルシンボルを定義します。
func (s *SymbolTable) Define(name string) Symbol {
	if s.Outer == nil {
		// トップレベルではグローバルとして定義
		symbol := Symbol{
			Name:      name,
			Namespace: s.currentNs,
			Index:     s.numDefinitions,
			Scope:     GlobalScope,
		}
		qualifiedName := QualifySymbol(s.currentNs, name)
		s.clearBindingProvenance(s.currentNs, name)
		s.globalStore[qualifiedName] = symbol
		s.store[name] = symbol // 非修飾名でもアクセス可能にする
		s.numDefinitions++
		return symbol
	}

	symbol := Symbol{Name: name, Index: s.numLocals, Scope: LocalScope}
	s.store[name] = symbol
	s.numLocals++
	if s.numLocals > s.maxLocals {
		s.maxLocals = s.numLocals
	}
	return symbol
}

func (s *SymbolTable) DefineChecked(name string) (Symbol, error) {
	if s.Outer != nil && s.numLocals >= MaxLocalSlots {
		return Symbol{}, fmt.Errorf("too many local bindings: maximum is %d", MaxLocalSlots)
	}
	return s.Define(name), nil
}

// DefineTempChecked reserves a local slot for a compiler-internal temporary without
// adding a resolvable binding to the symbol table. The returned symbol can still be
// used directly with load/set emission helpers.
func (s *SymbolTable) DefineTempChecked(name string) (Symbol, error) {
	if s.numLocals >= MaxLocalSlots {
		return Symbol{}, fmt.Errorf("too many local bindings: maximum is %d", MaxLocalSlots)
	}
	symbol := Symbol{Name: name, Index: s.numLocals, Scope: LocalScope}
	s.numLocals++
	if s.numLocals > s.maxLocals {
		s.maxLocals = s.numLocals
	}
	return symbol, nil
}

// DefineGlobal はグローバルシンボルを現在の名前空間に定義します。
func (s *SymbolTable) DefineGlobal(name string) Symbol {
	return s.DefineGlobalInNamespace(s.currentNs, name)
}

// DefineProtectedGlobal は現在の名前空間に protected global symbol を定義します。
// protected global は通常の def/var では再定義できません。
func (s *SymbolTable) DefineProtectedGlobal(name string) (Symbol, error) {
	return s.DefineProtectedGlobalInNamespace(s.currentNs, name)
}

// DefineProtectedGlobalInNamespace は指定した名前空間に protected global symbol を定義します。
func (s *SymbolTable) DefineProtectedGlobalInNamespace(ns, name string) (Symbol, error) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	qualifiedName := QualifySymbol(ns, name)
	if _, ok := root.globalStore[qualifiedName]; ok {
		return Symbol{}, fmt.Errorf("cannot redefine protected type name: %s", name)
	}

	symbol := Symbol{
		Name:      name,
		Namespace: ns,
		Index:     root.numDefinitions,
		Scope:     ProtectedGlobalScope,
	}
	root.globalStore[qualifiedName] = symbol
	if ns == s.currentNs {
		s.store[name] = symbol
	}
	root.numDefinitions++
	return symbol, nil
}

// ValidateVarGlobalAllowed returns an error when a protected global already owns this name.
func (s *SymbolTable) ValidateVarGlobalAllowed(name string) error {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	qualifiedName := QualifySymbol(s.currentNs, name)
	if existing, ok := root.globalStore[qualifiedName]; ok && existing.Scope == ProtectedGlobalScope {
		return fmt.Errorf("cannot redefine protected type name: %s", name)
	}
	return nil
}

func (s *SymbolTable) DefineVarGlobalChecked(name string) (Symbol, error) {
	if err := s.ValidateVarGlobalAllowed(name); err != nil {
		return Symbol{}, err
	}
	return s.DefineGlobal(name), nil
}

// DefineGlobalInNamespace は指定した名前空間にグローバルシンボルを定義します。
// 既に同名のグローバル変数が存在する場合は、同じインデックスを再利用します。
func (s *SymbolTable) DefineGlobalInNamespace(ns, name string) Symbol {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	qualifiedName := QualifySymbol(ns, name)
	if ns == s.currentNs {
		root.clearBindingProvenance(ns, name)
	}

	// 既存のシンボルがあれば、同じインデックスを再利用
	if existing, ok := root.globalStore[qualifiedName]; ok {
		// 現在の名前空間なら非修飾名でもアクセス可能に更新
		if ns == s.currentNs {
			s.store[name] = existing
		}
		return existing
	}

	symbol := Symbol{
		Name:      name,
		Namespace: ns,
		Index:     root.numDefinitions,
		Scope:     GlobalScope,
	}
	root.globalStore[qualifiedName] = symbol
	// 現在の名前空間なら非修飾名でもアクセス可能
	if ns == s.currentNs {
		s.store[name] = symbol
	}
	root.numDefinitions++
	return symbol
}

func (s *SymbolTable) clearBindingProvenance(ns, name string) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	qualifiedName := QualifySymbol(ns, name)
	delete(root.varRefers, qualifiedName)
	delete(root.macroRefers, qualifiedName)
}

// DefineBuiltin は組み込み関数を定義します。
func (s *SymbolTable) DefineBuiltin(index int, name string) Symbol {
	symbol := Symbol{Name: name, Index: index, Scope: BuiltinScope}
	s.store[name] = symbol
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	for _, ns := range []string{CoreNamespace, StdlibCoreNamespace} {
		byNS := root.builtinAliases[ns]
		if byNS == nil {
			byNS = make(map[string]int)
			root.builtinAliases[ns] = byNS
		}
		byNS[name] = index
	}
	root.builtinNames[index] = name
	return symbol
}

func (s *SymbolTable) DefinePrivateBuiltin(index int, name string) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	root.privateBuiltins[name] = index
	root.builtinNames[index] = name
}

// ResolveBuiltin は組み込み関数のインデックスを解決します。
// 見つからない場合は (0, false) を返します。
func (s *SymbolTable) ResolveBuiltin(name string) (int, bool) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	if byNS := root.builtinAliases[CoreNamespace]; byNS != nil {
		index, ok := byNS[name]
		return index, ok
	}
	return 0, false
}

func (s *SymbolTable) ResolvePrivateBuiltin(name string) (Symbol, bool) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	if !root.allowPrivateBuiltins {
		return Symbol{}, false
	}
	if idx, ok := root.privateBuiltins[name]; ok {
		return Symbol{Name: name, Scope: BuiltinScope, Index: idx, Namespace: StdlibCoreNamespace}, true
	}
	return Symbol{}, false
}

func (s *SymbolTable) BuiltinLayout() []string {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	maxIndex := -1
	for index := range root.builtinNames {
		if index > maxIndex {
			maxIndex = index
		}
	}

	if maxIndex < 0 {
		return nil
	}

	names := make([]string, maxIndex+1)
	for index, name := range root.builtinNames {
		names[index] = name
	}
	return names
}

func (s *SymbolTable) SetAllowPrivateBuiltins(allow bool) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	root.allowPrivateBuiltins = allow
}

func (s *SymbolTable) AllowPrivateBuiltins() bool {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	return root.allowPrivateBuiltins
}

// ResolveIntrinsic resolves runtime core helpers without consulting aliases or the current namespace.
// It is intended for compiler-emitted helper calls such as syntax-quote lowering.
func (s *SymbolTable) ResolveIntrinsic(name string) (Symbol, bool) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	if idx, ok := root.privateBuiltins[name]; ok {
		return Symbol{Name: name, Scope: BuiltinScope, Index: idx, Namespace: StdlibCoreNamespace}, true
	}

	if byNS := root.builtinAliases[StdlibCoreNamespace]; byNS != nil {
		if idx, ok := byNS[name]; ok {
			return Symbol{Name: name, Scope: BuiltinScope, Index: idx, Namespace: StdlibCoreNamespace}, true
		}
	}
	if sym, ok := root.globalStore[QualifySymbol(StdlibCoreNamespace, name)]; ok {
		return sym, true
	}

	if byNS := root.builtinAliases[CoreNamespace]; byNS != nil {
		if idx, ok := byNS[name]; ok {
			return Symbol{Name: name, Scope: BuiltinScope, Index: idx, Namespace: StdlibCoreNamespace}, true
		}
	}
	if sym, ok := root.globalStore[QualifySymbol(CoreNamespace, name)]; ok {
		return sym, true
	}

	return Symbol{}, false
}

// defineFree は自由変数を定義します。
func (s *SymbolTable) defineFree(original Symbol) Symbol {
	s.FreeSymbols = append(s.FreeSymbols, original)

	symbol := Symbol{Name: original.Name, Index: len(s.FreeSymbols) - 1, Scope: FreeScope}
	s.store[original.Name] = symbol
	return symbol
}

// Resolve はシンボルを解決します。
func (s *SymbolTable) Resolve(name string) (Symbol, bool) {
	// 修飾シンボルかどうかをチェック
	ns, localName := ParseQualifiedSymbol(name)
	if ns != "" {
		// 修飾シンボル: 指定された名前空間から検索
		return s.resolveQualified(ns, localName)
	}

	// 非修飾シンボル: ローカル → 親スコープ → グローバル(現在のns) → グローバル(core) → 組み込み
	return s.resolveUnqualified(name)
}

// resolveQualified は修飾されたシンボルを解決します。
func (s *SymbolTable) resolveQualified(ns, name string) (Symbol, bool) {
	// ルートシンボルテーブルまで遡る
	root := s
	for root.Outer != nil {
		root = root.Outer
	}

	// alias を解決（fb/x → foo.bar/x）
	resolvedNs := ns
	if realNs, ok := root.aliases[ns]; ok {
		resolvedNs = realNs
	}

	qualifiedName := QualifySymbol(resolvedNs, name)
	if sym, ok := root.globalStore[qualifiedName]; ok {
		if root.isPrivateGlobalQualified(qualifiedName) && resolvedNs != s.currentNs {
			return Symbol{}, false
		}
		return sym, true
	}

	if byNS := root.builtinAliases[resolvedNs]; byNS != nil {
		if idx, ok := byNS[name]; ok {
			return Symbol{Name: name, Scope: BuiltinScope, Index: idx}, true
		}
	}

	if root.allowPrivateBuiltins && (resolvedNs == CoreNamespace || resolvedNs == StdlibCoreNamespace) {
		if idx, ok := root.privateBuiltins[name]; ok {
			return Symbol{Name: name, Scope: BuiltinScope, Index: idx, Namespace: StdlibCoreNamespace}, true
		}
	}

	if resolvedNs == CoreNamespace || resolvedNs == StdlibCoreNamespace {
		if sym, ok := root.store[name]; ok && sym.Scope == BuiltinScope {
			return sym, true
		}
	}

	return Symbol{}, false
}

// resolveUnqualified は非修飾シンボルを解決します。
func (s *SymbolTable) resolveUnqualified(name string) (Symbol, bool) {
	excludedCore := s.IsCoreSymbolExcluded(name)

	// ローカルストアを検索
	obj, ok := s.store[name]
	if ok {
		// グローバルシンボルの場合、現在の名前空間またはcore/doggo.core名前空間のものか確認
		// 他の名前空間のグローバルシンボルは非修飾では解決しない
		if isGlobalLikeScope(obj.Scope) {
			if obj.Namespace != s.currentNs && obj.Namespace != CoreNamespace && obj.Namespace != StdlibCoreNamespace {
				// 他の名前空間のシンボルは無視（後続の検索へ）
				ok = false
			}
			if (obj.Namespace == CoreNamespace || obj.Namespace == StdlibCoreNamespace) && excludedCore {
				ok = false
			}
		}
		if obj.Scope == BuiltinScope && excludedCore {
			ok = false
		}
		if ok {
			return obj, true
		}
	}

	// 親スコープを検索
	if s.Outer != nil {
		obj, ok = s.Outer.Resolve(name)
		if !ok {
			return obj, ok
		}

		// グローバルはそのまま返す
		if isGlobalLikeScope(obj.Scope) {
			return obj, ok
		}
		// 組み込みは :refer-clojure :exclude の対象なら隠す
		if obj.Scope == BuiltinScope {
			if excludedCore {
				return Symbol{}, false
			}
			return obj, ok
		}

		// ブロックスコープの場合は Free 化せずそのまま返す
		// （catch や let など同一関数内のブロックでは、親の Local をそのまま参照できる）
		if s.isBlockScope {
			return obj, ok
		}

		// 関数スコープの場合は、ローカルまたは自由変数を自由変数として定義
		free := s.defineFree(obj)
		return free, true
	}

	// ルートスコープの場合: グローバルストアを検索
	// 1. 現在の名前空間
	qualifiedName := QualifySymbol(s.currentNs, name)
	if sym, ok := s.globalStore[qualifiedName]; ok {
		return sym, true
	}
	if sourceQualified := s.ResolveVarRefer(name); sourceQualified != "" {
		if sym, ok := s.globalStore[sourceQualified]; ok {
			return sym, true
		}
	}

	if idx, ok := s.resolveBuiltinAlias(name); ok {
		return Symbol{Name: name, Scope: BuiltinScope, Index: idx}, true
	}

	if !excludedCore {
		if idx, ok := s.ResolveBuiltin(name); ok {
			return Symbol{Name: name, Scope: BuiltinScope, Index: idx}, true
		}
	}

	if sym, ok := s.ResolvePrivateBuiltin(name); ok {
		return sym, true
	}

	// 2. core名前空間
	if !excludedCore && s.currentNs != CoreNamespace {
		coreQualifiedName := QualifySymbol(CoreNamespace, name)
		if sym, ok := s.globalStore[coreQualifiedName]; ok {
			return sym, true
		}
	}

	// 3. doggo.core名前空間（*ns* など言語の基本シンボル用）
	if !excludedCore && s.currentNs != StdlibCoreNamespace {
		stdlibCoreQualifiedName := QualifySymbol(StdlibCoreNamespace, name)
		if sym, ok := s.globalStore[stdlibCoreQualifiedName]; ok {
			return sym, true
		}
	}

	return Symbol{}, false
}

func (s *SymbolTable) resolveBuiltinAlias(name string) (int, bool) {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	byNS := root.builtinAliases[s.currentNs]
	if byNS == nil {
		return 0, false
	}
	idx, ok := byNS[name]
	return idx, ok
}

func cloneSymbolMap(in map[string]Symbol) map[string]Symbol {
	out := make(map[string]Symbol, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for name := range in {
		out[name] = struct{}{}
	}
	return out
}

func cloneStringSetMap(in map[string]map[string]struct{}) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{}, len(in))
	for ns, set := range in {
		clonedSet := make(map[string]struct{}, len(set))
		for name := range set {
			clonedSet[name] = struct{}{}
		}
		out[ns] = clonedSet
	}
	return out
}

func cloneIntMapMap(in map[string]map[string]int) map[string]map[string]int {
	out := make(map[string]map[string]int, len(in))
	for ns, m := range in {
		cloned := make(map[string]int, len(m))
		for name, idx := range m {
			cloned[name] = idx
		}
		out[ns] = cloned
	}
	return out
}

func cloneIntStringMap(in map[int]string) map[int]string {
	out := make(map[int]string, len(in))
	for index, name := range in {
		out[index] = name
	}
	return out
}

func cloneSymbolSlice(in []Symbol) []Symbol {
	if len(in) == 0 {
		return nil
	}
	out := make([]Symbol, len(in))
	copy(out, in)
	return out
}

func cloneNamespaceMetaMap(in map[string]*coll.Map) map[string]*coll.Map {
	out := make(map[string]*coll.Map, len(in))
	for ns, meta := range in {
		out[ns] = meta
	}
	return out
}

// ResetLocals はローカル変数の定義をリセットします。
// 複数アリティの関数コンパイル時に、アリティごとにローカル変数をリセットするために使用します。
// 自由変数やグローバル変数の定義は保持されます。
func (s *SymbolTable) ResetLocals() {
	for name, sym := range s.store {
		if sym.Scope == LocalScope {
			delete(s.store, name)
		}
	}
	s.numLocals = 0
	s.maxLocals = 0
}

// ResetTopLevelLocals clears transient local-slot accounting for a fresh top-level compilation.
func (s *SymbolTable) ResetTopLevelLocals() {
	root := s
	for root.Outer != nil {
		root = root.Outer
	}
	for name, sym := range root.store {
		if sym.Scope == LocalScope {
			delete(root.store, name)
		}
	}
	root.numLocals = 0
	root.maxLocals = 0
}

// NumLocals returns the maximum simultaneously-live local-slot count for this lexical scope tree.
func (s *SymbolTable) NumLocals() int {
	return s.maxLocals
}
