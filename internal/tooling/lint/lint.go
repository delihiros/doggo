package lint

import (
	"fmt"
	"strings"

	"doggo/internal/syntax/frontend"
	toolindex "doggo/internal/tooling/index"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// Diagnostic は lint 解析結果の 1 件を表します。
// Line/Column は 1-based です。
type Diagnostic struct {
	File    string
	Line    int
	Column  int
	Message string
}

type AnalyzeOptions struct {
	Index                 *toolindex.WorkspaceIndex
	CurrentNamespace      string
	EnableUndefinedSymbol bool
}

// AnalyzeWithOptions はオプション付きで source を解析して lint 診断を返します。
func AnalyzeWithOptions(filePath, source string, opts AnalyzeOptions) []Diagnostic {
	parsed := frontend.Parse(filePath, source)
	if len(parsed.Diagnostics) > 0 {
		issues := make([]Diagnostic, 0, len(parsed.Diagnostics))
		for _, diag := range parsed.Diagnostics {
			issues = append(issues, Diagnostic{
				File:    filePath,
				Line:    diag.Span.Start.Line,
				Column:  diag.Span.Start.Column,
				Message: diag.Message,
			})
		}
		return issues
	}
	forms := parsed.LoweredForms

	issues := lintForms(filePath, forms)
	if !opts.EnableUndefinedSymbol || opts.Index == nil {
		return issues
	}

	currentNS := strings.TrimSpace(opts.CurrentNamespace)
	if ns := namespaceFromForms(forms); ns != "" {
		currentNS = ns
	}
	if currentNS == "" {
		currentNS = "user"
	}

	issues = append(issues, lintUndefinedSymbols(filePath, forms, currentNS, opts.Index)...)
	return issues
}

func lintForms(filePath string, forms []types.Value) []Diagnostic {
	issues := make([]Diagnostic, 0)
	imported := make(map[string]Diagnostic)

	for _, form := range forms {
		walkValue(form, func(list *coll.List) {
			if len(list.Items) == 0 {
				return
			}

			head, ok := types.SymbolName(list.Items[0])
			if !ok {
				return
			}

			switch head {
			case "require":
				for i := 1; i < len(list.Items); i++ {
					if !isStaticRequireArg(list.Items[i]) {
						issues = append(issues, Diagnostic{
							File:    filePath,
							Line:    list.Line,
							Column:  list.Column,
							Message: "require argument must be symbol/keyword/vector literal",
						})
					}
				}
			case "import":
				if len(list.Items) != 2 {
					issues = append(issues, Diagnostic{
						File:    filePath,
						Line:    list.Line,
						Column:  list.Column,
						Message: "import requires exactly 1 argument",
					})
					return
				}

				pkg, ok := list.Items[1].(atom.String)
				if !ok {
					issues = append(issues, Diagnostic{
						File:    filePath,
						Line:    list.Line,
						Column:  list.Column,
						Message: "import argument must be string literal",
					})
					return
				}

				name := string(pkg)
				if prev, exists := imported[name]; exists {
					issues = append(issues, Diagnostic{
						File:    filePath,
						Line:    list.Line,
						Column:  list.Column,
						Message: fmt.Sprintf("duplicate import %q (first at %s:%d:%d)", name, prev.File, prev.Line, prev.Column),
					})
					return
				}

				imported[name] = Diagnostic{
					File:   filePath,
					Line:   list.Line,
					Column: list.Column,
				}
			}
		})
	}

	return issues
}

type symbolScope struct {
	parent  *symbolScope
	symbols map[string]struct{}
}

func newScope(parent *symbolScope) *symbolScope {
	return &symbolScope{
		parent:  parent,
		symbols: make(map[string]struct{}),
	}
}

func (s *symbolScope) define(name string) {
	if s == nil || name == "" {
		return
	}
	s.symbols[name] = struct{}{}
}

func (s *symbolScope) resolve(name string) bool {
	for scope := s; scope != nil; scope = scope.parent {
		if _, ok := scope.symbols[name]; ok {
			return true
		}
	}
	return false
}

func lintUndefinedSymbols(filePath string, forms []types.Value, currentNS string, idx *toolindex.WorkspaceIndex) []Diagnostic {
	issues := make([]Diagnostic, 0)
	scope := newScope(nil)
	for _, form := range forms {
		lintValue(filePath, form, scope, idx, currentNS, 1, 1, &issues)
	}
	return issues
}

func lintValue(filePath string, v types.Value, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, line, col int, issues *[]Diagnostic) {
	switch node := v.(type) {
	case types.Symbol, *types.SymbolWithMeta:
		name, _ := types.SymbolName(node)
		appendUndefinedSymbolIfNeeded(filePath, name, scope, idx, currentNS, line, col, issues)
	case *coll.List:
		lintList(filePath, node, scope, idx, currentNS, line, col, issues)
	case *coll.Vector:
		for _, item := range node.Items {
			lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
		}
	case *coll.Map:
		for _, key := range node.Order {
			lintValue(filePath, key, scope, idx, currentNS, line, col, issues)
			lintValue(filePath, node.Get(key), scope, idx, currentNS, line, col, issues)
		}
	case *coll.Set:
		for _, item := range node.Order {
			lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
		}
	}
}

func lintList(filePath string, list *coll.List, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, parentLine, parentCol int, issues *[]Diagnostic) {
	if list == nil || len(list.Items) == 0 {
		return
	}
	line := parentLine
	if list.Line > 0 {
		line = list.Line
	}
	col := parentCol
	if list.Column > 0 {
		col = list.Column
	}

	head, isSymbol := types.SymbolName(list.Items[0])
	if !isSymbol {
		lintValue(filePath, list.Items[0], scope, idx, currentNS, line, col, issues)
		for _, item := range list.Items[1:] {
			lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
		}
		return
	}

	switch head {
	case "quote", "syntax-quote", "quasiquote":
		return
	case "ns", "require", "import":
		return
	case "def", "defonce":
		lintDefLike(filePath, list, scope, idx, currentNS, line, col, issues)
		return
	case "defn", "defmacro", "fn":
		lintFnLike(filePath, list, scope, idx, currentNS, line, col, issues)
		return
	case "let", "loop":
		lintLetLike(filePath, list, scope, idx, currentNS, line, col, issues)
		return
	case "if-let":
		lintIfLet(filePath, list, scope, idx, currentNS, line, col, issues)
		return
	case "when-let":
		lintWhenLet(filePath, list, scope, idx, currentNS, line, col, issues)
		return
	case "doseq", "for":
		lintBindingSequence(filePath, list, scope, idx, currentNS, line, col, issues)
		return
	case "catch":
		lintCatch(filePath, list, scope, idx, currentNS, line, col, issues)
		return
	case "var":
		if len(list.Items) == 2 {
			return
		}
	}

	if isCompilerRecognizedHeadSymbol(head) {
		for _, item := range list.Items[1:] {
			lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
		}
		return
	}

	appendUndefinedSymbolIfNeeded(filePath, head, scope, idx, currentNS, line, col, issues)
	for _, item := range list.Items[1:] {
		lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
	}
}

func isCompilerRecognizedHeadSymbol(symbol string) bool {
	switch symbol {
	case "if", "do", "let", "letfn", "loop", "recur", "throw", "try", "catch", "finally",
		"fn", "def", "defn", "defmacro", "defonce", "quote", "syntax-quote", "quasiquote",
		"unquote", "unquote-splicing", "splice-unquote", "var", "set!", "import", "require", "ns", "in-ns",
		"when", "when-let", "if-let", "doseq", "for",
		"+", "-", "*", "/", "mod", "=", "not=", ">", ">=", "<", "<=", "not", "and", "or",
		".", "new", "reify", "go", "dosync",
		"defmulti", "defmethod", "defrecord", "deftype":
		return true
	default:
		return false
	}
}

func lintDefLike(filePath string, list *coll.List, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, line, col int, issues *[]Diagnostic) {
	if len(list.Items) < 3 {
		return
	}
	for _, item := range list.Items[2:] {
		lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
	}
}

func lintFnLike(filePath string, list *coll.List, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, line, col int, issues *[]Diagnostic) {
	if len(list.Items) < 2 {
		return
	}
	start := 1
	if head, ok := types.SymbolName(list.Items[0]); ok && head != "fn" {
		start = 2
	}
	if start < len(list.Items) {
		if _, ok := types.SymbolName(list.Items[start]); ok {
			start++
		}
	}
	if start >= len(list.Items) {
		return
	}

	switch body := list.Items[start].(type) {
	case *coll.Vector:
		child := newScope(scope)
		defineBindings(child, body)
		for _, item := range list.Items[start+1:] {
			lintValue(filePath, item, child, idx, currentNS, line, col, issues)
		}
	case *coll.List:
		for i := start; i < len(list.Items); i++ {
			arity, ok := list.Items[i].(*coll.List)
			if !ok || len(arity.Items) == 0 {
				continue
			}
			params, ok := arity.Items[0].(*coll.Vector)
			if !ok {
				continue
			}
			child := newScope(scope)
			defineBindings(child, params)
			for _, item := range arity.Items[1:] {
				lintValue(filePath, item, child, idx, currentNS, line, col, issues)
			}
		}
	default:
		for _, item := range list.Items[start:] {
			lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
		}
	}
}

func lintLetLike(filePath string, list *coll.List, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, line, col int, issues *[]Diagnostic) {
	if len(list.Items) < 2 {
		return
	}
	bindings, ok := list.Items[1].(*coll.Vector)
	if !ok {
		for _, item := range list.Items[1:] {
			lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
		}
		return
	}
	child := newScope(scope)
	for i := 0; i < len(bindings.Items); i += 2 {
		key := bindings.Items[i]
		if i+1 < len(bindings.Items) {
			lintValue(filePath, bindings.Items[i+1], child, idx, currentNS, line, col, issues)
		}
		defineValueBindings(child, key)
	}
	for _, item := range list.Items[2:] {
		lintValue(filePath, item, child, idx, currentNS, line, col, issues)
	}
}

func lintIfLet(filePath string, list *coll.List, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, line, col int, issues *[]Diagnostic) {
	if len(list.Items) < 3 {
		return
	}
	bindings, ok := list.Items[1].(*coll.Vector)
	if !ok || len(bindings.Items) < 2 {
		for _, item := range list.Items[1:] {
			lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
		}
		return
	}

	lintValue(filePath, bindings.Items[1], scope, idx, currentNS, line, col, issues)
	child := newScope(scope)
	defineValueBindings(child, bindings.Items[0])
	lintValue(filePath, list.Items[2], child, idx, currentNS, line, col, issues)
	if len(list.Items) >= 4 {
		lintValue(filePath, list.Items[3], scope, idx, currentNS, line, col, issues)
	}
}

func lintWhenLet(filePath string, list *coll.List, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, line, col int, issues *[]Diagnostic) {
	if len(list.Items) < 3 {
		return
	}
	bindings, ok := list.Items[1].(*coll.Vector)
	if !ok || len(bindings.Items) < 2 {
		for _, item := range list.Items[1:] {
			lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
		}
		return
	}

	lintValue(filePath, bindings.Items[1], scope, idx, currentNS, line, col, issues)
	child := newScope(scope)
	defineValueBindings(child, bindings.Items[0])
	for _, item := range list.Items[2:] {
		lintValue(filePath, item, child, idx, currentNS, line, col, issues)
	}
}

func lintBindingSequence(filePath string, list *coll.List, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, line, col int, issues *[]Diagnostic) {
	lintLetLike(filePath, list, scope, idx, currentNS, line, col, issues)
}

func lintCatch(filePath string, list *coll.List, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, line, col int, issues *[]Diagnostic) {
	if len(list.Items) < 4 {
		for _, item := range list.Items[1:] {
			lintValue(filePath, item, scope, idx, currentNS, line, col, issues)
		}
		return
	}

	child := newScope(scope)
	if name, ok := types.SymbolName(list.Items[2]); ok {
		child.define(name)
	}
	for _, item := range list.Items[3:] {
		lintValue(filePath, item, child, idx, currentNS, line, col, issues)
	}
}

func defineBindings(scope *symbolScope, vec *coll.Vector) {
	if scope == nil || vec == nil {
		return
	}
	for _, item := range vec.Items {
		defineValueBindings(scope, item)
	}
}

func defineValueBindings(scope *symbolScope, v types.Value) {
	switch x := v.(type) {
	case types.Symbol, *types.SymbolWithMeta:
		name, _ := types.SymbolName(x)
		scope.define(name)
	case *coll.Vector:
		for _, item := range x.Items {
			defineValueBindings(scope, item)
		}
	case *coll.Map:
		for _, key := range x.Order {
			defineValueBindings(scope, key)
		}
	}
}

func appendUndefinedSymbolIfNeeded(filePath, symbol string, scope *symbolScope, idx *toolindex.WorkspaceIndex, currentNS string, line, col int, issues *[]Diagnostic) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" || strings.HasPrefix(symbol, ":") {
		return
	}
	if isAlwaysAvailableSymbol(symbol) || isSpecialFormSymbol(symbol) {
		return
	}
	if scope.resolve(symbol) {
		return
	}
	if _, ok := idx.BuiltinSymbols[symbol]; ok {
		return
	}
	if _, ok := idx.ResolveDefinition(currentNS, symbol); ok {
		return
	}
	*issues = append(*issues, Diagnostic{
		File:    filePath,
		Line:    line,
		Column:  col,
		Message: fmt.Sprintf("undefined symbol: %s", symbol),
	})
}

func isSpecialFormSymbol(symbol string) bool {
	switch symbol {
	case "if", "do", "let", "loop", "recur", "throw", "try", "catch", "finally",
		"fn", "def", "defn", "defmacro", "defonce", "quote", "syntax-quote", "quasiquote",
		"unquote", "unquote-splicing", "var", "set!", "import", "require", "ns",
		"when", "when-let", "if-let", "doseq", "for":
		return true
	default:
		return false
	}
}

func isAlwaysAvailableSymbol(symbol string) bool {
	switch symbol {
	case "nil", "true", "false":
		return true
	default:
		return false
	}
}

func namespaceFromForms(forms []types.Value) string {
	for _, form := range forms {
		list, ok := form.(*coll.List)
		if !ok || len(list.Items) < 2 {
			continue
		}
		head, ok := types.SymbolName(list.Items[0])
		if !ok || head != "ns" {
			continue
		}
		ns, ok := types.SymbolName(list.Items[1])
		if ok && ns != "" {
			return ns
		}
	}
	return ""
}

func isStaticRequireArg(v types.Value) bool {
	if v == nil {
		return false
	}

	switch v.(type) {
	case types.Symbol, *types.SymbolWithMeta, atom.Keyword, *coll.Vector:
		return true
	}

	list, ok := v.(*coll.List)
	if !ok || len(list.Items) != 2 {
		return false
	}

	head, ok := types.SymbolName(list.Items[0])
	if !ok || head != "quote" {
		return false
	}

	return isStaticRequireArg(list.Items[1])
}

func walkValue(v types.Value, visit func(*coll.List)) {
	switch node := v.(type) {
	case *coll.List:
		visit(node)
		for _, item := range node.Items {
			walkValue(item, visit)
		}
	case *coll.Vector:
		for _, item := range node.Items {
			walkValue(item, visit)
		}
	case *coll.Map:
		for _, key := range node.Order {
			walkValue(key, visit)
			walkValue(node.Get(key), visit)
		}
	case *coll.Set:
		for _, item := range node.Order {
			walkValue(item, visit)
		}
	}
}
