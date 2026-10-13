package vm

import (
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/types/atom"
)

func TestNewRunnerWithConfigAllowsExplicitCoreBootstrapPolicy(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: true,
		RuntimeProfile: testExactDefaultRuntimeProfile().
			SetBootstrapPolicy(NewRuntimeBootstrapPolicy(
				"test-bootstrap",
				NewRuntimeBootstrapStep("install-test-bootstrap", func(r *Runner) error {
					r.initializeCoreGlobalIfMissing("test-bootstrap", atom.NewNumberInt(7))
					return nil
				}),
			)),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run("test-bootstrap")
	if err != nil {
		t.Fatalf("test-bootstrap lookup failed: %v", err)
	}
	if got := value.String(); got != "7" {
		t.Fatalf("unexpected bootstrap value: %s", got)
	}

	assertRunErrorContains(t, runner, `(repeat 1)`, "undefined variable: repeat")
}

func TestInstallRuntimeBootstrapPhasePreservesExistingCoreContractBindings(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: testExactDefaultRuntimeProfile(),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	custom := atom.NewString("custom-require")
	sym := runner.symbolTable.DefineGlobalInNamespace(compiler.CoreNamespace, "require")
	runner.getOrCreateExecutionSession().globals[sym.Index] = custom

	if err := runner.installRuntimeBootstrapPhase(runtimeBootstrapPhaseCoreContract); err != nil {
		t.Fatalf("install bootstrap phase: %v", err)
	}

	value, err := runner.Run("require")
	if err != nil {
		t.Fatalf("require lookup failed: %v", err)
	}
	if !value.Equals(custom) {
		t.Fatalf("bootstrap phase overwrote explicit core contract binding: got %s", value.String())
	}
}

func TestInstallRuntimeBindingGroupReplacesExistingBindings(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:    t.TempDir(),
		LoadPaths:      []string{},
		BootstrapCore:  false,
		RuntimeProfile: testExactDefaultRuntimeProfile(),
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	custom := atom.NewString("custom-macroexpand")
	sym := runner.symbolTable.DefineGlobalInNamespace(compiler.CoreNamespace, "macroexpand")
	runner.getOrCreateExecutionSession().globals[sym.Index] = custom

	if err := runner.installRuntimeBindingGroup(runtimeBindingGroupMacroExpand); err != nil {
		t.Fatalf("install binding group: %v", err)
	}

	value, err := runner.Run("macroexpand")
	if err != nil {
		t.Fatalf("macroexpand lookup failed: %v", err)
	}
	if value.Equals(custom) {
		t.Fatalf("binding group should replace existing binding, got preserved value %s", value.String())
	}
	if got := value.String(); got != "#<builtin:macroexpand>" {
		t.Fatalf("unexpected macroexpand binding after group install: %s", got)
	}
}
