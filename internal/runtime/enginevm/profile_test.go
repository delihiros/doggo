package enginevm

import (
	"reflect"
	"strings"
	"testing"

	"doggo/internal/runtime/composition"
	"doggo/internal/runtime/profile"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/vm"
	"doggo/internal/vm/engine"
)

type testEngineRunnerModule struct {
	name  string
	group string
}

func (m testEngineRunnerModule) Name() string {
	return m.name
}

func (m testEngineRunnerModule) Install(installer engine.RuntimeModuleInstaller) error {
	return installer.InstallRuntimeBindingGroup(m.group)
}

type testEngineBootstrapPolicy struct {
	name      string
	stepNames []string
	source    string
}

func (p testEngineBootstrapPolicy) Name() string {
	return p.name
}

func (p testEngineBootstrapPolicy) StepNames() []string {
	return append([]string(nil), p.stepNames...)
}

func (p testEngineBootstrapPolicy) Bootstrap(bootstrapper engine.RuntimeBootstrapper) error {
	_, err := bootstrapper.Run(p.source)
	return err
}

func TestResolveExactRuntimeProfileUsesPracticalPureStandardResolution(t *testing.T) {
	engineCfg := composition.StandardEngineConfig(composition.StandardConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
		Profile:       profile.Default(),
	})

	exactProfile, err := resolveExactRuntimeProfile(engineCfg, profile.Default())
	if err != nil {
		t.Fatal(err)
	}
	if exactProfile == nil {
		t.Fatal("expected exact runtime profile")
	}
	if exactProfile.BuiltinCatalog() == nil {
		t.Fatal("expected builtin catalog")
	}
	for _, name := range []string{"count", "first", "rest", "nth"} {
		if _, ok := exactProfile.BuiltinCatalog().Lookup(name); !ok {
			t.Fatalf("expected standard runtime builtin %q", name)
		}
	}
	for _, name := range []string{"slurp", "spit", "print", "println", "prn", "log!", "io-file*", "os-getenv*", "os-setenv*", "os-env*", "time-now*", "time-sleep*", "process-start*", "process-run*"} {
		if _, ok := exactProfile.BuiltinCatalog().Lookup(name); ok {
			t.Fatalf("did not expect effectful builtin %q in standard resolution", name)
		}
	}
}

func TestResolveExactRuntimeProfilePreservesExplicitEngineComposition(t *testing.T) {
	engineCfg := engine.RuntimeConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{"lib"},
		BootstrapCore: true,
		BuiltinCatalog: engine.NewBuiltinCatalog(nil, []vm.BuiltinDefinition{
			{
				Name: "seven",
				Builtin: &vm.Builtin{Fn: func(args ...types.Value) types.Value {
					return atom.NewNumberInt(7)
				}},
			},
		}),
		Features: []engine.RuntimeFeature{
			engine.RuntimeFeatureMacroExpand,
			engine.RuntimeFeatureFileLoad,
		},
		Modules: []engine.RunnerModule{
			testEngineRunnerModule{name: "macroexpand-group", group: "macro-expand"},
		},
		BootstrapPolicy: testEngineBootstrapPolicy{
			name:      "test-bootstrap",
			stepNames: []string{"install-test-bootstrap"},
			source:    `(def boot-value 13)`,
		},
	}

	exactProfile, err := resolveExactRuntimeProfile(engineCfg, profile.New("explicit"))
	if err != nil {
		t.Fatalf("exact runtime profile: %v", err)
	}
	if exactProfile == nil {
		t.Fatal("expected exact runtime profile")
	}
	if got := exactProfile.Features(); !reflect.DeepEqual(got, []vm.RuntimeFeature{
		vm.RuntimeFeatureMacroExpand,
		vm.RuntimeFeatureFileLoad,
	}) {
		t.Fatalf("exact runtime features = %v", got)
	}
	if exactProfile.Modules() == nil || len(exactProfile.Modules()) != 1 {
		t.Fatalf("expected one adapted engine module, got %d", len(exactProfile.Modules()))
	}
	if got := exactProfile.BootstrapPolicy().StepNames(); !reflect.DeepEqual(got, []string{
		"install-core-runtime-contract",
		"install-test-bootstrap",
	}) {
		t.Fatalf("adapted bootstrap steps = %v", got)
	}
	if exactProfile.BuiltinCatalog() == nil {
		t.Fatal("expected builtin catalog")
	}
	if _, ok := exactProfile.BuiltinCatalog().Lookup("seven"); !ok {
		t.Fatal("expected explicit builtin seven to be preserved")
	}
	if _, ok := exactProfile.BuiltinCatalog().Lookup("count"); ok {
		t.Fatal("did not expect standard builtin count in explicit engine catalog")
	}

	runner, err := vm.NewRunnerWithConfig(vm.RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  true,
		RuntimeProfile: exactProfile,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run("macroexpand")
	if err != nil {
		t.Fatalf("module-adapted macroexpand lookup failed: %v", err)
	}
	if got := value.String(); got != "#<builtin:macroexpand>" {
		t.Fatalf("unexpected macroexpand binding result: %s", got)
	}

	value, err = runner.Run("boot-value")
	if err != nil {
		t.Fatalf("bootstrap-adapted boot-value lookup failed: %v", err)
	}
	if got := value.String(); got != "13" {
		t.Fatalf("unexpected bootstrap value: %s", got)
	}

	if _, err := runner.Run(`(repeat 1)`); err == nil || !strings.Contains(err.Error(), "undefined variable: repeat") {
		t.Fatalf("expected custom bootstrap policy to skip default doggo.core load, got %v", err)
	}
}
