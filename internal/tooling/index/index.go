package index

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"doggo/internal/compiler"
	"doggo/internal/project"
	"doggo/internal/syntax/frontend"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/vm"
	"doggo/stdlib"
)

type Position struct {
	Line   int
	Column int
}

type Range struct {
	Start Position
	End   Position
}

type SymbolDef struct {
	Namespace string
	Name      string
	Qualified string
	Kind      string
	URI       string
	Range     Range
}

type HoverInfo struct {
	Qualified string
	Kind      string
	Doc       string
	URI       string
	Range     Range
}

type DocumentSymbol struct {
	Name      string
	Qualified string
	Kind      string
	URI       string
	Range     Range
}

type NamespaceInfo struct {
	Name     string
	URI      string
	Aliases  map[string]string
	Refers   map[string]string
	ReferAll []string
	Exclude  map[string]struct{}
}

type WorkspaceIndex struct {
	Symbols         map[string]SymbolDef
	ByNamespace     map[string]map[string]SymbolDef
	Namespaces      map[string]*NamespaceInfo
	NamespaceByURI  map[string]string
	SymbolsByURI    map[string][]string
	DocByQualified  map[string]string
	KindByQualified map[string]string
	DocumentByURI   map[string][]DocumentSymbol
	BuiltinSymbols  map[string]struct{}
	ProjectRootPath string

	sourcesByURI    map[string]string
	namespaceByURI  map[string][]namespaceContext
	emittedByURI    map[string]map[string]emittedBinding
	generationByURI map[string]generationDocument
}

// emittedBinding records collision-only names without making generated or type
// declarations available as rename targets. Protected type names cannot be referred.
type emittedBinding struct {
	protected bool
}

type generationDocument struct {
	source *project.GenerationSource
	name   string
}

type namespaceContext struct {
	start Position
	info  *NamespaceInfo
}

type BuildOptions struct {
	ProjectRoot       string
	Overlay           map[string]string
	GenerationSources []*project.GenerationSource
}

func BuildWorkspaceIndex(opts BuildOptions) (*WorkspaceIndex, error) {
	projectRoot := strings.TrimSpace(opts.ProjectRoot)
	if projectRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		projectRoot = cwd
	}
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	absRoot = filepath.Clean(absRoot)

	idx := &WorkspaceIndex{
		Symbols:         make(map[string]SymbolDef),
		ByNamespace:     make(map[string]map[string]SymbolDef),
		Namespaces:      make(map[string]*NamespaceInfo),
		NamespaceByURI:  make(map[string]string),
		SymbolsByURI:    make(map[string][]string),
		DocByQualified:  make(map[string]string),
		KindByQualified: make(map[string]string),
		DocumentByURI:   make(map[string][]DocumentSymbol),
		BuiltinSymbols:  make(map[string]struct{}),
		ProjectRootPath: absRoot,
		sourcesByURI:    make(map[string]string),
		namespaceByURI:  make(map[string][]namespaceContext),
		emittedByURI:    make(map[string]map[string]emittedBinding),
		generationByURI: make(map[string]generationDocument),
	}

	for _, b := range vm.DefaultBuiltinCatalog().Definitions() {
		if b.Name == "" {
			continue
		}
		idx.BuiltinSymbols[b.Name] = struct{}{}
	}

	overlay := normalizeOverlay(opts.Overlay)
	fileSet, err := collectWorkspaceFileSet(absRoot)
	if err != nil {
		return nil, err
	}
	for path := range overlay {
		if filepath.Ext(path) != ".dog" {
			continue
		}
		fileSet[path] = struct{}{}
	}

	sortedPaths := make([]string, 0, len(fileSet))
	for path := range fileSet {
		sortedPaths = append(sortedPaths, path)
	}
	sort.Strings(sortedPaths)

	for _, path := range sortedPaths {
		content, ok := overlay[path]
		if !ok {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			content = string(data)
		}

		uri := PathToFileURI(path)
		if err := idx.IndexDocument(uri, content); err != nil {
			return nil, err
		}
	}
	for _, source := range opts.GenerationSources {
		files, err := source.Files()
		if err != nil {
			return nil, err
		}
		for _, name := range files {
			if filepath.Ext(name) != ".dog" {
				continue
			}
			content, err := source.ReadFile(name)
			if err != nil {
				return nil, err
			}
			filePath, err := source.FilePath(name)
			if err != nil {
				return nil, err
			}
			uri := PathToFileURI(filePath)
			if err := idx.IndexDocument(uri, string(content)); err != nil {
				return nil, err
			}
			idx.generationByURI[uri] = generationDocument{source: source, name: name}
		}
	}

	if err := idx.indexStdlib(); err != nil {
		return nil, err
	}
	return idx, nil
}

func (i *WorkspaceIndex) IndexDocument(uri, source string) error {
	if strings.TrimSpace(uri) == "" {
		return fmt.Errorf("uri is required")
	}
	uri = NormalizeURI(uri)
	i.removeURI(uri)
	i.sourcesByURI[uri] = source
	doc := parseIndexedDocument(uri, source)
	i.addDocument(uri, doc)
	return nil
}

func (i *WorkspaceIndex) RemoveDocument(uri string) error {
	if strings.TrimSpace(uri) == "" {
		return fmt.Errorf("uri is required")
	}
	uri = NormalizeURI(uri)
	i.removeURI(uri)
	delete(i.sourcesByURI, uri)
	return nil
}

func (i *WorkspaceIndex) DocumentSymbols(uri string) []DocumentSymbol {
	uri = NormalizeURI(uri)
	syms := i.DocumentByURI[uri]
	out := make([]DocumentSymbol, len(syms))
	copy(out, syms)
	return out
}

func (i *WorkspaceIndex) WorkspaceSymbols(query string) []SymbolDef {
	needle := strings.ToLower(strings.TrimSpace(query))
	out := make([]SymbolDef, 0, len(i.Symbols))
	for _, def := range i.Symbols {
		if needle != "" && !matchesWorkspaceQuery(def, needle) {
			continue
		}
		out = append(out, def)
	}
	sort.Slice(out, func(a, b int) bool {
		leftQ := out[a].Qualified
		rightQ := out[b].Qualified
		if leftQ == "" {
			leftQ = compiler.QualifySymbol(out[a].Namespace, out[a].Name)
		}
		if rightQ == "" {
			rightQ = compiler.QualifySymbol(out[b].Namespace, out[b].Name)
		}
		if leftQ != rightQ {
			return leftQ < rightQ
		}
		if out[a].URI != out[b].URI {
			return out[a].URI < out[b].URI
		}
		if out[a].Range.Start.Line != out[b].Range.Start.Line {
			return out[a].Range.Start.Line < out[b].Range.Start.Line
		}
		return out[a].Range.Start.Column < out[b].Range.Start.Column
	})
	return out
}

func (i *WorkspaceIndex) ResolveHover(currentNS, symbol string) (HoverInfo, bool) {
	def, ok := i.ResolveDefinition(currentNS, symbol)
	if ok {
		qualified := def.Qualified
		if qualified == "" {
			qualified = compiler.QualifySymbol(def.Namespace, def.Name)
		}
		kind := def.Kind
		if kind == "" {
			kind = i.KindByQualified[qualified]
		}
		return HoverInfo{
			Qualified: qualified,
			Kind:      kind,
			Doc:       i.DocByQualified[qualified],
			URI:       def.URI,
			Range:     def.Range,
		}, true
	}

	if _, ok := i.BuiltinSymbols[symbol]; ok {
		return HoverInfo{
			Qualified: symbol,
			Kind:      "builtin",
		}, true
	}

	return HoverInfo{}, false
}

func (i *WorkspaceIndex) CompletionCandidates(currentNS, prefix string) []string {
	if nsPrefix, namePrefix, ok := splitQualifiedPrefix(prefix); ok {
		return i.qualifiedCompletionCandidates(currentNS, nsPrefix, namePrefix)
	}

	set := make(map[string]struct{})
	excluded := i.coreExcluded(currentNS)

	add := func(name string) {
		if name == "" {
			return
		}
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			return
		}
		set[name] = struct{}{}
	}

	for _, defs := range i.ByNamespace {
		for name := range defs {
			add(name)
		}
	}

	if nsInfo := i.NamespaceInfo(currentNS); nsInfo != nil {
		for name := range nsInfo.Refers {
			add(name)
		}
		for _, ns := range nsInfo.ReferAll {
			for name := range i.ByNamespace[ns] {
				add(name)
			}
		}
	}

	for name := range i.BuiltinSymbols {
		if _, isExcluded := excluded[name]; isExcluded {
			continue
		}
		add(name)
	}

	out := make([]string, 0, len(set))
	for name := range set {
		if _, blocked := excluded[name]; blocked {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (i *WorkspaceIndex) qualifiedCompletionCandidates(currentNS, nsPrefix, namePrefix string) []string {
	nsInfo := i.NamespaceInfo(currentNS)
	resolvedNS := nsPrefix
	if nsInfo != nil {
		if alias, ok := nsInfo.Aliases[nsPrefix]; ok {
			resolvedNS = alias
		}
	}

	defs := i.ByNamespace[resolvedNS]
	if len(defs) == 0 {
		return []string{}
	}

	set := make(map[string]struct{})
	for name := range defs {
		if namePrefix != "" && !strings.HasPrefix(name, namePrefix) {
			continue
		}
		set[nsPrefix+"/"+name] = struct{}{}
	}

	out := make([]string, 0, len(set))
	for candidate := range set {
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out
}

func (i *WorkspaceIndex) ResolveDefinition(currentNS, symbol string) (SymbolDef, bool) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return SymbolDef{}, false
	}

	nsInfo := i.NamespaceInfo(currentNS)

	if strings.Contains(symbol, "/") {
		nsPart, name := compiler.ParseQualifiedSymbol(symbol)
		if nsInfo != nil {
			if resolvedNS, ok := nsInfo.Aliases[nsPart]; ok {
				nsPart = resolvedNS
			}
		}
		return i.lookup(nsPart, name)
	}

	if def, ok := i.lookup(currentNS, symbol); ok {
		return def, true
	}

	if nsInfo != nil {
		if refNS, ok := nsInfo.Refers[symbol]; ok {
			if def, ok := i.lookup(refNS, symbol); ok {
				return def, true
			}
		}
		for _, refAllNS := range nsInfo.ReferAll {
			if def, ok := i.lookup(refAllNS, symbol); ok {
				return def, true
			}
		}
		if _, excluded := nsInfo.Exclude[symbol]; excluded {
			return SymbolDef{}, false
		}
	}

	if def, ok := i.lookup(compiler.CoreNamespace, symbol); ok {
		return def, true
	}
	if def, ok := i.lookup(compiler.StdlibCoreNamespace, symbol); ok {
		return def, true
	}

	return SymbolDef{}, false
}

// HasGlobalBinding also sees globals emitted by core declaration forms, which
// must prevent rename collisions even when they have no editable SymbolDef.
func (i *WorkspaceIndex) HasGlobalBinding(currentNS, symbol string) bool {
	if _, found := i.ResolveDefinition(currentNS, symbol); found {
		return true
	}
	info := i.NamespaceInfo(currentNS)
	if strings.Contains(symbol, "/") {
		ns, name := compiler.ParseQualifiedSymbol(symbol)
		if info != nil {
			if target, ok := info.Aliases[ns]; ok {
				ns = target
			}
		}
		return i.hasEmittedBinding(ns, name, false)
	}
	if i.hasEmittedBinding(currentNS, symbol, false) {
		return true
	}
	if info != nil {
		if ns, found := info.Refers[symbol]; found && i.hasEmittedBinding(ns, symbol, true) {
			return true
		}
		for _, ns := range info.ReferAll {
			if i.hasEmittedBinding(ns, symbol, true) {
				return true
			}
		}
		if _, excluded := info.Exclude[symbol]; excluded {
			return false
		}
	}
	return i.hasEmittedBinding(compiler.CoreNamespace, symbol, true) ||
		i.hasEmittedBinding(compiler.StdlibCoreNamespace, symbol, true)
}

func (i *WorkspaceIndex) hasEmittedBinding(ns, name string, referred bool) bool {
	qualified := compiler.QualifySymbol(ns, name)
	for _, bindings := range i.emittedByURI {
		if binding, found := bindings[qualified]; found && (!referred || !binding.protected) {
			return true
		}
	}
	return false
}

func (i *WorkspaceIndex) NamespaceForURI(uri string) string {
	uri = NormalizeURI(uri)
	if ns, ok := i.NamespaceByURI[uri]; ok && ns != "" {
		return ns
	}
	return compiler.DefaultNamespace
}

// NamespaceAtPosition returns the namespace active at a 1-based UTF-16 position.
func (i *WorkspaceIndex) NamespaceAtPosition(uri string, position Position) string {
	ns := compiler.DefaultNamespace
	for _, context := range i.namespaceByURI[NormalizeURI(uri)] {
		if context.start.Line > position.Line || (context.start.Line == position.Line && context.start.Column > position.Column) {
			break
		}
		ns = context.info.Name
	}
	return ns
}

// NamespacesForURI includes each namespace that can provide reference spellings
// in the document, including forms preceding its first namespace declaration.
func (i *WorkspaceIndex) NamespacesForURI(uri string) []string {
	names := []string{compiler.DefaultNamespace}
	for _, context := range i.namespaceByURI[NormalizeURI(uri)] {
		if !containsString(names, context.info.Name) {
			names = append(names, context.info.Name)
		}
	}
	return names
}

// IsGenerationDocument identifies immutable retained dependency documents.
func (i *WorkspaceIndex) IsGenerationDocument(uri string) bool {
	_, ok := i.generationByURI[NormalizeURI(uri)]
	return ok
}

// GenerationDocumentSource reads dependency text through its retained snapshot.
func (i *WorkspaceIndex) GenerationDocumentSource(uri string) (string, error, bool) {
	doc, ok := i.generationByURI[NormalizeURI(uri)]
	if !ok {
		return "", nil, false
	}
	content, err := doc.source.ReadFile(doc.name)
	return string(content), err, true
}

// ValidateGenerationSources prevents cached symbols from surviving a corrupt
// or closed retained dependency snapshot.
func (i *WorkspaceIndex) ValidateGenerationSources() error {
	for _, doc := range i.generationByURI {
		if _, err := doc.source.FilePath(doc.name); err != nil {
			return err
		}
		if _, err := doc.source.ReadFile(doc.name); err != nil {
			return err
		}
	}
	return nil
}

func (i *WorkspaceIndex) NamespaceInfo(ns string) *NamespaceInfo {
	if ns == "" {
		ns = compiler.DefaultNamespace
	}
	return i.Namespaces[ns]
}

func (i *WorkspaceIndex) lookup(ns, name string) (SymbolDef, bool) {
	defs, ok := i.ByNamespace[ns]
	if !ok {
		return SymbolDef{}, false
	}
	def, ok := defs[name]
	return def, ok
}

func (i *WorkspaceIndex) coreExcluded(currentNS string) map[string]struct{} {
	out := make(map[string]struct{})
	nsInfo := i.NamespaceInfo(currentNS)
	if nsInfo == nil {
		return out
	}
	for name := range nsInfo.Exclude {
		out[name] = struct{}{}
	}
	return out
}

func matchesWorkspaceQuery(def SymbolDef, needle string) bool {
	qualified := def.Qualified
	if qualified == "" {
		qualified = compiler.QualifySymbol(def.Namespace, def.Name)
	}

	parts := []string{
		def.Name,
		def.Namespace,
		qualified,
		def.Kind,
	}
	for _, part := range parts {
		if strings.Contains(strings.ToLower(part), needle) {
			return true
		}
	}
	return false
}

type indexedDocument struct {
	Namespace string
	Meta      []namespaceContext
	Defs      []SymbolDef
	Symbols   []DocumentSymbol
	Docs      map[string]string
	Emitted   map[string]emittedBinding
}

func parseIndexedDocument(uri, source string) *indexedDocument {
	parsed := frontend.Parse(uri, source)
	if len(parsed.Diagnostics) > 0 {
		return &indexedDocument{}
	}
	forms := parsed.LoweredForms

	doc := &indexedDocument{
		Namespace: compiler.DefaultNamespace,
		Defs:      make([]SymbolDef, 0),
		Symbols:   make([]DocumentSymbol, 0),
		Docs:      make(map[string]string),
		Emitted:   make(map[string]emittedBinding),
	}
	currentNS := compiler.DefaultNamespace
	fileNamespace := ""

	for formIndex, form := range forms {
		list, ok := form.(*coll.List)
		if !ok || len(list.Items) == 0 {
			continue
		}

		head, ok := types.SymbolName(list.Items[0])
		if !ok {
			continue
		}

		switch head {
		case "ns":
			nsName, nsInfo := parseNamespaceDecl(list)
			if nsName == "" {
				continue
			}
			currentNS = nsName
			fileNamespace = nsName
			if nsInfo != nil {
				nsInfo.URI = uri
				doc.Meta = append(doc.Meta, namespaceContext{
					start: PositionFromOffset(source, parsed.Forms[formIndex].StartOffset),
					info:  nsInfo,
				})
			}
		case "def", "defn", "defmacro", "defonce":
			if len(list.Items) < 2 {
				continue
			}
			name, ok := types.SymbolName(list.Items[1])
			if !ok || name == "" {
				continue
			}
			qualified := compiler.QualifySymbol(currentNS, name)
			nameNode := parsed.Forms[formIndex].Items[1]
			def := SymbolDef{
				Namespace: currentNS,
				Name:      name,
				Qualified: qualified,
				Kind:      head,
				URI:       uri,
				Range: Range{
					Start: PositionFromOffset(source, nameNode.StartOffset),
					End:   PositionFromOffset(source, nameNode.EndOffset),
				},
			}
			doc.Defs = append(doc.Defs, def)
			doc.Symbols = append(doc.Symbols, DocumentSymbol{
				Name:      name,
				Qualified: qualified,
				Kind:      head,
				URI:       uri,
				Range:     def.Range,
			})
			if docString := extractDocFromDefinition(list, head); docString != "" {
				doc.Docs[qualified] = docString
			}
		}
	}

	if fileNamespace != "" {
		doc.Namespace = fileNamespace
	}
	emittedNS := compiler.DefaultNamespace
	for _, form := range forms {
		emittedNS = collectEmittedBindings(form, emittedNS, doc.Emitted)
	}

	return doc
}

// Only explicit declaration forms and do grouping contribute bindings. In
// particular quote, metadata data, fields, and method parameters are not walked.
func collectEmittedBindings(form types.Value, currentNS string, bindings map[string]emittedBinding) string {
	list, ok := form.(*coll.List)
	if !ok || len(list.Items) == 0 {
		return currentNS
	}
	head, _ := types.SymbolName(list.Items[0])
	switch head {
	case "ns":
		if ns, _ := parseNamespaceDecl(list); ns != "" {
			return ns
		}
	case "do":
		for _, item := range list.Items[1:] {
			currentNS = collectEmittedBindings(item, currentNS, bindings)
		}
	case "defrecord", "deftype", "defprotocol":
		if len(list.Items) < 2 || (head != "defprotocol" && len(list.Items) < 3) {
			return currentNS
		}
		name, ok := types.SymbolName(list.Items[1])
		if !ok || name == "" {
			return currentNS
		}
		add := func(name string, protected bool) {
			bindings[compiler.QualifySymbol(currentNS, name)] = emittedBinding{protected: protected}
		}
		if head == "defprotocol" {
			add(name, false)
			for _, item := range list.Items[2:] {
				if method, ok := item.(*coll.List); ok && len(method.Items) > 0 {
					if name, ok := types.SymbolName(method.Items[0]); ok && name != "" {
						add(name, false)
					}
				}
			}
		} else {
			add(name, true)
			add("->"+name, false)
			if head == "defrecord" {
				add("map->"+name, false)
			}
		}
	}
	return currentNS
}

func (i *WorkspaceIndex) addDocument(uri string, doc *indexedDocument) {
	if doc == nil {
		return
	}

	if doc.Namespace != "" {
		i.NamespaceByURI[uri] = doc.Namespace
	}
	i.emittedByURI[uri] = doc.Emitted
	for _, context := range doc.Meta {
		i.namespaceByURI[uri] = append(i.namespaceByURI[uri], namespaceContext{
			start: context.start,
			info:  cloneNamespaceInfo(context.info),
		})
	}
	if len(doc.Symbols) > 0 {
		i.DocumentByURI[uri] = append([]DocumentSymbol(nil), doc.Symbols...)
	}

	if len(doc.Defs) == 0 {
		i.rebuildNamespaces()
		return
	}

	qualified := make([]string, 0, len(doc.Defs))
	for _, def := range doc.Defs {
		i.Symbols[def.Qualified] = def
		i.KindByQualified[def.Qualified] = def.Kind

		if docString := doc.Docs[def.Qualified]; docString != "" {
			i.DocByQualified[def.Qualified] = docString
		}

		nsDefs := i.ByNamespace[def.Namespace]
		if nsDefs == nil {
			nsDefs = make(map[string]SymbolDef)
			i.ByNamespace[def.Namespace] = nsDefs
		}
		nsDefs[def.Name] = def
		qualified = append(qualified, def.Qualified)
	}
	i.SymbolsByURI[uri] = qualified
	i.rebuildNamespaces()
}

func (i *WorkspaceIndex) removeURI(uri string) {
	delete(i.NamespaceByURI, uri)
	delete(i.namespaceByURI, uri)
	delete(i.emittedByURI, uri)
	delete(i.DocumentByURI, uri)

	if symbols, ok := i.SymbolsByURI[uri]; ok {
		for _, qualified := range symbols {
			def, exists := i.Symbols[qualified]
			delete(i.Symbols, qualified)
			delete(i.DocByQualified, qualified)
			delete(i.KindByQualified, qualified)
			if exists {
				nsDefs := i.ByNamespace[def.Namespace]
				delete(nsDefs, def.Name)
				if len(nsDefs) == 0 {
					delete(i.ByNamespace, def.Namespace)
				}
			}
		}
		delete(i.SymbolsByURI, uri)
	}

	i.rebuildNamespaces()
}

func (i *WorkspaceIndex) rebuildNamespaces() {
	i.Namespaces = make(map[string]*NamespaceInfo)
	uris := make([]string, 0, len(i.namespaceByURI))
	for uri := range i.namespaceByURI {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	for _, uri := range uris {
		for _, context := range i.namespaceByURI[uri] {
			meta := context.info
			if meta == nil || meta.Name == "" {
				continue
			}
			dst := i.getOrCreateNamespace(meta.Name)
			dst.URI = uri
			mergeNamespaceInfo(dst, meta)
		}
	}
}

func (i *WorkspaceIndex) indexStdlib() error {
	entries, err := fs.ReadDir(stdlib.StdlibFS, "doggo")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".dog") {
			continue
		}
		path := filepath.ToSlash(filepath.Join("doggo", entry.Name()))
		data, err := fs.ReadFile(stdlib.StdlibFS, path)
		if err != nil {
			continue
		}
		uri := "stdlib://" + path
		if err := i.IndexDocument(uri, string(data)); err != nil {
			return err
		}
	}
	return nil
}

func (i *WorkspaceIndex) getOrCreateNamespace(ns string) *NamespaceInfo {
	info := i.Namespaces[ns]
	if info != nil {
		return info
	}
	info = &NamespaceInfo{
		Name:     ns,
		Aliases:  make(map[string]string),
		Refers:   make(map[string]string),
		ReferAll: []string{},
		Exclude:  make(map[string]struct{}),
	}
	i.Namespaces[ns] = info
	return info
}

func mergeNamespaceInfo(dst, src *NamespaceInfo) {
	if dst == nil || src == nil {
		return
	}
	for alias, ns := range src.Aliases {
		dst.Aliases[alias] = ns
	}
	for name, ns := range src.Refers {
		dst.Refers[name] = ns
	}
	for _, ns := range src.ReferAll {
		if !containsString(dst.ReferAll, ns) {
			dst.ReferAll = append(dst.ReferAll, ns)
		}
	}
	for name := range src.Exclude {
		dst.Exclude[name] = struct{}{}
	}
}

func cloneNamespaceInfo(src *NamespaceInfo) *NamespaceInfo {
	if src == nil {
		return nil
	}
	out := &NamespaceInfo{
		Name:     src.Name,
		URI:      src.URI,
		Aliases:  make(map[string]string, len(src.Aliases)),
		Refers:   make(map[string]string, len(src.Refers)),
		ReferAll: make([]string, 0, len(src.ReferAll)),
		Exclude:  make(map[string]struct{}, len(src.Exclude)),
	}
	for alias, ns := range src.Aliases {
		out.Aliases[alias] = ns
	}
	for name, ns := range src.Refers {
		out.Refers[name] = ns
	}
	out.ReferAll = append(out.ReferAll, src.ReferAll...)
	for name := range src.Exclude {
		out.Exclude[name] = struct{}{}
	}
	return out
}

func splitQualifiedPrefix(prefix string) (string, string, bool) {
	if !strings.Contains(prefix, "/") {
		return "", "", false
	}
	nsPart, namePart := compiler.ParseQualifiedSymbol(prefix)
	if nsPart == "" {
		return "", "", false
	}
	return nsPart, namePart, true
}

func extractDocFromDefinition(list *coll.List, kind string) string {
	if len(list.Items) < 2 {
		return ""
	}

	if doc := extractDocFromSymbolMeta(list.Items[1]); doc != "" {
		return doc
	}

	switch kind {
	case "def", "defonce":
		if len(list.Items) >= 3 {
			if doc, ok := list.Items[2].(atom.String); ok {
				return string(doc)
			}
		}
	case "defn", "defmacro":
		idx := 2
		if idx < len(list.Items) {
			if doc, ok := list.Items[idx].(atom.String); ok {
				return string(doc)
			}
			if attrs, ok := list.Items[idx].(*coll.Map); ok {
				if doc := extractDocFromMap(attrs); doc != "" {
					return doc
				}
			}
		}
		if idx+1 < len(list.Items) {
			if attrs, ok := list.Items[idx+1].(*coll.Map); ok {
				if doc := extractDocFromMap(attrs); doc != "" {
					return doc
				}
			}
		}
	}

	return ""
}

func extractDocFromSymbolMeta(v types.Value) string {
	symWithMeta, ok := v.(*types.SymbolWithMeta)
	if !ok {
		return ""
	}
	meta, ok := symWithMeta.Meta.(*coll.Map)
	if !ok || meta == nil {
		return ""
	}
	return extractDocFromMap(meta)
}

func extractDocFromMap(m *coll.Map) string {
	if m == nil {
		return ""
	}
	val := m.Get(atom.NewKeyword("doc"))
	doc, ok := val.(atom.String)
	if !ok {
		return ""
	}
	return string(doc)
}

func parseNamespaceDecl(list *coll.List) (string, *NamespaceInfo) {
	if len(list.Items) < 2 {
		return "", nil
	}
	nsName, ok := types.SymbolName(list.Items[1])
	if !ok || nsName == "" {
		return "", nil
	}

	info := &NamespaceInfo{
		Name:     nsName,
		Aliases:  make(map[string]string),
		Refers:   make(map[string]string),
		ReferAll: []string{},
		Exclude:  make(map[string]struct{}),
	}

	idx := 2
	if idx < len(list.Items) {
		if _, ok := list.Items[idx].(atom.String); ok {
			idx++
		}
	}
	if idx < len(list.Items) {
		if _, ok := list.Items[idx].(*coll.Map); ok {
			idx++
		}
	}

	for ; idx < len(list.Items); idx++ {
		clause, ok := list.Items[idx].(*coll.List)
		if !ok || len(clause.Items) == 0 {
			continue
		}

		head, ok := clause.Items[0].(atom.Keyword)
		if !ok {
			continue
		}

		switch string(head) {
		case "require":
			parseRequireClause(info, clause)
		case "refer-clojure":
			parseReferClojureClause(info, clause)
		}
	}

	return nsName, info
}

func parseRequireClause(info *NamespaceInfo, clause *coll.List) {
	for i := 1; i < len(clause.Items); i++ {
		parseRequireSpec(info, clause.Items[i])
	}
}

func parseRequireSpec(info *NamespaceInfo, spec types.Value) {
	if _, ok := symbolFromValue(spec); ok {
		return
	}

	vec, ok := spec.(*coll.Vector)
	if !ok || len(vec.Items) == 0 {
		return
	}
	targetNS, ok := symbolFromValue(vec.Items[0])
	if !ok || targetNS == "" {
		return
	}

	for i := 1; i < len(vec.Items); i++ {
		kw, ok := vec.Items[i].(atom.Keyword)
		if !ok {
			continue
		}
		switch string(kw) {
		case "as":
			if i+1 >= len(vec.Items) {
				continue
			}
			alias, ok := symbolFromValue(vec.Items[i+1])
			if ok && alias != "" {
				info.Aliases[alias] = targetNS
			}
			i++
		case "refer":
			if i+1 >= len(vec.Items) {
				continue
			}
			parseReferOption(info, targetNS, vec.Items[i+1])
			i++
		}
	}
}

func parseReferOption(info *NamespaceInfo, targetNS string, v types.Value) {
	if kw, ok := v.(atom.Keyword); ok && string(kw) == "all" {
		if !containsString(info.ReferAll, targetNS) {
			info.ReferAll = append(info.ReferAll, targetNS)
		}
		return
	}
	vec, ok := v.(*coll.Vector)
	if !ok {
		return
	}
	for _, item := range vec.Items {
		name, ok := symbolFromValue(item)
		if !ok || name == "" {
			continue
		}
		info.Refers[name] = targetNS
	}
}

func parseReferClojureClause(info *NamespaceInfo, clause *coll.List) {
	for i := 1; i+1 < len(clause.Items); i += 2 {
		kw, ok := clause.Items[i].(atom.Keyword)
		if !ok {
			continue
		}
		if string(kw) != "exclude" {
			continue
		}
		vec, ok := clause.Items[i+1].(*coll.Vector)
		if !ok {
			continue
		}
		for _, item := range vec.Items {
			name, ok := symbolFromValue(item)
			if !ok || name == "" {
				continue
			}
			info.Exclude[name] = struct{}{}
		}
	}
}

func symbolFromValue(v types.Value) (string, bool) {
	if v == nil {
		return "", false
	}
	switch x := v.(type) {
	case types.Symbol:
		return string(x), true
	case *types.SymbolWithMeta:
		return x.Name, true
	case *coll.List:
		if len(x.Items) == 2 {
			if head, ok := types.SymbolName(x.Items[0]); ok && head == "quote" {
				return symbolFromValue(x.Items[1])
			}
		}
	}
	return "", false
}

func normalizeOverlay(overlay map[string]string) map[string]string {
	out := make(map[string]string, len(overlay))
	for path, source := range overlay {
		if strings.TrimSpace(path) == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		out[filepath.Clean(abs)] = source
	}
	return out
}

// CollectWorkspaceFiles returns sorted absolute .dog files for a project.
func CollectWorkspaceFiles(projectRoot string) ([]string, error) {
	set, err := collectWorkspaceFileSet(projectRoot)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set))
	for path := range set {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func collectWorkspaceFileSet(projectRoot string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if err := collectDogFiles(projectRoot, map[string]struct{}{
		".git":   {},
		".doggo": {},
	}, out); err != nil {
		return nil, err
	}

	return out, nil
}

func collectDogFiles(root string, excludes map[string]struct{}, out map[string]struct{}) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := excludes[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".dog" {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		out[filepath.Clean(abs)] = struct{}{}
		return nil
	})
}

func PathToFileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	normalized := filepath.ToSlash(filepath.Clean(abs))
	if strings.HasPrefix(normalized, "//") {
		host, tail, _ := strings.Cut(strings.TrimPrefix(normalized, "//"), "/")
		return (&url.URL{Scheme: "file", Host: host, Path: "/" + tail}).String()
	}
	if runtime.GOOS == "windows" && len(normalized) >= 2 && normalized[1] == ':' {
		normalized = "/" + strings.ToUpper(normalized[:1]) + normalized[1:]
	}
	u := &url.URL{Scheme: "file", Path: normalized}
	return u.String()
}

func FileURIToPath(rawURI string) (string, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported URI scheme: %s", u.Scheme)
	}
	path := u.Path
	if path == "" {
		return "", fmt.Errorf("empty file URI path")
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		path = "//" + u.Host + path
	} else if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.Clean(filepath.FromSlash(path)), nil
}

// NormalizeURI gives file-backed documents the same identity as disk paths.
func NormalizeURI(uri string) string {
	path, err := FileURIToPath(uri)
	if err != nil {
		return uri
	}
	return PathToFileURI(path)
}

// PositionFromOffset converts a source byte offset to a 1-based UTF-16 position.
func PositionFromOffset(source string, offset int) Position {
	offset = min(max(offset, 0), len(source))
	prefix := source[:offset]
	line := strings.Count(prefix, "\n") + 1
	lineStart := strings.LastIndexByte(prefix, '\n') + 1
	column := 1
	for _, r := range prefix[lineStart:] {
		column++
		if r > 0xFFFF {
			column++
		}
	}
	return Position{Line: line, Column: column}
}

func containsString(xs []string, x string) bool {
	for _, item := range xs {
		if item == x {
			return true
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
