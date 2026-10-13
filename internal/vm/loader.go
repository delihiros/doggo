package vm

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"

	"doggo/internal/project"
	"doggo/internal/types"
	"doggo/internal/vm/engine"
	"doggo/stdlib"
)

const stdlibNamespacePathPrefix = "doggo/"

// CodeExecutor はコードを実行するためのインターフェースです。
// Loader がファイルをロードした後、コードを実行するために使用します。
type CodeExecutor interface {
	RunWithOrigin(source string, origin engine.SourceOrigin) (types.Value, error)
}

type trustedCodeExecutor interface {
	runWithOriginTrusted(source string, origin engine.SourceOrigin) (types.Value, error)
}

type sourceOriginResolver interface {
	resolveSourceOrigin(file string) (engine.SourceOrigin, bool)
}

// NamespaceAwareExecutor は名前空間の取得・設定が可能な CodeExecutor です。
// require 時に名前空間を保存・復元するために使用します。
type NamespaceAwareExecutor interface {
	CodeExecutor
	CurrentNamespace() string
	SetCurrentNamespace(ns string)
}

// Loader はライブラリファイルのロードを管理します。
type Loader struct {
	loadMu            sync.Mutex // compiler state is shared; source execution runs outside this lock
	stateMu           sync.Mutex // loaded/loading state
	loadPaths         []string   // ライブラリ検索パス
	generationSources []*project.GenerationSource
	loadedLibs        map[string]bool // ロード済みライブラリの名前空間名
	loadingLibs       map[string]*libraryLoad
	waitingLibs       map[string]map[string]int // 読み込み中namespace間の待機辺
	executor          CodeExecutor              // コード実行用
	projectRoot       string
}

type libraryLoad struct {
	done chan struct{}
	err  error
}

func (l *Loader) enter(ctx context.Context) (context.Context, func()) {
	ctx = withBindingState(ctx)
	state := bindingStateFromContext(ctx)
	state.mu.RLock()
	owned := state.loaderOwner == l
	state.mu.RUnlock()
	if owned {
		return ctx, func() {}
	}

	l.loadMu.Lock()
	state.mu.Lock()
	state.loaderOwner = l
	state.mu.Unlock()
	return ctx, func() {
		state.mu.Lock()
		state.loaderOwner = nil
		state.mu.Unlock()
		l.loadMu.Unlock()
	}
}

// NewLoader は新しい Loader を作成します。
func NewLoader(projectRoot string, loadPaths []string, generationSources []*project.GenerationSource, executor CodeExecutor) *Loader {
	loader := &Loader{
		loadedLibs:        make(map[string]bool),
		loadingLibs:       make(map[string]*libraryLoad),
		waitingLibs:       make(map[string]map[string]int),
		executor:          executor,
		projectRoot:       resolveProjectRootBestEffort(projectRoot),
		generationSources: append([]*project.GenerationSource(nil), generationSources...),
	}
	loader.loadPaths = loader.normalizeLoadPaths(loadPaths)
	return loader
}

func resolveProjectRootBestEffort(projectRoot string) string {
	trimmed := strings.TrimSpace(projectRoot)
	if trimmed == "" {
		return ""
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return trimmed
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved
	}
	return abs
}

// LoadFile はファイルを読み込み、現在の環境で実行します。
// 相対パスは project root を基準に解決します。
func (l *Loader) LoadFile(path string) (types.Value, error) {
	ctx := context.Background()
	if runner, ok := l.executor.(*Runner); ok {
		ctx = runner.contextForExecution()
	}
	return l.loadFile(ctx, path, engine.SourceOrigin{})
}

// LoadFileFromContext は現在実行中のソース情報を考慮してファイルを読み込みます。
// 相対パスは現在のフレームがファイル由来ならそのディレクトリを基準にし、
// そうでなければ project root を基準にします。
func (l *Loader) LoadFileFromContext(ctx context.Context, path string) (types.Value, error) {
	return l.loadFile(ctx, path, l.loadFileOriginFromContext(ctx))
}

func (l *Loader) loadFile(ctx context.Context, path string, origin engine.SourceOrigin) (types.Value, error) {
	ctx = withBindingState(ctx)
	requested := filepath.ToSlash(strings.TrimSpace(path))
	isRelative := !filepath.IsAbs(filepath.FromSlash(requested))
	if origin.GenerationSource != nil && isRelative {
		relativePath := pathpkg.Join(pathpkg.Dir(origin.RelativePath), requested)
		content, err := origin.GenerationSource.ReadFile(relativePath)
		if err != nil {
			return nil, fmt.Errorf("load-file: %w", err)
		}
		fileOrigin := engine.SourceOrigin{
			File:             origin.GenerationSource.DisplayPath(relativePath),
			GenerationSource: origin.GenerationSource,
			RelativePath:     relativePath,
		}
		return l.execute(ctx, string(content), fileOrigin, false)
	}
	if isRelative && strings.HasPrefix(strings.TrimSpace(origin.File), "generation://") {
		return nil, fmt.Errorf("load-file: unresolved generation origin %q", origin.File)
	}

	canonicalPath := l.resolveLoadFilePath(path, origin)

	// ファイルを読み込む
	content, err := os.ReadFile(canonicalPath)
	if err != nil {
		return nil, fmt.Errorf("load-file: %w", err)
	}

	fileOrigin := engine.SourceOrigin{File: l.displayPath(canonicalPath)}
	return l.execute(ctx, string(content), fileOrigin, l.isTrustedProjectStdlibImplementationPath(canonicalPath))
}

// Require は名前空間名からファイルを検索してロードします。
// 既にロード済みの場合は何もしません。
// ロード後は呼び出し元の名前空間を復元します（最小の驚き原則）。
//
// 検索順序:
// 1. 非 `doggo.*`: 設定済み loadPaths の順
// 2. `doggo.*`: project stdlib ディスク (`<projectRoot>/stdlib/doggo/*.dog`)
// 3. `doggo.*`: 埋め込み stdlib（stdlib.StdlibFS）
func (l *Loader) Require(nsName string) error {
	ctx := context.Background()
	if runner, ok := l.executor.(*Runner); ok {
		ctx = runner.contextForExecution()
	}
	return l.RequireFromContext(ctx, nsName)
}

func (l *Loader) RequireFromContext(ctx context.Context, nsName string) error {
	ctx = withBindingState(ctx)

	l.stateMu.Lock()
	if l.loadedLibs[nsName] {
		l.stateMu.Unlock()
		return nil
	}
	if contextLoadsLibrary(ctx, l, nsName) {
		l.stateMu.Unlock()
		return fmt.Errorf("require: circular dependency detected for '%s'", nsName)
	}
	if loading := l.loadingLibs[nsName]; loading != nil {
		sources := l.activeContextLoads(ctx)
		for _, source := range sources {
			if l.waitPathExists(nsName, source, nil) {
				l.stateMu.Unlock()
				return fmt.Errorf("require: circular dependency detected between '%s' and '%s'", source, nsName)
			}
		}
		l.addWaitEdges(sources, nsName)
		l.stateMu.Unlock()
		var err error
		select {
		case <-loading.done:
			err = loading.err
		case <-ctx.Done():
			err = ctx.Err()
		}
		l.stateMu.Lock()
		l.removeWaitEdges(sources, nsName)
		l.stateMu.Unlock()
		return err
	}
	loading := &libraryLoad{done: make(chan struct{})}
	l.loadingLibs[nsName] = loading
	l.stateMu.Unlock()

	pushContextLoadingLibrary(ctx, l, nsName)
	err := l.require(ctx, nsName)
	popContextLoadingLibrary(ctx, l, nsName)

	l.stateMu.Lock()
	if err == nil {
		l.loadedLibs[nsName] = true
	}
	loading.err = err
	delete(l.loadingLibs, nsName)
	close(loading.done)
	l.stateMu.Unlock()
	return err
}

// The following helpers are called with stateMu held.
func (l *Loader) activeContextLoads(ctx context.Context) []string {
	owned := contextLoadingLibraries(ctx, l)
	active := owned[:0]
	for _, library := range owned {
		if l.loadingLibs[library] != nil {
			active = append(active, library)
		}
	}
	return active
}

func (l *Loader) waitPathExists(from, target string, seen map[string]bool) bool {
	if from == target {
		return true
	}
	if seen == nil {
		seen = make(map[string]bool)
	}
	if seen[from] {
		return false
	}
	seen[from] = true
	for next := range l.waitingLibs[from] {
		if l.waitPathExists(next, target, seen) {
			return true
		}
	}
	return false
}

func (l *Loader) addWaitEdges(sources []string, target string) {
	for _, source := range sources {
		if l.waitingLibs[source] == nil {
			l.waitingLibs[source] = make(map[string]int)
		}
		l.waitingLibs[source][target]++
	}
}

func (l *Loader) removeWaitEdges(sources []string, target string) {
	for _, source := range sources {
		remaining := l.waitingLibs[source][target] - 1
		if remaining > 0 {
			l.waitingLibs[source][target] = remaining
			continue
		}
		delete(l.waitingLibs[source], target)
		if len(l.waitingLibs[source]) == 0 {
			delete(l.waitingLibs, source)
		}
	}
}

func (l *Loader) require(ctx context.Context, nsName string) error {
	// 名前空間を保存（executor が対応している場合）
	var savedNs string
	nsAware, hasNsSupport := l.executor.(NamespaceAwareExecutor)
	if hasNsSupport {
		savedNs = l.contextNamespace(ctx, nsAware)
		defer l.restoreNamespace(ctx, nsAware, savedNs)
	}

	// 名前空間名をファイルパスに変換 (foo.bar -> foo/bar.dog)
	filePath := l.namespaceToPath(nsName)

	// ソースコードを取得（ディスク優先 → 埋め込みフォールバック）
	resolved, err := l.resolveSource(filePath)
	if err != nil {
		return fmt.Errorf("require: could not find library '%s' (searched for '%s'): %w", nsName, filePath, err)
	}

	// ソースコードを実行
	_, err = l.execute(ctx, resolved.Content, resolved.Origin, isStdlibNamespacePath(filePath))
	if err != nil {
		return fmt.Errorf("require: error loading '%s': %w", nsName, err)
	}

	return nil
}

func (l *Loader) contextNamespace(ctx context.Context, nsAware NamespaceAwareExecutor) string {
	if namespace := namespaceFromContext(ctx); namespace != "" {
		return namespace
	}
	ctx, leave := l.enter(ctx)
	defer leave()
	namespace := nsAware.CurrentNamespace()
	setNamespaceInContext(ctx, namespace)
	return namespace
}

func (l *Loader) restoreNamespace(ctx context.Context, nsAware NamespaceAwareExecutor, namespace string) {
	ctx, leave := l.enter(ctx)
	defer leave()
	setNamespaceInContext(ctx, namespace)
	if runner, ok := nsAware.(*Runner); ok {
		runner.setCurrentNamespaceState(namespace)
		return
	}
	nsAware.SetCurrentNamespace(namespace)
}

func (l *Loader) execute(ctx context.Context, source string, origin engine.SourceOrigin, trusted bool) (types.Value, error) {
	if runner, ok := l.executor.(*Runner); ok {
		return runner.runSource(source, origin, trusted, ctx)
	}
	if trusted {
		if trustedExecutor, ok := l.executor.(trustedCodeExecutor); ok {
			return trustedExecutor.runWithOriginTrusted(source, origin)
		}
	}
	return l.executor.RunWithOrigin(source, origin)
}

// resolveSource はファイルパスからソースコードを解決します。
// `doggo.*` namespace は予約済みとみなし、project stdlib と埋め込み stdlib からのみ解決します。
type resolvedSource struct {
	Content string
	Origin  engine.SourceOrigin
}

func (l *Loader) resolveSource(relativePath string) (*resolvedSource, error) {
	if isStdlibNamespacePath(relativePath) {
		if fullPath, found := l.findProjectStdlibFile(relativePath); found {
			return l.readResolvedFile(fullPath)
		}
		return l.readEmbeddedStdlib(relativePath)
	}

	if fullPath, found := l.findLoadPathFile(relativePath); found {
		return l.readResolvedFile(fullPath)
	}
	canonical := pathpkg.Clean(filepath.ToSlash(relativePath))
	for _, source := range l.generationSources {
		if source == nil {
			continue
		}
		// Match the live project's root-then-src layout within each retained source.
		for _, candidate := range []string{canonical, pathpkg.Join("src", canonical)} {
			content, err := source.ReadFile(candidate)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			return &resolvedSource{
				Content: string(content),
				Origin: engine.SourceOrigin{
					File:             source.DisplayPath(candidate),
					GenerationSource: source,
					RelativePath:     candidate,
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("not found")
}

func (l *Loader) readResolvedFile(fullPath string) (*resolvedSource, error) {
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file '%s': %w", fullPath, err)
	}
	return &resolvedSource{
		Content: string(content),
		Origin:  engine.SourceOrigin{File: l.displayPath(fullPath)},
	}, nil
}

func (l *Loader) readEmbeddedStdlib(relativePath string) (*resolvedSource, error) {
	// embed.FS のパスは常にスラッシュ区切り
	embedPath := strings.ReplaceAll(relativePath, string(os.PathSeparator), "/")
	content, err := fs.ReadFile(stdlib.StdlibFS, embedPath)
	if err != nil {
		return nil, fmt.Errorf("not found in project or embedded stdlib")
	}
	return &resolvedSource{
		Content: string(content),
		Origin:  engine.SourceOrigin{File: "stdlib://" + embedPath},
	}, nil
}

// namespaceToPath は名前空間名をファイルパスに変換します。
// foo.bar -> foo/bar.dog
func (l *Loader) namespaceToPath(nsName string) string {
	// . を / に置換
	path := strings.ReplaceAll(nsName, ".", string(os.PathSeparator))
	return path + ".dog"
}

func isStdlibNamespacePath(relativePath string) bool {
	return strings.HasPrefix(filepath.ToSlash(relativePath), stdlibNamespacePathPrefix)
}

func (l *Loader) findProjectStdlibFile(relativePath string) (string, bool) {
	if l.projectRoot == "" {
		return "", false
	}
	fullPath := filepath.Join(l.projectRoot, "stdlib", relativePath)
	if _, err := os.Stat(fullPath); err == nil {
		return l.resolvePathBestEffort(fullPath), true
	}
	return "", false
}

// findLoadPathFile はロードパスからファイルを検索します。
func (l *Loader) findLoadPathFile(relativePath string) (string, bool) {
	for _, basePath := range l.GetLoadPaths() {
		fullPath := filepath.Join(basePath, relativePath)
		if _, err := os.Stat(fullPath); err == nil {
			return l.resolvePathBestEffort(fullPath), true
		}
	}
	return "", false
}

func (l *Loader) resolvePathBestEffort(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved
	}
	return abs
}

func (l *Loader) resolveLoadPathBestEffort(path string) string {
	if filepath.IsAbs(path) {
		return l.resolvePathBestEffort(path)
	}
	if l.projectRoot != "" {
		return l.resolvePathBestEffort(filepath.Join(l.projectRoot, path))
	}
	return l.resolvePathBestEffort(path)
}

func (l *Loader) resolveLoadFilePath(path string, origin engine.SourceOrigin) string {
	normalizedPath := filepath.FromSlash(strings.TrimSpace(path))
	if normalizedPath == "" {
		return l.resolvePathBestEffort(path)
	}
	if filepath.IsAbs(normalizedPath) {
		return l.resolvePathBestEffort(normalizedPath)
	}
	if baseDir, ok := l.originBaseDir(origin); ok {
		return l.resolvePathBestEffort(filepath.Join(baseDir, normalizedPath))
	}
	if l.projectRoot != "" {
		return l.resolvePathBestEffort(filepath.Join(l.projectRoot, normalizedPath))
	}
	return l.resolvePathBestEffort(normalizedPath)
}

// ResolveResourcePath resolves a project resource path.
// Search order:
// 1. absolute path as-is
// 2. <projectRoot>/resources/<name>
// 3. <projectRoot>/stdlib/resources/<name>
// 4. <loadPath>/resources/<name>
// 5. <loadPath>/stdlib/resources/<name>
// 6. <projectRoot>/<name>
// 7. <loadPath>/<name>
func (l *Loader) ResolveResourcePath(name string) (string, bool) {
	normalized := filepath.FromSlash(strings.TrimSpace(name))
	if normalized == "" {
		return "", false
	}

	loadPaths := l.GetLoadPaths()
	candidates := make([]string, 0, 2+len(loadPaths)*2)
	if filepath.IsAbs(normalized) {
		candidates = append(candidates, normalized)
	} else {
		if l.projectRoot != "" {
			candidates = append(candidates,
				filepath.Join(l.projectRoot, "resources", normalized),
				filepath.Join(l.projectRoot, "stdlib", "resources", normalized),
				filepath.Join(l.projectRoot, normalized),
			)
		}
		for _, basePath := range loadPaths {
			candidates = append(candidates,
				filepath.Join(basePath, "resources", normalized),
				filepath.Join(basePath, "stdlib", "resources", normalized),
				filepath.Join(basePath, normalized),
			)
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		canonical := l.resolvePathBestEffort(candidate)
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		if info, err := os.Stat(canonical); err == nil && !info.IsDir() {
			return canonical, true
		}
	}
	return "", false
}

func (l *Loader) originBaseDir(origin engine.SourceOrigin) (string, bool) {
	file := strings.TrimSpace(origin.FileOrUnknown())
	switch {
	case file == "":
		return "", false
	case strings.HasPrefix(file, "<"):
		return "", false
	case strings.HasPrefix(file, "stdlib://"):
		return "", false
	}

	if filepath.IsAbs(file) {
		return filepath.Dir(file), true
	}

	diskPath := filepath.FromSlash(file)
	if l.projectRoot != "" {
		return filepath.Dir(filepath.Join(l.projectRoot, diskPath)), true
	}
	return filepath.Dir(l.resolvePathBestEffort(diskPath)), true
}

func (l *Loader) loadFileOriginFromContext(ctx context.Context) engine.SourceOrigin {
	if vm := GetVM(ctx); vm != nil && vm.frameIndex > 0 {
		if frame := vm.currentFrame(); frame != nil {
			if file := strings.TrimSpace(frame.CurrentFile()); file != "" {
				if resolver, ok := l.executor.(sourceOriginResolver); ok {
					if origin, found := resolver.resolveSourceOrigin(file); found {
						return origin
					}
				}
				return engine.SourceOrigin{File: file}
			}
		}
	}
	if origin, ok := sourceOriginFromContext(ctx); ok {
		return origin
	}
	return engine.SourceOrigin{}
}

func (l *Loader) displayPath(canonicalPath string) string {
	if l.projectRoot == "" {
		return filepath.ToSlash(canonicalPath)
	}
	rel, err := filepath.Rel(l.projectRoot, canonicalPath)
	if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(canonicalPath)
}

func (l *Loader) isTrustedProjectStdlibImplementationPath(canonicalPath string) bool {
	if l.projectRoot == "" {
		return false
	}
	canonical := l.resolvePathBestEffort(canonicalPath)
	stdlibRoot := filepath.Join(l.projectRoot, "stdlib", "doggo")
	rel, err := filepath.Rel(stdlibRoot, canonical)
	if err != nil || rel == "." || rel == "" {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if !strings.HasSuffix(filepath.ToSlash(rel), ".dog") {
		return false
	}
	return !strings.HasSuffix(filepath.Base(canonical), "_test.dog")
}

// GetLoadPaths はライブラリ検索パスを返します。
func (l *Loader) GetLoadPaths() []string {
	return append([]string(nil), l.loadPaths...)
}

func (l *Loader) normalizeLoadPaths(paths []string) []string {
	if len(paths) == 0 {
		return []string{}
	}
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, l.resolveLoadPathBestEffort(trimmed))
	}
	return normalized
}
