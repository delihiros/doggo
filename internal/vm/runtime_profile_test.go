package vm

import (
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func testExactDefaultRuntimeProfile() *RuntimeProfile {
	return testExactDefaultRuntimeProfileWithModules(nil)
}

func testExactDefaultRuntimeProfileWithModules(modules []RunnerModule) *RuntimeProfile {
	return DefaultRuntimeProfile().
		SetFeatures(nil).
		SetModules(modules)
}

func testComposedBuiltinCatalog(t *testing.T, catalog *BuiltinCatalog) *BuiltinCatalog {
	t.Helper()
	if catalog == nil {
		return DefaultBuiltinCatalog()
	}
	return MustBuiltinCatalog(DefaultBuiltinCatalog().Definitions(), catalog.Definitions())
}

func testExactDefaultRuntimeProfileWithBuiltinCatalog(t *testing.T, catalog *BuiltinCatalog) *RuntimeProfile {
	t.Helper()
	return testExactDefaultRuntimeProfile().
		SetBuiltinCatalog(testComposedBuiltinCatalog(t, catalog))
}

func TestNewRunnerWithConfigAllowsExplicitRuntimeProfile(t *testing.T) {
	customGroup := runtimeBindingGroup("profile-custom")
	profile := NewRuntimeProfile("test-profile").
		SetBuiltinCatalog(testBuiltinCatalogWithSeven(t)).
		setBindingCatalog(runnerRuntimeBindingCatalog{
			specs: []runtimeBindingSpec{
				{
					group:     customGroup,
					namespace: compiler.CoreNamespace,
					name:      "profile-value",
					build: func(*Runner) types.Value {
						return atom.NewNumberInt(11)
					},
				},
			},
		}).
		SetModules([]RunnerModule{
			NewRunnerModule("profile-custom", func(r *Runner) error {
				return r.installRuntimeBindingGroup(customGroup)
			}),
		}).
		SetBootstrapPolicy(
			NewRuntimeBootstrapPolicy(
				"test-bootstrap",
				NewRuntimeBootstrapStep("install-boot-value", func(r *Runner) error {
					r.initializeCoreGlobalIfMissing("boot-value", atom.NewNumberInt(13))
					return nil
				}),
			),
		)

	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  true,
		RuntimeProfile: profile,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`[profile-value boot-value (seven)]`)
	if err != nil {
		t.Fatalf("explicit runtime profile should install custom composition: %v", err)
	}
	if got := value.String(); got != "[11 13 7]" {
		t.Fatalf("unexpected runtime profile result: %s", got)
	}

	assertRunErrorContains(t, runner, "load-file", "undefined variable: load-file")

	assertRunErrorContains(t, runner, `(count [1 2 3])`, "undefined variable: count")
}

func TestNewRunnerWithConfigExplicitRuntimeProfileUsesPrecomposedBuiltinCatalogExactly(t *testing.T) {
	profile := NewRuntimeProfile("precomposed").
		SetBuiltinCatalog(MustBuiltinCatalog(DefaultBuiltinCatalog().Definitions())).
		SetModules([]RunnerModule{})

	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: profile,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`(count [1 2 3])`)
	if err != nil {
		t.Fatalf("precomposed builtin catalog should be accepted exactly: %v", err)
	}
	if got := value.String(); got != "3" {
		t.Fatalf("unexpected count result: %s", got)
	}
}
