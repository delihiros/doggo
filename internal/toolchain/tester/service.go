package tester

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	doggoatom "doggo/internal/atom"
	"doggo/internal/project"
	"doggo/internal/runtime/enginevm"
	"doggo/internal/runtime/profile"
	runtimerunner "doggo/internal/runtime/runner"
	"doggo/internal/types"
	"doggo/internal/types/coll"
)

type RunOptions struct {
	Paths      []string
	Namespaces []string
}

type Runner interface {
	LoadFile(path string) (types.Value, error)
	Run(expr string) (types.Value, error)
}

type Service struct {
	OpenGenerationSession func(scriptPath string) (*project.GenerationSession, error)
	NewRunner             func(cfg runtimerunner.Config) (Runner, error)
}

func (s Service) Run(opts RunOptions) (_ *Report, err error) {
	openSession := s.OpenGenerationSession
	if openSession == nil {
		openSession = project.OpenGenerationSession
	}
	newRunner := s.NewRunner
	if newRunner == nil {
		newRunner = newStandardRunner
	}

	session, err := openSession(runtimeConfigHint(opts.Paths))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	cfg := session.Config

	targetFiles, err := ResolveTargetFiles(cfg.ProjectRoot, opts.Paths)
	if err != nil {
		return nil, err
	}

	runner, err := newRunner(runtimerunner.Config{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         append([]string(nil), cfg.LoadPaths...),
		GenerationSources: append([]*project.GenerationSource(nil), cfg.GenerationSources...),
		Lock:              cfg.Lock,
		BootstrapCore:     true,
	})
	if err != nil {
		return nil, err
	}
	if _, err := runner.Run(`(require 'doggo.test)`); err != nil {
		return nil, err
	}
	for _, file := range targetFiles {
		if _, err := runner.LoadFile(file); err != nil {
			return nil, err
		}
	}

	targetNamespaces := normalizeStrings(opts.Namespaces)
	if len(targetNamespaces) == 0 {
		targetNamespaces, err = extractRegisteredTestNamespaces(runner)
		if err != nil {
			return nil, err
		}
	}
	if len(targetNamespaces) == 0 {
		return &Report{Summary: Summary{}, Cases: []Result{}}, nil
	}

	value, err := runner.Run(buildRunTestsExpr(targetNamespaces))
	if err != nil {
		return nil, err
	}
	return DecodeReport(value)
}

func newStandardRunner(cfg runtimerunner.Config) (Runner, error) {
	if cfg.Profile == nil {
		cfg.Profile = profile.Host()
	}
	runner, err := runtimerunner.New(cfg)
	if err != nil {
		return nil, err
	}
	vmRunner, ok := enginevm.AsVMRunner(runner)
	if !ok || vmRunner == nil {
		return nil, fmt.Errorf("runtime runner did not expose a vm runner")
	}
	return vmRunner, nil
}

func ResolveTargetFiles(projectRoot string, paths []string) ([]string, error) {
	matchTestFile := func(path string) bool {
		return strings.HasSuffix(path, "_test.dog")
	}
	excludes := map[string]struct{}{
		".doggo":   {},
		".git":     {},
		"docs":     {},
		"examples": {},
		"testdata": {},
		"vendor":   {},
	}

	if len(paths) == 0 {
		return collectDogFiles([]string{projectRoot}, excludes, matchTestFile, false)
	}
	return collectDogFiles(paths, excludes, matchTestFile, true)
}

func runtimeConfigHint(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	hint := paths[0]
	if info, err := os.Stat(hint); err == nil && info.IsDir() {
		// Generation sessions resolve a file's containing directory. A target
		// directory must start the search there, including its own doggo.mod.
		return filepath.Join(hint, project.ModFileName)
	}
	return hint
}

func extractRegisteredTestNamespaces(runner Runner) ([]string, error) {
	testsVar, err := runner.Run(`doggo.test/*tests*`)
	if err != nil {
		return nil, err
	}
	atomVal, ok := testsVar.(*doggoatom.Atom)
	if !ok {
		return nil, fmt.Errorf("doggo.test/*tests* is not atom")
	}

	state := atomVal.Deref()
	m, ok := state.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("doggo.test/*tests* value is not map")
	}

	set := make(map[string]struct{})
	for _, key := range m.Order {
		vec, ok := key.(*coll.Vector)
		if !ok || len(vec.Items) == 0 {
			continue
		}
		nsName, ok := types.SymbolName(vec.Items[0])
		if !ok || nsName == "" {
			continue
		}
		set[nsName] = struct{}{}
	}

	out := make([]string, 0, len(set))
	for ns := range set {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out, nil
}

func buildRunTestsExpr(namespaces []string) string {
	if len(namespaces) == 0 {
		return "(doggo.test/run-tests)"
	}
	parts := make([]string, 0, len(namespaces)+1)
	parts = append(parts, "doggo.test/run-tests")
	for _, ns := range namespaces {
		parts = append(parts, "'"+ns)
	}
	return "(" + strings.Join(parts, " ") + ")"
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func collectDogFiles(paths []string, excludes map[string]struct{}, matchFile func(path string) bool, includeExplicitFiles bool) ([]string, error) {
	targets := paths
	if len(targets) == 0 {
		targets = []string{"."}
	}

	found := make(map[string]struct{})
	for _, raw := range targets {
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			if filepath.Ext(abs) != ".dog" {
				return nil, fmt.Errorf("not a .dog file: %s", raw)
			}
			if includeExplicitFiles || matchFile == nil || matchFile(abs) {
				found[filepath.Clean(abs)] = struct{}{}
			}
			continue
		}
		if err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != abs && shouldSkipDir(path, d.Name(), excludes) {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".dog" {
				return nil
			}
			if matchFile != nil && !matchFile(path) {
				return nil
			}
			found[filepath.Clean(path)] = struct{}{}
			return nil
		}); err != nil {
			return nil, err
		}
	}

	out := make([]string, 0, len(found))
	for path := range found {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func shouldSkipDir(path, name string, excludes map[string]struct{}) bool {
	if excludes == nil {
		return false
	}
	if _, ok := excludes[name]; ok {
		return true
	}
	if path == "." || path == string(filepath.Separator) {
		return false
	}
	_, ok := excludes[filepath.Base(path)]
	return ok
}
