package codegen

import (
	"bytes"
	"errors"
	"go/format"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"text/template"

	"doggo/internal/project"
	"doggo/internal/testutil"
	"doggo/internal/toolchain/reifytarget"
)

func TestRunInteropGenCLIGeneratesInteropFileFromDiscoveredProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)

	nestedDir := filepath.Join(projectRoot, "nested", "work")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}

	restoreWorkingDir := chdirForTest(t, nestedDir)
	defer restoreWorkingDir()

	outputFile := filepath.Join(projectRoot, "internal", "interop", "generated.go")
	if err := RunInteropGenCLI([]string{"-o", outputFile, "fmt"}); err != nil {
		t.Fatalf("RunInteropGenCLI failed: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}

	output := string(content)
	if !strings.Contains(output, "package interop") {
		t.Fatalf("expected interop package declaration, got:\n%s", output)
	}
	if !strings.Contains(output, `RegisterPackage("fmt"`) {
		t.Fatalf("expected fmt registration, got:\n%s", output)
	}
}

func TestGenerateInteropFileProducesExpectedOutput(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)

	outputFile := filepath.Join(projectRoot, "internal", "interop", "generated.go")
	if err := GenerateInteropFile(projectRoot, outputFile, []string{"fmt"}); err != nil {
		t.Fatalf("GenerateInteropFile failed: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}

	output := string(content)
	if !strings.Contains(output, `"fmt"`) {
		t.Fatalf("expected fmt import, got:\n%s", output)
	}
	if !regexp.MustCompile(`"Println":\s+fmt\.Println`).MatchString(output) {
		t.Fatalf("expected fmt.Println export, got:\n%s", output)
	}
}

func TestGenerateInteropRootDoesNotLoadReplacementAfterFinalVerification(t *testing.T) {
	stagePath := t.TempDir()
	projectRoot := filepath.Join(stagePath, "toolchain")
	if err := os.Mkdir(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestGoMod(t, projectRoot)
	stage, err := os.OpenRoot(stagePath)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	tree := project.NewOwnedTree(stage)
	defer tree.Close()
	if err := tree.CaptureExisting(); err != nil {
		t.Fatal(err)
	}
	moved := projectRoot + "-owned"
	attempted, replaced := false, false
	beforeCodegenPackagesLoadForTest = func(path string) {
		attempted = true
		replaced = testutil.RenameRetained(t, path, moved)
		if !replaced {
			return
		}
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeCodegenPackagesLoadForTest = nil })

	generateErr := (Invoker{}).GenerateInteropTree(tree, stagePath, []string{"fmt"})
	if !attempted {
		t.Fatal("packages-load replacement hook was not called")
	}
	if !replaced {
		if err := errors.Join(generateErr, tree.VerifyComplete()); err != nil {
			t.Fatalf("generation after blocked replacement: %v", err)
		}
		return
	}
	if generateErr == nil {
		t.Fatal("GenerateInteropTree accepted a replaced toolchain")
	}
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("replacement project was mutated: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(moved, "go.mod")); err != nil {
		t.Fatalf("owned project was lost: %v", err)
	}
}

func TestGenerateInteropRootCleanupPreservesTempReplacement(t *testing.T) {
	stagePath := t.TempDir()
	projectRoot := filepath.Join(stagePath, "toolchain")
	if err := os.Mkdir(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestGoMod(t, projectRoot)
	stage, err := os.OpenRoot(stagePath)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	tree := project.NewOwnedTree(stage)
	defer tree.Close()
	if err := tree.CaptureExisting(); err != nil {
		t.Fatal(err)
	}
	var replacement, moved string
	replaced := false
	beforeCodegenTempCleanupForTest = func(path string) {
		replacement, moved = path, path+"-owned"
		replaced = testutil.RenameRetained(t, path, moved)
		if !replaced {
			return
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		beforeCodegenTempCleanupForTest = nil
		_ = os.RemoveAll(replacement)
		_ = os.RemoveAll(moved)
	})

	generateErr := (Invoker{}).GenerateInteropTree(tree, stagePath, []string{"fmt"})
	if replacement == "" {
		t.Fatal("temp-cleanup replacement hook was not called")
	}
	if !replaced {
		if err := errors.Join(generateErr, tree.VerifyComplete()); err != nil {
			t.Fatalf("generation after blocked temp replacement: %v", err)
		}
		if _, err := os.Lstat(replacement); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned temporary root remains: %v", err)
		}
		return
	}
	if generateErr == nil {
		t.Fatal("GenerateInteropTree accepted uncertain temp cleanup")
	}
	entries, err := os.ReadDir(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("temp replacement was mutated: %v", entries)
	}
}

func TestGenerateInteropFileFiltersUnsupportedExports(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)

	outputFile := filepath.Join(projectRoot, "internal", "interop", "generated.go")
	if err := GenerateInteropFile(projectRoot, outputFile, []string{"math", "reflect"}); err != nil {
		t.Fatalf("GenerateInteropFile failed: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	output := string(content)

	for _, unsupported := range []string{`"Kind":`, `"ChanDir":`, `"TypeFor":`, `"MaxUint":`, `"MaxUint64":`} {
		if strings.Contains(output, unsupported) {
			t.Errorf("generated output contains unsupported export %s", unsupported)
		}
	}
	for _, supported := range []string{`"TypeOf":`, `"Pi":`} {
		if !strings.Contains(output, supported) {
			t.Errorf("generated output is missing supported export %s", supported)
		}
	}
}

func TestGenerateInteropFileRejectsMixedTargetsWithoutOverwriting(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")

	outputFile := filepath.Join(projectRoot, "internal", "interop", "generated.go")
	sentinel := []byte("existing generated interop\n")
	if err := os.WriteFile(outputFile, sentinel, 0644); err != nil {
		t.Fatalf("seed generated interop: %v", err)
	}

	err := GenerateInteropFile(projectRoot, outputFile, []string{"fmt", "example.invalid/missing"})
	if err == nil {
		t.Error("GenerateInteropFile succeeded with a missing requested package")
	} else if !strings.Contains(err.Error(), "example.invalid/missing") {
		t.Errorf("GenerateInteropFile error = %v, want missing package path", err)
	}
	got, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated interop: %v", err)
	}
	if !bytes.Equal(got, sentinel) {
		t.Fatalf("generated interop was overwritten:\n%s", got)
	}
}

func TestGenerateInteropFileProducesGofmtStableOutput(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)

	outputFile := filepath.Join(projectRoot, "internal", "interop", "generated.go")
	if err := GenerateInteropFile(projectRoot, outputFile, []string{"fmt"}); err != nil {
		t.Fatalf("GenerateInteropFile failed: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	formatted, err := format.Source(content)
	if err != nil {
		t.Fatalf("generated file is not parseable by gofmt: %v", err)
	}
	if !bytes.Equal(content, formatted) {
		t.Fatalf("generated file is not gofmt-stable")
	}
}

func TestGenerateInteropFileReturnsErrorWhenNoPackagesRequested(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)

	outputFile := filepath.Join(projectRoot, "internal", "interop", "generated.go")
	err := GenerateInteropFile(projectRoot, outputFile, nil)
	if err == nil {
		t.Fatal("expected GenerateInteropFile to fail with no packages")
	}
	if !strings.Contains(err.Error(), "no packages loaded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeneratorCLIsUseGoWorkspaceModules(t *testing.T) {
	tempRoot := t.TempDir()
	appRoot := writeGeneratorWorkspaceFixture(t, tempRoot)
	t.Setenv("GOWORK", filepath.Join(tempRoot, "go.work"))
	t.Setenv("GOPROXY", "off")
	restoreWorkingDir := chdirForTest(t, appRoot)
	defer restoreWorkingDir()
	assertWorkspaceGenerators(t, appRoot)
}

func writeGeneratorWorkspaceFixture(t *testing.T, tempRoot string) string {
	t.Helper()
	const pkg = "example.com/greeter"
	appRoot := filepath.Join(tempRoot, "app")
	greeterRoot := filepath.Join(tempRoot, "greeter")
	for _, dir := range []string{appRoot, greeterRoot} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(appRoot, "go.mod"):         "module doggo\n\ngo 1.24.0\n",
		filepath.Join(tempRoot, "go.work"):       "go 1.24.0\n\nuse (\n\t./app\n\t./greeter\n)\n",
		filepath.Join(greeterRoot, "go.mod"):     "module " + pkg + "\n\ngo 1.24.0\n",
		filepath.Join(greeterRoot, "greeter.go"): "package greeter\n\ntype Greeter interface {\n\tGreet(string) string\n}\n\nfunc Hello(name string) string { return name }\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	writeTestFiles(t, appRoot, map[string]string{
		"internal/types/types.go":      "package types\n\ntype Value interface{}\ntype Environment interface{}\ntype Map interface { Get(Value) Value }\ntype Vector interface { Value; Count() int; Nth(int) Value }\n",
		"internal/types/atom/atom.go":  "package atom\n\nimport \"doggo/internal/types\"\n\ntype keyword string\nfunc NewKeyword(value string) types.Value { return keyword(value) }\nvar NilValue types.Value\ntype GoObject struct{ Value interface{} }\nfunc (o *GoObject) Unwrap() interface{} { return o.Value }\n",
		"internal/interop/registry.go": "package interop\n\nimport (\"reflect\"; \"doggo/internal/types\")\nfunc RegisterPackage(string, map[string]interface{}) {}\nfunc RegisterReifyFactory(reflect.Type, func(interface{}, types.Environment, types.Map) interface{}) {}\nfunc ConvertReflectValueToDoggo(reflect.Value) (types.Value, error) { return nil, nil }\nfunc ConvertDoggoToReflectValue(types.Value, reflect.Type) (reflect.Value, error) { return reflect.Value{}, nil }\n",
		"internal/reify/base.go":       "package reify\n",
	})

	return appRoot
}

func assertWorkspaceGenerators(t *testing.T, appRoot string) {
	t.Helper()
	const (
		pkg           = "example.com/greeter"
		interfaceName = pkg + ".Greeter"
	)

	interopPath := filepath.Join(appRoot, "internal", "interop", "generated.go")
	if err := RunInteropGenCLI([]string{"-o", interopPath, pkg}); err != nil {
		t.Fatalf("RunInteropGenCLI failed: %v", err)
	}
	reifyPath := filepath.Join(appRoot, "internal", "reify", "generated_reify.go")
	if err := RunReifyGenCLI([]string{"-o", reifyPath, interfaceName}); err != nil {
		t.Fatalf("RunReifyGenCLI failed: %v", err)
	}

	interop, err := os.ReadFile(interopPath)
	if err != nil {
		t.Fatalf("read generated interop: %v", err)
	}
	if !strings.Contains(string(interop), `RegisterPackage("`+pkg+`"`) {
		t.Fatalf("generated interop does not register workspace package:\n%s", interop)
	}
	if !regexp.MustCompile(`"Hello":\s+greeter\.Hello`).Match(interop) {
		t.Fatalf("generated interop does not include workspace function:\n%s", interop)
	}
	implementation, err := os.ReadFile(reifyPath)
	if err != nil {
		t.Fatalf("read generated implementation: %v", err)
	}
	if !strings.Contains(string(implementation), `interop.RegisterReifyFactory(greeterType`) {
		t.Fatalf("generated implementation does not register workspace interface:\n%s", implementation)
	}
	if !strings.Contains(string(implementation), "type GreeterGreeterImpl struct") {
		t.Fatalf("generated implementation does not contain workspace interface implementation:\n%s", implementation)
	}
}

func TestRunReifyGenCLIGeneratesImplementationsFromDiscoveredProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)

	nestedDir := filepath.Join(projectRoot, "nested", "work")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}

	restoreWorkingDir := chdirForTest(t, nestedDir)
	defer restoreWorkingDir()

	outputFile := filepath.Join(projectRoot, "generated_reify.go")
	if err := RunReifyGenCLI([]string{"-o", outputFile, "fmt.Stringer", "net/http.Handler"}); err != nil {
		t.Fatalf("RunReifyGenCLI failed: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated implementation file: %v", err)
	}

	output := string(content)
	if !strings.Contains(output, "type FmtStringerImpl struct") {
		t.Fatalf("expected fmt.Stringer implementation, got:\n%s", output)
	}
	if !strings.Contains(output, "type HttpHandlerImpl struct") {
		t.Fatalf("expected net/http.Handler implementation, got:\n%s", output)
	}
}

func TestGenerateReifyFileProducesExpectedOutput(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)

	outputFile := filepath.Join(projectRoot, "generated_reify.go")
	if _, err := GenerateReifyFile(projectRoot, outputFile, requiredCandidates("fmt.Stringer")); err != nil {
		t.Fatalf("GenerateReifyFile failed: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated implementation file: %v", err)
	}

	output := string(content)
	if !strings.Contains(output, `interop.RegisterReifyFactory(stringerType`) {
		t.Fatalf("expected fmt.Stringer implementation registration, got:\n%s", output)
	}
	if !strings.Contains(output, "type FmtStringerImpl struct") {
		t.Fatalf("expected fmt.Stringer implementation struct, got:\n%s", output)
	}
}

func TestGenerateReifyFilePreservesVariadicMethod(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")

	sourcePath := filepath.Join(projectRoot, "variadic", "variadic.go")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatalf("mkdir variadic package: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("package variadic\n\ntype Joiner interface { Join(prefix string, values ...int) string }\n"), 0644); err != nil {
		t.Fatalf("write variadic package: %v", err)
	}

	outputFile := filepath.Join(projectRoot, "generated_reify.go")
	if _, err := GenerateReifyFile(projectRoot, outputFile, requiredCandidates("doggo/variadic.Joiner")); err != nil {
		t.Fatalf("GenerateReifyFile failed: %v", err)
	}
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated implementation file: %v", err)
	}
	output := string(content)
	if !strings.Contains(output, "func (b *VariadicJoinerImpl) Join(arg0 string, arg1 ...int) string") {
		t.Errorf("generated method does not preserve variadic parameter:\n%s", output)
	}
	if !strings.Contains(output, "arg1Value, err := wrapGoValue([]int(arg1))") {
		t.Errorf("generated method does not wrap the variadic slice as one callback argument:\n%s", output)
	}
	if strings.Contains(output, "wrapGoValue(arg1...)") {
		t.Errorf("generated method expands the variadic slice for the callback:\n%s", output)
	}
	if !strings.Contains(output, `variadic "doggo/variadic"`) {
		t.Errorf("generated output does not import the target package:\n%s", output)
	}
	if !strings.Contains(output, "var _ variadic.Joiner = (*VariadicJoinerImpl)(nil)") {
		t.Errorf("generated implementation has no compile-time interface assertion:\n%s", output)
	}
}

func TestGenerateReifyFileAllocatesImportsAcrossImplementations(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")

	for path, content := range map[string]string{
		"alpha/alpha.go":                 "package shared\n\ntype Item struct{}\ntype First interface { Use(Item) }\n",
		"beta/beta.go":                   "package shared\n\ntype Item struct{}\ntype Second interface { Use(Item) }\n",
		"customcontext/customcontext.go": "package context\n\ntype Item struct{}\ntype Third interface { Use(Item) }\n",
	} {
		fullPath := filepath.Join(projectRoot, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", fullPath, err)
		}
	}

	outputFile := filepath.Join(projectRoot, "generated_reify.go")
	if _, err := GenerateReifyFile(projectRoot, outputFile, requiredCandidates(
		"doggo/alpha.First",
		"doggo/beta.Second",
		"doggo/customcontext.Third",
		"log/slog.Handler",
	)); err != nil {
		t.Fatalf("GenerateReifyFile failed: %v", err)
	}
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated implementation file: %v", err)
	}
	output := string(content)
	for _, want := range []string{
		`shared "doggo/alpha"`,
		`shared2 "doggo/beta"`,
		`context2 "doggo/customcontext"`,
		`slog "log/slog"`,
		"func (b *SharedFirstImpl) Use(arg0 shared.Item)",
		"func (b *Shared2SecondImpl) Use(arg0 shared2.Item)",
		"func (b *Context2ThirdImpl) Use(arg0 context2.Item)",
		"var _ shared.First = (*SharedFirstImpl)(nil)",
		"var _ shared2.Second = (*Shared2SecondImpl)(nil)",
		"var _ context2.Third = (*Context2ThirdImpl)(nil)",
		"var _ slog.Handler = (*SlogHandlerImpl)(nil)",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("generated output does not contain %q:\n%s", want, output)
		}
	}
	if got := strings.Count(output, `"context"`); got != 1 {
		t.Errorf("generated context import count = %d, want 1:\n%s", got, output)
	}
	if strings.Contains(output, `context "context"`) {
		t.Errorf("generated output redeclares the fixed context import:\n%s", output)
	}
}

func TestGenerateReifyFileCanonicalizesInterfaceAlias(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")

	for path, content := range map[string]string{
		"origin/origin.go": "package origin\n\ntype Reader interface { Read([]byte) (int, error) }\n",
		"facade/facade.go": "package facade\n\nimport \"doggo/origin\"\n\ntype Reader = origin.Reader\n",
	} {
		fullPath := filepath.Join(projectRoot, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", fullPath, err)
		}
	}

	outputFile := filepath.Join(projectRoot, "generated_reify.go")
	if _, err := GenerateReifyFile(projectRoot, outputFile, requiredCandidates("doggo/origin.Reader", "doggo/facade.Reader")); err != nil {
		t.Fatalf("GenerateReifyFile failed: %v", err)
	}
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated implementation file: %v", err)
	}
	output := string(content)
	if got := strings.Count(output, `interop.RegisterReifyFactory(readerType`); got != 1 {
		t.Fatalf("canonical origin.Reader registrations = %d, want 1:\n%s", got, output)
	}
	if got := strings.Count(output, "type FacadeReaderImpl struct"); got != 1 {
		t.Fatalf("canonical FacadeReaderImpl declarations = %d, want 1:\n%s", got, output)
	}
}

func TestGenerateReifyFileAcceptsOnlySemanticallyValidCandidates(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")

	writeTestFiles(t, projectRoot, map[string]string{
		"facade/facade.go": `package facade

type hidden interface { Public() }
type Public = hidden
type PublicAgain = hidden
type Generic[T any] interface { Take(T) }
type Int = Generic[int]
type Sealed interface { sealed() }
type Collides interface { Invoker(); Env(); Impl() }
type Variadic interface { Join(prefix string, values ...int) string }
`,
		"owner/internal/secret/secret.go": "package secret\n\ntype Token struct{}\n",
		"owner/api/api.go": `package api

import "doggo/owner/internal/secret"

type LeaksInternal interface { Use(secret.Token) }
`,
		"predeclared/predeclared.go": `package error

type Token struct{}
type UsesPredeclaredName interface { Use(Token) error }
`,
		"generatedname/generatedname.go": `package zeroValue

type Token struct{}
type UsesGeneratedName interface { Use(Token) }
`,
	})

	outputFile := filepath.Join(projectRoot, "internal", "reify", "generated_reify.go")
	targets, err := GenerateReifyFile(projectRoot, outputFile, []reifytarget.Candidate{
		{Source: "doggo/facade.PublicAgain"},
		{Source: "doggo/facade.Sealed"},
		{Source: "doggo/owner/api.LeaksInternal"},
		{Source: "doggo/facade.Public", Required: true},
		{Source: "doggo/facade.Int", Required: true},
		{Source: "doggo/facade.Collides", Required: true},
		{Source: "doggo/facade.Variadic", Required: true},
		{Source: "doggo/predeclared.UsesPredeclaredName", Required: true},
		{Source: "doggo/generatedname.UsesGeneratedName", Required: true},
	})
	if err != nil {
		t.Fatalf("GenerateReifyFile failed: %v", err)
	}

	wantTargets := []project.ReifyTarget{
		{Source: "doggo/facade.Collides", Runtime: "doggo/facade.Collides"},
		{Source: "doggo/facade.Int", Runtime: "doggo/facade.Generic[int]"},
		{Source: "doggo/facade.Variadic", Runtime: "doggo/facade.Variadic"},
		{Source: "doggo/facade.Public", Runtime: "doggo/facade.hidden"},
		{Source: "doggo/generatedname.UsesGeneratedName", Runtime: "doggo/generatedname.UsesGeneratedName"},
		{Source: "doggo/predeclared.UsesPredeclaredName", Runtime: "doggo/predeclared.UsesPredeclaredName"},
	}
	if !reflect.DeepEqual(targets, wantTargets) {
		t.Fatalf("targets = %+v, want %+v", targets, wantTargets)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated implementation file: %v", err)
	}
	output := string(content)
	for _, want := range []string{
		"var _ facade.Public = (*FacadePublicImpl)(nil)",
		"var publicType = reflect.TypeOf((*facade.Public)(nil)).Elem()",
		"interop.RegisterReifyFactory(publicType",
		"func (b *FacadeVariadicImpl) Join(arg0 string, arg1 ...int) string",
		"wrapGoValue([]int(arg1))",
		"invoker CallbackInvoker",
		"env     types.Environment",
		"impl    types.Map",
		`error2 "doggo/predeclared"`,
		`zeroValue2 "doggo/generatedname"`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("generated output does not contain %q:\n%s", want, output)
		}
	}
	for _, omitted := range []string{"PublicAgainImpl", "SealedImpl", "LeaksInternalImpl"} {
		if strings.Contains(output, omitted) {
			t.Errorf("generated output contains omitted optional target %q:\n%s", omitted, output)
		}
	}
}

func TestGenerateReifyFileReturnsCompilerDiagnosticForRequiredInvalidCandidate(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	writeTestFiles(t, projectRoot, map[string]string{
		"facade/facade.go":                "package facade\n\ntype Sealed interface { sealed() }\n",
		"owner/internal/secret/secret.go": "package secret\n\ntype Token struct{}\n",
		"owner/api/api.go":                "package api\n\nimport \"doggo/owner/internal/secret\"\n\ntype LeaksInternal interface { Use(secret.Token) }\n",
	})

	for _, tt := range []struct {
		source string
		want   string
	}{
		{source: "doggo/facade.Sealed", want: "unexported method"},
		{source: "doggo/owner/api.LeaksInternal", want: "use of internal package"},
	} {
		t.Run(tt.source, func(t *testing.T) {
			outputFile := filepath.Join(projectRoot, "internal", "reify", "generated_reify.go")
			_, err := GenerateReifyFile(projectRoot, outputFile, requiredCandidates(tt.source))
			if err == nil {
				t.Fatalf("GenerateReifyFile succeeded for %s", tt.source)
			}
			if !strings.Contains(err.Error(), tt.source) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want source and compiler diagnostic containing %q", err, tt.want)
			}
		})
	}
}

func TestGenerateReifyFileAtomicWriteRejectsOutsideAndSymlinkOutput(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")

	outside := filepath.Join(t.TempDir(), "generated_reify.go")
	if _, err := GenerateReifyFile(projectRoot, outside, requiredCandidates("fmt.Stringer")); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("outside output error = %v, want containment error", err)
	}

	target := t.TempDir()
	link := filepath.Join(projectRoot, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := GenerateReifyFile(projectRoot, filepath.Join(link, "generated_reify.go"), requiredCandidates("fmt.Stringer")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink output error = %v, want symlink error", err)
	}
}

func TestGenerateReifyFileRejectsOutputExcludedFromCompiledPackage(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")

	inactiveGOOS := "windows"
	if runtime.GOOS == inactiveGOOS {
		inactiveGOOS = "linux"
	}
	for _, name := range []string{"_ignored.go", "generated_test.go", "generated_" + inactiveGOOS + ".go"} {
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(projectRoot, "internal", "reify", name)
			_, err := GenerateReifyFile(projectRoot, output, requiredCandidates("reflect.Type"))
			if err == nil {
				t.Fatalf("GenerateReifyFile accepted excluded output %s", output)
			}
			if !strings.Contains(err.Error(), output) || !strings.Contains(err.Error(), "compiled package") {
				t.Fatalf("error = %v, want output path and compiled-package diagnostic", err)
			}
		})
	}
}

func TestGenerateInteropFileRejectsOutputExcludedFromCompiledPackage(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")

	output := filepath.Join(projectRoot, "internal", "interop", "_ignored.go")
	err := GenerateInteropFile(projectRoot, output, []string{"fmt"})
	if err == nil {
		t.Fatalf("GenerateInteropFile accepted excluded output %s", output)
	}
	if !strings.Contains(err.Error(), output) || !strings.Contains(err.Error(), "compiled package") {
		t.Fatalf("error = %v, want output path and compiled-package diagnostic", err)
	}
}

func TestGenerateReifyFileRejectsRequiredEmptyCandidate(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")

	output := filepath.Join(projectRoot, "internal", "reify", "generated_reify.go")
	_, err := GenerateReifyFile(projectRoot, output, []reifytarget.Candidate{{Required: true}})
	if err == nil {
		t.Fatal("GenerateReifyFile accepted a required empty candidate")
	}
	if !strings.Contains(err.Error(), `reify target ""`) || !strings.Contains(err.Error(), "invalid source") {
		t.Fatalf("error = %v, want required empty-source diagnostic", err)
	}
}

func TestGenerateReifyFileRejectsUnsupportedInterfaces(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")

	for path, content := range map[string]string{
		"origin/origin.go": "package origin\n\ntype PlainReader interface { Read() int }\ntype GenericReader[T any] interface { Read() T }\n",
		"facade/facade.go": `package facade

import "doggo/origin"

type Anonymous = interface { Close() error }
type Error = error
type IntReader = origin.GenericReader[int]
type RawGenericReader[T any] = origin.PlainReader
type Constraint interface { ~int | ~int64 }
type hidden struct{}
type Sealed interface { sealed() }
type HiddenComposite interface { Use(map[string][]*hidden) }
`,
	} {
		fullPath := filepath.Join(projectRoot, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", fullPath, err)
		}
	}

	tests := []struct {
		name string
		spec string
		want string
	}{
		{"GenericReader", "doggo/origin.GenericReader", "type arguments"},
		{"RawGenericReader", "doggo/facade.RawGenericReader", "type arguments"},
		{"Constraint", "doggo/facade.Constraint", "constraint"},
		{"Sealed", "doggo/facade.Sealed", "sealed"},
		{"HiddenComposite", "doggo/facade.HiddenComposite", "hidden"},
		{"ReflectType", "reflect.Type", "implement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFile := filepath.Join(projectRoot, "generated_reify_"+tt.name+".go")
			sentinel := []byte("existing generated implementations\n")
			if err := os.WriteFile(outputFile, sentinel, 0644); err != nil {
				t.Fatalf("seed generated implementations: %v", err)
			}

			_, err := GenerateReifyFile(projectRoot, outputFile, requiredCandidates(tt.spec))
			if err == nil {
				t.Fatalf("GenerateReifyFile succeeded for %s", tt.spec)
			}
			if !strings.Contains(err.Error(), tt.spec) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("GenerateReifyFile error = %v, want %s error containing %q", err, tt.spec, tt.want)
			}
			got, readErr := os.ReadFile(outputFile)
			if readErr != nil {
				t.Fatalf("read generated implementations: %v", readErr)
			}
			if !bytes.Equal(got, sentinel) {
				t.Fatalf("generated implementations were overwritten:\n%s", got)
			}
		})
	}
}

func TestGenerateReifyFileRejectsMixedTargetsWithoutOverwriting(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")

	outputFile := filepath.Join(projectRoot, "generated_reify.go")
	sentinel := []byte("existing generated implementations\n")
	if err := os.WriteFile(outputFile, sentinel, 0644); err != nil {
		t.Fatalf("seed generated implementations: %v", err)
	}

	_, err := GenerateReifyFile(projectRoot, outputFile, requiredCandidates("fmt.Stringer", "fmt.DoesNotExist"))
	if err == nil {
		t.Error("GenerateReifyFile succeeded with a missing requested interface")
	} else if !strings.Contains(err.Error(), "fmt.DoesNotExist") {
		t.Errorf("GenerateReifyFile error = %v, want missing interface spec", err)
	}
	got, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read generated implementations: %v", err)
	}
	if !bytes.Equal(got, sentinel) {
		t.Fatalf("generated implementations were overwritten:\n%s", got)
	}
}

func TestGeneratorsRejectIllTypedDependencyWithoutOverwriting(t *testing.T) {
	const (
		pkg           = "doggo/target"
		interfaceName = pkg + ".Greeter"
	)

	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)
	t.Setenv("GOWORK", "off")
	for _, dir := range []string{"broken", "target"} {
		if err := os.MkdirAll(filepath.Join(projectRoot, dir), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(projectRoot, "broken", "broken.go"): "package broken\n\nconst Value = 1\n\nvar _ int = \"broken\"\n",
		filepath.Join(projectRoot, "target", "target.go"): "package target\n\nimport \"doggo/broken\"\n\ntype Greeter interface { Greet(string) string }\n\nfunc Value() int { return broken.Value }\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	interopPath := filepath.Join(projectRoot, "generated.go")
	reifyPath := filepath.Join(projectRoot, "generated_reify.go")
	sentinel := []byte("existing generated code\n")
	for _, path := range []string{interopPath, reifyPath} {
		if err := os.WriteFile(path, sentinel, 0644); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}

	interopErr := GenerateInteropFile(projectRoot, interopPath, []string{pkg})
	if interopErr == nil {
		t.Error("GenerateInteropFile succeeded with an ill-typed dependency")
	} else if !strings.Contains(interopErr.Error(), pkg) {
		t.Errorf("GenerateInteropFile error = %v, want requested package path", interopErr)
	}
	_, reifyErr := GenerateReifyFile(projectRoot, reifyPath, requiredCandidates(interfaceName))
	if reifyErr == nil {
		t.Error("GenerateReifyFile succeeded with an ill-typed dependency")
	} else if !strings.Contains(reifyErr.Error(), interfaceName) {
		t.Errorf("GenerateReifyFile error = %v, want requested interface spec", reifyErr)
	}

	for _, path := range []string{interopPath, reifyPath} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !bytes.Equal(got, sentinel) {
			t.Errorf("generated file %s was overwritten:\n%s", path, got)
		}
	}
}

func TestGenerateReifyFileReturnsWriteError(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot)

	outputFile := filepath.Join(projectRoot, "missing", "generated_reify.go")
	_, err := GenerateReifyFile(projectRoot, outputFile, requiredCandidates("fmt.Stringer"))
	if err == nil {
		t.Fatal("expected GenerateReifyFile to fail on unwritable output path")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateStructName(t *testing.T) {
	tests := []struct {
		pkgAlias  string
		ifaceName string
		expected  string
	}{
		{"fmt", "Stringer", "FmtStringerImpl"},
		{"ebiten", "Game", "EbitenGameImpl"},
		{"http", "Handler", "HttpHandlerImpl"},
		{"io", "Reader", "IoReaderImpl"},
	}

	for _, tt := range tests {
		t.Run(tt.pkgAlias+"."+tt.ifaceName, func(t *testing.T) {
			if got := generateStructName(tt.pkgAlias, tt.ifaceName); got != tt.expected {
				t.Fatalf("generateStructName(%q, %q) = %q, want %q", tt.pkgAlias, tt.ifaceName, got, tt.expected)
			}
		})
	}
}

func TestParseInterfaceCollectsExternalImportsAndCanonicalizesParams(t *testing.T) {
	currentPkg := types.NewPackage("example/game", "game")
	httpPkg := types.NewPackage("net/http", "http")
	contextPkg := types.NewPackage("context", "context")

	requestType := types.NewNamed(
		types.NewTypeName(token.NoPos, httpPkg, "Request", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	contextType := types.NewNamed(
		types.NewTypeName(token.NoPos, contextPkg, "Context", nil),
		types.NewInterfaceType(nil, nil).Complete(),
		nil,
	)
	eventType := types.NewNamed(
		types.NewTypeName(token.NoPos, currentPkg, "Event", nil),
		types.NewStruct(nil, nil),
		nil,
	)

	sig := types.NewSignatureType(
		nil,
		nil,
		nil,
		types.NewTuple(
			types.NewVar(token.NoPos, currentPkg, "", types.NewPointer(requestType)),
			types.NewVar(token.NoPos, currentPkg, "ctx", contextType),
			types.NewVar(token.NoPos, currentPkg, "event", eventType),
		),
		types.NewTuple(
			types.NewVar(token.NoPos, currentPkg, "", contextType),
			types.NewVar(token.NoPos, currentPkg, "", types.Universe.Lookup("error").Type()),
		),
		false,
	)
	iface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, currentPkg, "Handle", sig),
	}, nil).Complete()

	imports := newImportSet()
	pkgAlias := imports.add(currentPkg.Path(), currentPkg.Name())
	got := parseInterface(iface, currentPkg.Path(), pkgAlias, "Handler", imports)
	if got.StructName != "GameHandlerImpl" {
		t.Fatalf("StructName = %q, want GameHandlerImpl", got.StructName)
	}
	if len(got.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(got.Methods))
	}

	method := got.Methods[0]
	if want := []ParamInfo{{Name: "arg0", Type: "*http.Request", Value: "arg0"}, {Name: "arg1", Type: "context.Context", Value: "arg1"}, {Name: "arg2", Type: "game.Event", Value: "arg2"}}; !reflect.DeepEqual(method.Params, want) {
		t.Fatalf("Params = %v, want %v", method.Params, want)
	}
	if want := []ResultInfo{{Type: "context.Context"}, {Type: "error"}}; !reflect.DeepEqual(method.Results, want) {
		t.Fatalf("Results = %v, want %v", method.Results, want)
	}
	if !method.HasError {
		t.Fatal("expected method.HasError to be true")
	}
	if want := []ImportInfo{{Alias: "game", Path: "example/game"}, {Alias: "http", Path: "net/http"}}; !reflect.DeepEqual(imports.imports(), want) {
		t.Fatalf("Imports = %v, want %v", imports.imports(), want)
	}
}

func TestParseInterfaceCanonicalParameterNamesAvoidGeneratedLocalCollisions(t *testing.T) {
	pkg := types.NewPackage("example/collision", "collision")
	sig := types.NewSignatureType(
		nil,
		nil,
		nil,
		types.NewTuple(
			types.NewVar(token.NoPos, pkg, "label", types.Typ[types.String]),
			types.NewVar(token.NoPos, pkg, "labelValue", types.Typ[types.String]),
		),
		types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.Universe.Lookup("error").Type())),
		false,
	)
	iface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Update", sig),
	}, nil).Complete()

	imports := newImportSet()
	pkgAlias := imports.add(pkg.Path(), pkg.Name())
	implementation := parseInterface(iface, pkg.Path(), pkgAlias, "Updater", imports)
	if want := []ParamInfo{{Name: "arg0", Type: "string", Value: "arg0"}, {Name: "arg1", Type: "string", Value: "arg1"}}; !reflect.DeepEqual(implementation.Methods[0].Params, want) {
		t.Fatalf("Params = %v, want %v", implementation.Methods[0].Params, want)
	}
}

func TestReifyTemplateBranches(t *testing.T) {
	funcMap := template.FuncMap{
		"toLower": toLower,
	}

	tmpl, err := template.New("implementation").Funcs(funcMap).Parse(reifyTemplate)
	if err != nil {
		t.Fatalf("parse implementation template: %v", err)
	}

	tests := []struct {
		name          string
		method        InterfaceMethod
		wantWarning   bool
		wantReturnErr bool
	}{
		{
			name: "warns for non-error methods",
			method: InterfaceMethod{
				Name:     "Draw",
				Params:   []ParamInfo{{Name: "screen", Type: "*ebiten.Image", Value: "screen"}},
				HasError: false,
			},
			wantWarning: true,
		},
		{
			name: "returns errors for error methods",
			method: InterfaceMethod{
				Name:     "Update",
				Params:   []ParamInfo{{Name: "label", Type: "string", Value: "label"}},
				Results:  []ResultInfo{{Type: "error"}},
				HasError: true,
			},
			wantReturnErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := TemplateData{
				Implementations: []InterfaceImplData{
					{
						StructName:    "TestImpl",
						InterfaceName: "Game",
						PkgPath:       "example/game",
						PkgAlias:      "game",
						TypeVar:       "gameType",
						Methods:       []InterfaceMethod{tt.method},
					},
				},
			}

			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data); err != nil {
				t.Fatalf("execute implementation template: %v", err)
			}

			output := buf.String()
			warningPattern := `fmt.Fprintf(os.Stderr, "warning: error in reified method Game.`
			if got := strings.Contains(output, warningPattern); got != tt.wantWarning {
				t.Fatalf("warning presence = %v, want %v\noutput:\n%s", got, tt.wantWarning, output)
			}
			if strings.Contains(output, `fmt.Printf("warning: error in reified method`) {
				t.Fatalf("reify warnings must not be written to stdout\noutput:\n%s", output)
			}

			returnErrPattern := "\tif err != nil {\n\t\treturn err\n\t}\n\treturn unwrapToError(result)"
			if got := strings.Contains(output, returnErrPattern); got != tt.wantReturnErr {
				t.Fatalf("error return presence = %v, want %v\noutput:\n%s", got, tt.wantReturnErr, output)
			}
			if tt.wantReturnErr {
				if !strings.Contains(output, "labelValue, err := wrapGoValue(label)") {
					t.Fatalf("expected argument conversion to expose its error\noutput:\n%s", output)
				}
				if !strings.Contains(output, "if err != nil {\n\t\treturn err\n\t}") {
					t.Fatalf("expected argument conversion error to be returned\noutput:\n%s", output)
				}
			}
		})
	}
}

func TestReifyTemplateIncludesInterfaceAndMethodInWarning(t *testing.T) {
	funcMap := template.FuncMap{
		"toLower": toLower,
	}

	tmpl, err := template.New("implementation").Funcs(funcMap).Parse(reifyTemplate)
	if err != nil {
		t.Fatalf("parse implementation template: %v", err)
	}

	data := TemplateData{
		Implementations: []InterfaceImplData{
			{
				StructName:    "GameImpl",
				InterfaceName: "Game",
				PkgPath:       "example/game",
				PkgAlias:      "game",
				TypeVar:       "gameType",
				Methods: []InterfaceMethod{
					{
						Name: "Render",
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute implementation template: %v", err)
	}

	if !strings.Contains(buf.String(), `fmt.Fprintf(os.Stderr, "warning: error in reified method Game.Render: %v\n", err)`) {
		t.Fatalf("expected warning to include interface and method name, output:\n%s", buf.String())
	}
}

func writeTestGoMod(t *testing.T, projectRoot string) {
	t.Helper()

	content := "module doggo\n\ngo 1.24.0\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte(content), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	writeTestFiles(t, projectRoot, map[string]string{
		"internal/types/types.go": `package types

type Value interface{}
type Environment interface{}
type Map interface { Get(Value) Value }
type Vector interface { Value; Count() int; Nth(int) Value }
`,
		"internal/types/atom/atom.go": `package atom

import "doggo/internal/types"

type keyword string
func NewKeyword(value string) types.Value { return keyword(value) }
var NilValue types.Value
type GoObject struct{ Value interface{} }
func (o *GoObject) Unwrap() interface{} { return o.Value }
`,
		"internal/interop/registry.go": `package interop

import (
	"reflect"
	"doggo/internal/types"
)

func RegisterPackage(string, map[string]interface{}) {}
func RegisterReifyFactory(reflect.Type, func(interface{}, types.Environment, types.Map) interface{}) {}
func ConvertReflectValueToDoggo(reflect.Value) (types.Value, error) { return nil, nil }
func ConvertDoggoToReflectValue(types.Value, reflect.Type) (reflect.Value, error) { return reflect.Value{}, nil }
`,
		"internal/reify/base.go": "package reify\n",
	})
}

func requiredCandidates(sources ...string) []reifytarget.Candidate {
	candidates := make([]reifytarget.Candidate, len(sources))
	for i, source := range sources {
		candidates[i] = reifytarget.Candidate{Source: source, Required: true}
	}
	return candidates
}

func writeTestFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", fullPath, err)
		}
	}
}

func chdirForTest(t *testing.T, dir string) func() {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd to %s: %v", previous, err)
		}
	}
}
