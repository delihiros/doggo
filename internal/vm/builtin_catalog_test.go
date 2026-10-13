package vm

import (
	"strings"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func testBuiltinCatalogWithSeven(t *testing.T) *BuiltinCatalog {
	t.Helper()

	catalog, err := NewBuiltinCatalog(
		[]BuiltinDefinition{
			{
				Name: "seven",
				Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
					return atom.NewNumberInt(7)
				}},
			},
		},
	)
	if err != nil {
		t.Fatalf("new builtin catalog: %v", err)
	}
	return catalog
}

func compileBuiltinCallBytecode(t *testing.T, name string) *compiler.Bytecode {
	t.Helper()

	symbolTable := compiler.NewSymbolTable()
	symbolTable.DefineBuiltin(0, name)

	comp := compiler.NewWithState(symbolTable, compiler.NewConstantStore(nil))
	if err := compileRuntimeValue(t, comp, coll.NewList(types.Symbol(name))); err != nil {
		t.Fatalf("compile builtin call: %v", err)
	}
	return comp.Bytecode()
}

func TestNewRunnerWithConfigNilBuiltinCatalogUsesDefaultBuiltins(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: testExactDefaultRuntimeProfile(),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`(count [1 2 3])`)
	if err != nil {
		t.Fatalf("count should be available from default builtin catalog: %v", err)
	}
	if value.String() != "3" {
		t.Fatalf("unexpected count result: %s", value.String())
	}
}

func TestNewRunnerWithConfigComposesExplicitBuiltinCatalogWithCoreRuntime(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: testExactDefaultRuntimeProfileWithBuiltinCatalog(t, testBuiltinCatalogWithSeven(t)),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`[(count [1]) (seven)]`)
	if err != nil {
		t.Fatalf("explicit builtin catalog should compose with core runtime builtins: %v", err)
	}
	if value.String() != "[1 7]" {
		t.Fatalf("unexpected composed builtin result: %s", value.String())
	}
}

func TestNewBuiltinCatalogRejectsDuplicateNames(t *testing.T) {
	_, err := NewBuiltinCatalog([]BuiltinDefinition{
		{Name: "dup", Builtin: &Builtin{Fn: func(args ...types.Value) types.Value { return atom.NilValue }}},
		{Name: "dup", Builtin: &Builtin{Fn: func(args ...types.Value) types.Value { return atom.NilValue }}},
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate builtin "dup"`) {
		t.Fatalf("expected duplicate builtin error, got %v", err)
	}
}

func TestNewBuiltinCatalogClonesAliasedBuiltins(t *testing.T) {
	defaultCount, ok := DefaultBuiltinCatalog().lookup("count")
	if !ok {
		t.Fatal("default builtin catalog is missing count")
	}

	catalog, err := NewBuiltinCatalog([]BuiltinDefinition{
		{Name: "len", Builtin: defaultCount},
	})
	if err != nil {
		t.Fatalf("new builtin catalog: %v", err)
	}

	aliased, ok := catalog.lookup("len")
	if !ok {
		t.Fatal("custom builtin catalog is missing len")
	}
	if aliased == defaultCount {
		t.Fatal("expected builtin catalog to clone aliased builtins")
	}
	if aliased.Name != "len" {
		t.Fatalf("expected aliased builtin name len, got %q", aliased.Name)
	}

	defaultCountAfter, ok := DefaultBuiltinCatalog().lookup("count")
	if !ok {
		t.Fatal("default builtin catalog lost count")
	}
	if defaultCountAfter.Name != "count" {
		t.Fatalf("expected default count builtin name to remain count, got %q", defaultCountAfter.Name)
	}
}

func TestBuiltinCatalogDefinitionsReturnDetachedBuiltins(t *testing.T) {
	definitions := DefaultBuiltinCatalog().Definitions()
	if len(definitions) == 0 {
		t.Fatal("expected builtin definitions")
	}

	originalName := definitions[0].Name
	definitions[0].Builtin.Name = "mutated"

	builtin, ok := DefaultBuiltinCatalog().lookup(originalName)
	if !ok {
		t.Fatalf("default builtin catalog is missing %s", originalName)
	}
	if builtin.Name != originalName {
		t.Fatalf("expected builtin %s to stay unchanged, got %q", originalName, builtin.Name)
	}
}

func TestBuiltinCatalogLookupReturnsDetachedBuiltin(t *testing.T) {
	builtin, ok := DefaultBuiltinCatalog().Lookup("count")
	if !ok {
		t.Fatal("expected count builtin")
	}

	builtin.Name = "mutated"

	defaultCount, ok := DefaultBuiltinCatalog().lookup("count")
	if !ok {
		t.Fatal("default builtin catalog is missing count")
	}
	if defaultCount.Name != "count" {
		t.Fatalf("expected default count builtin name to remain count, got %q", defaultCount.Name)
	}
}

func TestDefaultBuiltinCatalogIncludesSchemaBuiltins(t *testing.T) {
	for _, name := range []string{"valid?", "explain", "generate", "sample", "instrument", "unstrument"} {
		if _, ok := DefaultBuiltinCatalog().lookup(name); !ok {
			t.Fatalf("default builtin catalog is missing %s", name)
		}
	}
}

func TestDefaultPrivateBuiltinsDeclarePublicErrorOwner(t *testing.T) {
	for _, def := range DefaultBuiltinCatalog().Definitions() {
		if !def.IsPublic() && def.ErrorFunction == "" {
			t.Errorf("private builtin %q has no public error owner", def.Name)
		}
	}
}

func TestNewWithBuiltinCatalogResolvesBytecodeBuiltinLayout(t *testing.T) {
	bytecode := compileBuiltinCallBytecode(t, "seven")

	machine, err := NewWithBuiltinCatalog(bytecode, testBuiltinCatalogWithSeven(t))
	if err != nil {
		t.Fatalf("new vm with explicit builtin catalog: %v", err)
	}

	if err := machine.Run(); err != nil {
		t.Fatalf("run vm: %v", err)
	}
	if got := machine.StackTop().String(); got != "7" {
		t.Fatalf("unexpected vm result: %s", got)
	}
}

func TestNewRejectsBytecodeWithMissingBuiltinInDefaultCatalog(t *testing.T) {
	bytecode := compileBuiltinCallBytecode(t, "seven")

	_, err := New(bytecode)
	if err == nil || !strings.Contains(err.Error(), `runtime builtin catalog missing "seven"`) {
		t.Fatalf("expected missing builtin error, got %v", err)
	}
}

func TestNewRunnerWithConfigBootstrapCoreComposesExplicitBuiltinCatalogWithCoreRuntime(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: true,
		RuntimeProfile: DefaultRuntimeProfile().
			SetBuiltinCatalog(testComposedBuiltinCatalog(t, testBuiltinCatalogWithSeven(t))),
	})
	if err != nil {
		t.Fatalf("new runner with core: %v", err)
	}

	value, err := runner.Run(`(when true (seven))`)
	if err != nil {
		t.Fatalf("core-loaded runner should compose custom builtins with default runtime: %v", err)
	}
	if value.String() != "7" {
		t.Fatalf("unexpected core-loaded runner result: %s", value.String())
	}
}

func TestNewRunnerWithConfigSupportsBuiltinAliasesWithoutMutatingDefaults(t *testing.T) {
	countBuiltin, ok := DefaultBuiltinCatalog().Lookup("count")
	if !ok {
		t.Fatal("default builtin catalog is missing count")
	}
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
		RuntimeProfile: testExactDefaultRuntimeProfileWithBuiltinCatalog(t, MustBuiltinCatalog([]BuiltinDefinition{
			{Name: "len", Builtin: countBuiltin},
		})),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`[(count [1 2 3]) (len [1 2])]`)
	if err != nil {
		t.Fatalf("runner should support aliased builtins: %v", err)
	}
	if value.String() != "[3 2]" {
		t.Fatalf("unexpected aliased builtin result: %s", value.String())
	}

	defaultCount, ok := DefaultBuiltinCatalog().lookup("count")
	if !ok {
		t.Fatal("default builtin catalog is missing count")
	}
	if defaultCount.Name != "count" {
		t.Fatalf("expected default count builtin name to remain count, got %q", defaultCount.Name)
	}
}

func TestExplicitBuiltinCatalogPropagatesToDosyncClosure(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: testExactDefaultRuntimeProfileWithBuiltinCatalog(t, testBuiltinCatalogWithSeven(t)),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`(dosync (seven))`)
	if err != nil {
		t.Fatalf("dosync closure should inherit explicit builtin catalog: %v", err)
	}
	if value.String() != "7" {
		t.Fatalf("unexpected dosync result: %s", value.String())
	}
}

func TestExplicitBuiltinCatalogPropagatesToMultimethodMethod(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: testExactDefaultRuntimeProfileWithBuiltinCatalog(t, testBuiltinCatalogWithSeven(t)),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`
		(defmulti dispatch (fn [x] :value))
		(defmethod dispatch :value [x] (seven))
		(dispatch nil)
	`)
	if err != nil {
		t.Fatalf("multimethod method should inherit explicit builtin catalog: %v", err)
	}
	if value.String() != "7" {
		t.Fatalf("unexpected multimethod result: %s", value.String())
	}
}

func TestExplicitBuiltinCatalogPropagatesToGoClosure(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: testExactDefaultRuntimeProfileWithBuiltinCatalog(t, testBuiltinCatalogWithSeven(t)),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`
		(let [ch (chan)]
		  (go (>! ch (seven)))
		  (<! ch))
	`)
	if err != nil {
		t.Fatalf("go closure should inherit explicit builtin catalog: %v", err)
	}
	if value.String() != "7" {
		t.Fatalf("unexpected go result: %s", value.String())
	}
}
