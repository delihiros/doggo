package newproject

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
)

const (
	defaultVersion = "0.1.0"
	defaultLicense = "MIT"
	defaultCompat  = ">=0.1.0"
)

var templates = []string{"app", "lib", "cli"}

type Options struct {
	Template string
	Name     string
	Module   string
	DryRun   bool
	BaseDir  string
}

type PlannedFile struct {
	Path    string
	Content string
}

type Result struct {
	ProjectDir string
	Files      []PlannedFile
	DryRun     bool
}

type Service struct{}

type projectNames struct {
	name      string
	module    string
	namespace string
	sourceRel string
	testRel   string
}

func (Service) Run(opts Options) (_ *Result, retErr error) {
	names, err := deriveNames(opts)
	if err != nil {
		return nil, err
	}
	files, err := planFiles(opts.Template, names)
	if err != nil {
		return nil, err
	}

	baseDir := opts.BaseDir
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve target parent: %w", err)
	}
	projectDir := filepath.Join(baseDir, names.name)
	result := &Result{ProjectDir: projectDir, Files: files, DryRun: opts.DryRun}
	if err := requireAbsentTarget(projectDir); err != nil {
		return nil, err
	}

	stage, err := beginProjectStage(filepath.Dir(projectDir))
	if stage != nil {
		defer func() { retErr = errors.Join(retErr, stage.Close()) }()
	}
	if err != nil {
		return nil, fmt.Errorf("create temporary project: %w", err)
	}

	if err := assembleProject(stage, files); err != nil {
		return nil, fmt.Errorf("assemble project: %w", err)
	}
	if opts.DryRun {
		return result, nil
	}
	if err := publishNewProject(stage, projectDir); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("target %s already exists; choose another project name or remove it: %w", projectDir, err)
		}
		return nil, fmt.Errorf("publish project: %w", err)
	}
	return result, nil
}

func assembleProject(stage *projectStage, files []PlannedFile) (retErr error) {
	defer func() {
		if errors.Is(retErr, project.ErrOwnershipChanged) {
			stage.preserve = true
		}
	}()
	for _, file := range files {
		path := filepath.FromSlash(file.Path)
		if err := stage.tree.CreateAll(filepath.ToSlash(filepath.Dir(path)), 0755); err != nil {
			return fmt.Errorf("create parent for %s: %w", file.Path, err)
		}
		if err := stage.tree.WriteFile(filepath.ToSlash(path), []byte(file.Content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	if err := stage.verify(); err != nil {
		return err
	}
	if beforeNewProjectGenerationForTest != nil {
		beforeNewProjectGenerationForTest(stage.Path())
	}

	generationRoot, err := project.CloneVerifiedRoot(stage.root)
	if err != nil {
		return err
	}
	prepared, err := (generation.Service{}).PrepareSyncTree(generationRoot, stage.Path(), stage.tree, []string{})
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, prepared.Abort()) }()
	commitErr := prepared.Commit()
	if afterNewProjectCommitForTest != nil {
		if err := afterNewProjectCommitForTest(stage.Path()); err != nil {
			return errors.Join(commitErr, err)
		}
	}
	if commitErr != nil {
		return commitErr
	}
	if err := stage.verify(); err != nil {
		return err
	}
	sessionRoot, err := project.CloneVerifiedRoot(stage.root)
	if err != nil {
		return err
	}
	session, err := project.OpenGenerationSessionRoot(sessionRoot, stage.Path())
	if err != nil {
		return err
	}
	if err := session.Close(); err != nil {
		return err
	}
	if err := stage.verify(); err != nil {
		return err
	}
	if afterNewProjectGenerationForTest != nil {
		if err := afterNewProjectGenerationForTest(stage.Path()); err != nil {
			return err
		}
	}
	if err := project.SyncRootTree(stage.root); err != nil {
		return err
	}
	return stage.verify()
}

func requireAbsentTarget(target string) error {
	_, err := os.Lstat(target)
	if err == nil {
		return fmt.Errorf("target %s already exists; choose another project name or remove it", target)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect target %s: %w", target, err)
	}
	return nil
}

func deriveNames(opts Options) (projectNames, error) {
	name := strings.TrimSpace(opts.Name)
	if err := validateProjectName(name); err != nil {
		return projectNames{}, err
	}
	template := strings.TrimSpace(opts.Template)
	if !isTemplate(template) {
		return projectNames{}, fmt.Errorf("unknown template %q (available: %s)", template, strings.Join(templates, ", "))
	}
	module := strings.TrimSpace(opts.Module)
	if module == "" {
		module = name
	}
	if err := validateModule(module); err != nil {
		return projectNames{}, err
	}
	nsBase, err := namespaceBase(name)
	if err != nil {
		return projectNames{}, err
	}
	sourceName := "core"
	if template == "cli" {
		sourceName = "main"
	}
	namespace := nsBase + "." + sourceName
	sourceRel := filepath.ToSlash(filepath.Join("src", filepath.FromSlash(strings.ReplaceAll(nsBase, ".", "/")), sourceName+".dog"))
	testRel := filepath.ToSlash(filepath.Join("test", filepath.FromSlash(strings.ReplaceAll(nsBase, ".", "/")), sourceName+"_test.dog"))
	return projectNames{
		name:      name,
		module:    module,
		namespace: namespace,
		sourceRel: sourceRel,
		testRel:   testRel,
	}, nil
}

func validateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("invalid project name: must not be empty")
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return fmt.Errorf("invalid project name %q: must not contain '..'", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid project name %q: path separators are not allowed", name)
	}
	for _, r := range name {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.') {
			return fmt.Errorf("invalid project name %q: use ASCII letters, digits, '_', '-', or '.'", name)
		}
	}
	return nil
}

func validateModule(module string) error {
	if module == "" {
		return fmt.Errorf("invalid module: must not be empty")
	}
	if strings.Contains(module, "..") {
		return fmt.Errorf("invalid module %q: must not contain '..'", module)
	}
	if strings.ContainsAny(module, `\@?`) {
		return fmt.Errorf("invalid module %q: use ASCII letters, digits, '_', '-', '.', or '/'", module)
	}
	segments := strings.Split(module, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." {
			return fmt.Errorf("invalid module %q: path segments must not be empty", module)
		}
		for _, r := range segment {
			if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.') {
				return fmt.Errorf("invalid module %q: use ASCII letters, digits, '_', '-', '.', or '/'", module)
			}
		}
	}
	return nil
}

func namespaceBase(name string) (string, error) {
	parts := strings.Split(name, ".")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return "", fmt.Errorf("invalid project name %q: namespace segments must not be empty", name)
		}
		segment := strings.ReplaceAll(part, "-", "_")
		first := rune(segment[0])
		if unicode.IsDigit(first) {
			return "", fmt.Errorf("invalid project name %q: namespace segments must not start with a digit", name)
		}
		out = append(out, segment)
	}
	namespace := strings.Join(out, ".")
	if namespace == "doggo" || strings.HasPrefix(namespace, "doggo.") {
		return "", fmt.Errorf("invalid project name %q: doggo.* namespaces are reserved", name)
	}
	return namespace, nil
}

func isTemplate(template string) bool {
	for _, candidate := range templates {
		if template == candidate {
			return true
		}
	}
	return false
}

func planFiles(template string, names projectNames) ([]PlannedFile, error) {
	source, test, err := templateSource(template, names)
	if err != nil {
		return nil, err
	}
	files := []PlannedFile{
		{Path: project.ModFileName, Content: renderMod(names)},
		{Path: ".gitignore", Content: "/.doggo/\n"},
		{Path: "README.md", Content: renderReadme(template, names)},
		{Path: names.sourceRel, Content: source},
		{Path: names.testRel, Content: test},
	}
	sort.SliceStable(files[1:], func(i, j int) bool {
		return files[1+i].Path < files[1+j].Path
	})
	return files, nil
}

func renderMod(names projectNames) string {
	mod := modFor(names)
	return fmt.Sprintf(
		"{:module %s :name %s :version %s :license %s :compat %s :deps []}\n",
		strconv.Quote(mod.Module),
		strconv.Quote(mod.Name),
		strconv.Quote(mod.Version),
		strconv.Quote(mod.License),
		strconv.Quote(mod.Compat),
	)
}

func modFor(names projectNames) *project.ModFile {
	return &project.ModFile{
		Module:  names.module,
		Name:    names.name,
		Version: defaultVersion,
		License: defaultLicense,
		Compat:  defaultCompat,
		Deps:    []string{},
	}
}

func renderReadme(template string, names projectNames) string {
	commands := []string{"doggo mod sync"}
	switch template {
	case "app":
		commands = append(commands,
			"doggo build",
			fmt.Sprintf("doggo exec -- %s Doggo", names.sourceRel),
			fmt.Sprintf("doggo test %s", names.testRel),
		)
	case "cli":
		commands = append(commands,
			"doggo build",
			fmt.Sprintf("doggo exec -- %s one two", names.sourceRel),
			fmt.Sprintf("doggo test %s", names.testRel),
		)
	default:
		commands = append(commands, fmt.Sprintf("doggo test %s", names.testRel))
	}
	return fmt.Sprintf(`# %s

Generated with `+"`doggo new %s %s`"+`.

## Usage

`+"```bash\n%s\n```\n", names.name, template, names.name, strings.Join(commands, "\n"))
}

func templateSource(template string, names projectNames) (string, string, error) {
	switch template {
	case "app":
		return appSource(names), appTest(names), nil
	case "lib":
		return libSource(names), libTest(names), nil
	case "cli":
		return cliSource(names), cliTest(names), nil
	default:
		return "", "", fmt.Errorf("unknown template %q (available: %s)", template, strings.Join(templates, ", "))
	}
}

func appSource(names projectNames) string {
	return fmt.Sprintf(`(ns %s
  (:require [doggo.cli :as cli]))

(def greeting
  (fn [name]
    (str "Hello, " name "!")))

(def main
  (fn [& args]
    (println (greeting (if (empty? args) "Doggo" (first args))))))

(when (cli/entrypoint?)
  (cli/main main))
`, names.namespace)
}

func appTest(names projectNames) string {
	return fmt.Sprintf(`(ns %s-test
  (:require [doggo.test :as t]
    [%s :as subject]))

(t/deftest greeting-test
  (t/is (= "Hello, Doggo!" (subject/greeting "Doggo"))))
`, names.namespace, names.namespace)
}

func libSource(names projectNames) string {
	return fmt.Sprintf(`(ns %s)

(def add
  (fn [x y]
    (+ x y)))
`, names.namespace)
}

func libTest(names projectNames) string {
	return fmt.Sprintf(`(ns %s-test
  (:require [doggo.test :as t]
    [%s :as subject]))

(t/deftest add-test
  (t/is (= 3 (subject/add 1 2))))
`, names.namespace, names.namespace)
}

func cliSource(names projectNames) string {
	return fmt.Sprintf(`(ns %s
  (:require [doggo.cli :as cli]))

(def run
  (fn [args]
    (str "args=" (count args))))

(def main
  (fn [& args]
    (println (run args))))

(when (cli/entrypoint?)
  (cli/main main))
`, names.namespace)
}

func cliTest(names projectNames) string {
	return fmt.Sprintf(`(ns %s-test
  (:require [doggo.test :as t]
    [%s :as subject]))

(t/deftest run-test
  (t/is (= "args=2" (subject/run ["one" "two"]))))
`, names.namespace, names.namespace)
}
