package vm_test

import (
	"strings"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	vm "doggo/internal/vm"
)

func TestRuntimeProfilePublicAPIBuildsExactRuntime(t *testing.T) {
	profile := vm.NewRuntimeProfile("public").
		SetBuiltinCatalog(vm.MustBuiltinCatalog([]vm.BuiltinDefinition{
			{
				Name: "seven",
				Builtin: &vm.Builtin{Fn: func(args ...types.Value) types.Value {
					return atom.NewNumberInt(7)
				}},
			},
		})).
		SetModules([]vm.RunnerModule{})

	runner, err := vm.NewRunnerWithConfig(vm.RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: profile,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`(seven)`)
	if err != nil {
		t.Fatalf("public runtime profile should install explicit builtin catalog: %v", err)
	}
	if got := value.String(); got != "7" {
		t.Fatalf("unexpected seven result: %s", got)
	}

	if _, err := runner.Run(`(count [1 2 3])`); err == nil || !strings.Contains(err.Error(), "undefined variable: count") {
		t.Fatalf("public runtime profile should use exact builtin catalog, got %v", err)
	}
}

func TestRuntimeProfilePublicAPIComposesDefaultFeatures(t *testing.T) {
	profile := vm.NewRuntimeProfile("public-default-features").
		SetBuiltinCatalog(vm.DefaultBuiltinCatalog()).
		SetFeatures(vm.DefaultRuntimeFeatures())

	runner, err := vm.NewRunnerWithConfig(vm.RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: profile,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`load-file`)
	if err != nil {
		t.Fatalf("default runtime features should be usable from public exact profile: %v", err)
	}
	if got := value.String(); got != "#<builtin:load-file>" {
		t.Fatalf("unexpected load-file binding result: %s", got)
	}
}

func TestRuntimeProfilePublicAPIComposesDefaultBootstrapPolicyWithoutHiddenBindingCatalog(t *testing.T) {
	profile := vm.NewRuntimeProfile("public-default-bootstrap").
		SetBuiltinCatalog(vm.DefaultBuiltinCatalog()).
		SetFeatures(vm.DefaultRuntimeFeatures()).
		SetBootstrapPolicy(vm.DefaultRuntimeBootstrapPolicy())

	runner, err := vm.NewRunnerWithConfig(vm.RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  true,
		RuntimeProfile: profile,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`(take 2 (repeat 9))`)
	if err != nil {
		t.Fatalf("default bootstrap policy should be usable from public exact profile: %v", err)
	}
	if got := value.String(); got != "(9 9)" {
		t.Fatalf("unexpected repeat result: %s", got)
	}
}

func TestRuntimeProfilePublicAPIRejectsUnknownRuntimeFeature(t *testing.T) {
	profile := vm.NewRuntimeProfile("public-unknown-feature").
		SetBuiltinCatalog(vm.DefaultBuiltinCatalog()).
		SetFeatures([]vm.RuntimeFeature{"no-such-feature"})

	_, err := vm.NewRunnerWithConfig(vm.RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: profile,
	})
	if err == nil || !strings.Contains(err.Error(), `unknown runtime feature "no-such-feature"`) {
		t.Fatalf("expected unknown runtime feature error, got %v", err)
	}
}
