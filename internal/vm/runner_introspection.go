package vm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"doggo/internal/compiler"
	"doggo/internal/syntax/frontend"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// CompletionCandidates は REPL 補完候補（非修飾シンボル）を返します。
// current namespace + core/doggo.core + builtin を統合し、重複除去とソートを行います。
func (r *Runner) CompletionCandidates(prefix string) []string {
	set := make(map[string]struct{})
	addVisible := func(names []string) {
		for _, name := range names {
			if name == "" {
				continue
			}
			if prefix != "" && !strings.HasPrefix(name, prefix) {
				continue
			}
			set[name] = struct{}{}
		}
	}
	addCore := func(names []string) {
		for _, name := range names {
			if name == "" {
				continue
			}
			if r.symbolTable.IsCoreSymbolExcluded(name) {
				continue
			}
			if prefix != "" && !strings.HasPrefix(name, prefix) {
				continue
			}
			set[name] = struct{}{}
		}
	}

	currentNs := r.symbolTable.CurrentNamespace()
	addVisible(r.symbolTable.GetAllCompletionSymbolsInNamespace(currentNs))
	addVisible(r.symbolTable.GetAllVarRefersInNamespace(currentNs))
	addVisible(r.symbolTable.GetAllBuiltinAliasesInNamespace(currentNs))
	addCore(r.symbolTable.GetPublicSymbolsInNamespace(compiler.CoreNamespace))
	addCore(r.symbolTable.GetPublicSymbolsInNamespace(compiler.StdlibCoreNamespace))

	if r.macroExpander != nil {
		addVisible(r.macroExpander.GetAllCompletionMacrosInNamespace(currentNs))
		addVisible(r.symbolTable.GetAllMacroRefersInNamespace(currentNs))
		addCore(r.macroExpander.GetPublicMacrosInNamespace(compiler.CoreNamespace))
		addCore(r.macroExpander.GetPublicMacrosInNamespace(compiler.StdlibCoreNamespace))
	}

	for _, builtin := range r.builtinCatalog.Definitions() {
		if builtin.Name == "" {
			continue
		}
		if !builtin.IsPublic() {
			continue
		}
		addCore([]string{builtin.Name})
	}

	candidates := make([]string, 0, len(set))
	for name := range set {
		candidates = append(candidates, name)
	}
	sort.Strings(candidates)
	return candidates
}

func (r *Runner) docText(ctx context.Context, target types.Value) (string, error) {
	symbolName, err := introspectionSymbolName(target)
	if err != nil {
		return "", err
	}

	resolved := r.resolveIntrospectionTargetWithContext(ctx, symbolName)
	if resolved == nil {
		return fmt.Sprintf("Symbol not found: %s", symbolName), nil
	}

	var sb strings.Builder
	sb.WriteString(resolved.qualifiedName)

	if resolved.metadata != nil {
		if doc, ok := metaString(resolved.metadata, "doc"); ok && doc != "" {
			sb.WriteString("\n  ")
			sb.WriteString(doc)
		}
		if file, ok := metaString(resolved.metadata, "file"); ok && file != "" {
			line, _ := metaInt(resolved.metadata, "line")
			col, _ := metaInt(resolved.metadata, "column")
			if col <= 0 {
				col = 1
			}
			if line > 0 {
				sb.WriteString(fmt.Sprintf("\n  location: %s:%d:%d", file, line, col))
			} else {
				sb.WriteString(fmt.Sprintf("\n  location: %s", file))
			}
		}
	}

	switch v := resolved.value.(type) {
	case *compiler.Closure:
		if resolved.kind == "macro" {
			sb.WriteString("\n  type: macro")
		} else {
			sb.WriteString(fmt.Sprintf("\n  type: %s", v.Type()))
		}
	case *atom.Var:
		if v.Value != nil {
			sb.WriteString(fmt.Sprintf("\n  type: %s", v.Value.Type()))
		} else {
			sb.WriteString("\n  type: nil")
		}
	case *Builtin:
		sb.WriteString("\n  type: builtin")
	default:
		if v != nil {
			sb.WriteString(fmt.Sprintf("\n  type: %s", v.Type()))
		}
	}

	return sb.String(), nil
}

func (r *Runner) sourceText(ctx context.Context, target types.Value) (string, error) {
	symbolName, err := introspectionSymbolName(target)
	if err != nil {
		return "", err
	}

	resolved := r.resolveIntrospectionTargetWithContext(ctx, symbolName)
	if resolved == nil {
		return fmt.Sprintf("Source unavailable for %s", symbolName), nil
	}

	if resolved.metadata != nil {
		if source, ok := metaString(resolved.metadata, "source"); ok && source != "" {
			return source, nil
		}
		file, hasFile := metaString(resolved.metadata, "file")
		line, hasLine := metaInt(resolved.metadata, "line")
		defName, hasName := metaSymbolName(resolved.metadata, "name")
		if !hasName {
			defName = resolved.symbol.Name
		}
		if hasFile && hasLine {
			if source, ok := r.lookupSourceContent(file); ok {
				if form, ok := extractDefinitionForm(source, line, defName); ok {
					return form, nil
				}
			}
		}
	}

	return fmt.Sprintf("Source unavailable for %s", resolved.qualifiedName), nil
}

func (r *Runner) dirSymbols(target types.Value) ([]types.Value, error) {
	nsName, err := introspectionSymbolName(target)
	if err != nil {
		return nil, err
	}

	names := r.dirSymbolNames(nsName)
	items := make([]types.Value, len(names))
	for i, name := range names {
		items[i] = types.NewSymbol(name)
	}
	return items, nil
}

func (r *Runner) aproposSymbols(query types.Value) []types.Value {
	set := make(map[string]struct{})
	items := make([]types.Value, 0)

	addQualified := func(name string) {
		if name == "" {
			return
		}
		if _, ok := set[name]; ok {
			return
		}
		set[name] = struct{}{}
		items = append(items, types.NewSymbol(name))
	}

	for _, nsName := range r.introspectionNamespaces() {
		for _, name := range r.dirSymbolNames(nsName) {
			requested := compiler.QualifySymbol(nsName, name)
			if !matchesIntrospectionQuery(query, requested) && !matchesIntrospectionQuery(query, name) {
				continue
			}
			if resolved := r.resolveIntrospectionTarget(requested); resolved != nil {
				addQualified(resolved.qualifiedName)
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].String() < items[j].String()
	})
	return items
}

func (r *Runner) findDocEntries(query types.Value) []types.Value {
	entries := make([]types.Value, 0)
	seen := make(map[string]struct{})
	for _, nsName := range r.introspectionNamespaces() {
		for _, name := range r.dirSymbolNames(nsName) {
			requested := compiler.QualifySymbol(nsName, name)
			resolved := r.resolveIntrospectionTarget(requested)
			if resolved == nil || resolved.metadata == nil {
				continue
			}
			if _, ok := seen[resolved.qualifiedName]; ok {
				continue
			}

			doc, ok := metaString(resolved.metadata, "doc")
			if !ok || doc == "" {
				continue
			}

			if !matchesIntrospectionQuery(query, resolved.qualifiedName) &&
				!matchesIntrospectionQuery(query, name) &&
				!matchesIntrospectionQuery(query, doc) {
				continue
			}

			seen[resolved.qualifiedName] = struct{}{}
			entries = append(entries, coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("name"), types.NewSymbol(resolved.qualifiedName),
				atom.NewKeyword("doc"), atom.NewString(doc),
			}))
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].(types.Map).Get(atom.NewKeyword("name"))
		right := entries[j].(types.Map).Get(atom.NewKeyword("name"))
		return left.String() < right.String()
	})
	return entries
}

func (r *Runner) introspectionNamespaces() []string {
	set := make(map[string]struct{})
	for _, ns := range r.symbolTable.GetAllNamespaces() {
		if ns != "" {
			set[ns] = struct{}{}
		}
	}
	if r.macroExpander != nil {
		for _, ns := range r.macroExpander.GetAllNamespaces() {
			if ns != "" {
				set[ns] = struct{}{}
			}
		}
	}

	namespaces := make([]string, 0, len(set))
	for ns := range set {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)
	return namespaces
}

func (r *Runner) dirSymbolNames(ns string) []string {
	set := make(map[string]struct{})
	addAll := func(names []string) {
		for _, name := range names {
			if name == "" {
				continue
			}
			set[name] = struct{}{}
		}
	}

	addAll(r.symbolTable.GetPublicSymbolsInNamespace(ns))
	if ns == compiler.StdlibCoreNamespace {
		for _, builtin := range r.builtinCatalog.Definitions() {
			if builtin.Name == "" {
				continue
			}
			if !builtin.IsPublic() {
				continue
			}
			set[builtin.Name] = struct{}{}
		}
	}
	if r.macroExpander != nil {
		addAll(r.macroExpander.GetPublicMacrosInNamespace(ns))
	}

	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func matchesIntrospectionQuery(query types.Value, hay string) bool {
	switch v := query.(type) {
	case atom.String:
		return strings.Contains(hay, string(v))
	case *atom.Regex:
		return v.Re.MatchString(hay)
	case types.Symbol:
		return strings.Contains(hay, string(v))
	case *types.SymbolWithMeta:
		return strings.Contains(hay, v.Name)
	default:
		return false
	}
}

func introspectionSymbolName(target types.Value) (string, error) {
	if target == nil {
		return "", fmt.Errorf("expected symbol, got nil")
	}
	switch v := target.(type) {
	case types.Symbol:
		return string(v), nil
	case *types.SymbolWithMeta:
		return v.Name, nil
	case atom.String:
		return string(v), nil
	default:
		return "", fmt.Errorf("expected symbol, got %s", target.Type())
	}
}

type introspectionTarget struct {
	symbol        compiler.Symbol
	qualifiedName string
	value         types.Value
	metadata      *coll.Map
	kind          string
}

func (r *Runner) resolveIntrospectionTarget(symbolName string) *introspectionTarget {
	sym, ok := r.symbolTable.Resolve(symbolName)
	var macroTarget *introspectionTarget
	if r.macroExpander != nil {
		if qualified, macro, metadata, ok := r.macroExpander.ResolveMacroTarget(symbolName); ok {
			ns, name := compiler.ParseQualifiedSymbol(qualified)
			macroTarget = &introspectionTarget{
				symbol:        compiler.Symbol{Name: name, Namespace: ns},
				qualifiedName: qualified,
				value:         macro,
				metadata:      metadata,
				kind:          "macro",
			}
		}
	}

	if !ok {
		return macroTarget
	}

	if macroTarget != nil && !introspectionBindingShadows(symbolName, sym, macroTarget.qualifiedName, r.symbolTable) {
		return macroTarget
	}

	return r.introspectionValueTarget(symbolName, sym)
}

func (r *Runner) resolveIntrospectionTargetInNamespace(baseNs, symbolName string) *introspectionTarget {
	if baseNs == "" || strings.Contains(symbolName, "/") || baseNs == r.symbolTable.CurrentNamespace() {
		return r.resolveIntrospectionTarget(symbolName)
	}

	saved := r.symbolTable.CurrentNamespace()
	r.symbolTable.SetCurrentNamespace(baseNs)
	defer r.symbolTable.SetCurrentNamespace(saved)
	return r.resolveIntrospectionTarget(symbolName)
}

func (r *Runner) resolveIntrospectionTargetWithContext(ctx context.Context, symbolName string) *introspectionTarget {
	if strings.Contains(symbolName, "/") {
		return r.resolveIntrospectionTarget(symbolName)
	}

	if vm := GetVM(ctx); vm != nil {
		for i := vm.frameIndex - 1; i >= 0; i-- {
			frame := vm.frames[i]
			if frame == nil {
				continue
			}
			ns := frame.CurrentNamespace()
			if ns == "" {
				continue
			}
			if resolved := r.resolveIntrospectionTargetInNamespace(ns, symbolName); resolved != nil {
				return resolved
			}
		}
	}

	return r.resolveIntrospectionTarget(symbolName)
}

func (r *Runner) introspectionValueTarget(requested string, sym compiler.Symbol) *introspectionTarget {
	session := r.getOrCreateExecutionSession()
	target := &introspectionTarget{
		symbol:        sym,
		qualifiedName: sym.Name,
	}
	if sym.Scope == compiler.GlobalScope {
		target.qualifiedName = r.symbolTable.EffectiveQualifiedName(requested, sym)
		if sym.Index >= 0 && sym.Index < len(session.globals) {
			target.value = session.globals[sym.Index]
		}
		if v, ok := target.value.(*atom.Var); ok {
			target.metadata = v.Metadata
			if qualified, ok := metadataQualifiedName(v.Metadata); ok {
				target.qualifiedName = qualified
			}
		}
		return target
	}

	if sym.Scope == compiler.ProtectedGlobalScope {
		target.qualifiedName = r.symbolTable.EffectiveQualifiedName(requested, sym)
		if sym.Index >= 0 && sym.Index < len(session.globals) {
			target.value = session.globals[sym.Index]
		}
		switch v := target.value.(type) {
		case *coll.RecordDef:
			target.metadata = v.Metadata
		case *coll.DeftypeDef:
			target.metadata = v.Metadata
		}
		if qualified, ok := metadataQualifiedName(target.metadata); ok {
			target.qualifiedName = qualified
		}
		return target
	}

	if sym.Scope == compiler.BuiltinScope {
		target.qualifiedName = compiler.QualifySymbol(compiler.StdlibCoreNamespace, sym.Name)
		definitions := r.builtinCatalog.Definitions()
		if sym.Index >= 0 && sym.Index < len(definitions) {
			target.value = definitions[sym.Index].Builtin
		}
		target.kind = "builtin"
		return target
	}

	return target
}

func introspectionBindingShadows(requested string, sym compiler.Symbol, macroQualified string, symbolTable *compiler.SymbolTable) bool {
	switch sym.Scope {
	case compiler.GlobalScope, compiler.ProtectedGlobalScope:
		return symbolTable.EffectiveQualifiedName(requested, sym) != macroQualified
	case compiler.BuiltinScope:
		return false
	default:
		return true
	}
}

func metaString(meta *coll.Map, key string) (string, bool) {
	if meta == nil {
		return "", false
	}
	value := meta.Get(atom.NewKeyword(key))
	if value == nil {
		return "", false
	}
	if s, ok := value.(atom.String); ok {
		return string(s), true
	}
	return "", false
}

func metaInt(meta *coll.Map, key string) (int, bool) {
	if meta == nil {
		return 0, false
	}
	value := meta.Get(atom.NewKeyword(key))
	if value == nil {
		return 0, false
	}
	num, ok := value.(types.Number)
	if !ok {
		return 0, false
	}
	return int(num.ToFloat64()), true
}

func metaSymbolName(meta *coll.Map, key string) (string, bool) {
	if meta == nil {
		return "", false
	}
	value := meta.Get(atom.NewKeyword(key))
	if value == nil {
		return "", false
	}
	name, ok := types.SymbolName(value)
	return name, ok
}

func metadataQualifiedName(meta *coll.Map) (string, bool) {
	ns, ok := metaSymbolName(meta, "ns")
	if !ok || ns == "" {
		return "", false
	}
	name, ok := metaSymbolName(meta, "name")
	if !ok || name == "" {
		return "", false
	}
	return compiler.QualifySymbol(ns, name), true
}

func (r *Runner) lookupSourceContent(file string) (string, bool) {
	return r.runtimeSources().lookupContent(file)
}

func extractDefinitionForm(source string, targetLine int, targetName string) (string, bool) {
	reader, err := frontend.NewReader("<definition>", source)
	if err != nil {
		return "", false
	}
	var exactByLine types.Value
	var bestByLine types.Value
	bestLine := 0
	var bestByName types.Value
	var bestByNameOnLine types.Value

	for {
		form, parseErr := reader.ReadNext()
		if parseErr != nil {
			return "", false
		}
		if form == nil {
			break
		}
		walkSourceForms(form, func(list *coll.List) {
			if targetLine > 0 {
				if list.Line == targetLine && exactByLine == nil {
					exactByLine = list
				}
				if list.Line > 0 && list.Line <= targetLine && list.Line >= bestLine {
					bestByLine = list
					bestLine = list.Line
				}
			}

			if targetName != "" && listDefinesName(list, targetName) {
				bestByName = list
				if targetLine > 0 && list.Line == targetLine {
					bestByNameOnLine = list
				}
			}
		})
	}

	if bestByNameOnLine != nil {
		return bestByNameOnLine.String(), true
	}
	if bestByName != nil {
		return bestByName.String(), true
	}
	if targetName != "" {
		return "", false
	}
	if exactByLine != nil {
		return exactByLine.String(), true
	}
	if bestByLine != nil {
		return bestByLine.String(), true
	}

	return "", false
}

func listDefinesName(list *coll.List, targetName string) bool {
	if list == nil || len(list.Items) < 2 {
		return false
	}

	head, ok := types.SymbolName(list.Items[0])
	if !ok {
		return false
	}

	switch head {
	case "def", "defn", "defmacro", "defmulti", "defmethod", "defrecord", "deftype":
	default:
		return false
	}

	name, ok := types.SymbolName(list.Items[1])
	if !ok {
		return false
	}
	return name == targetName
}

func walkSourceForms(value types.Value, visit func(*coll.List)) {
	list, ok := value.(*coll.List)
	if !ok || list == nil {
		return
	}

	visit(list)
	if !isTopLevelDoForm(list) {
		return
	}

	for _, item := range list.Items[1:] {
		walkSourceForms(item, visit)
	}
}

func isTopLevelDoForm(list *coll.List) bool {
	if list == nil || len(list.Items) == 0 {
		return false
	}
	head, ok := types.SymbolName(list.Items[0])
	return ok && head == "do"
}
