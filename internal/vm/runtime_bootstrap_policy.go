package vm

import "fmt"

type RuntimeBootstrapStep interface {
	Name() string
	Run(*Runner) error
}

type runtimeBootstrapStep struct {
	name string
	run  func(*Runner) error
}

func (step runtimeBootstrapStep) Name() string {
	return step.name
}

func (step runtimeBootstrapStep) Run(r *Runner) error {
	if step.run == nil {
		return nil
	}
	return step.run(r)
}

func NewRuntimeBootstrapStep(name string, run func(*Runner) error) RuntimeBootstrapStep {
	return runtimeBootstrapStep{name: name, run: run}
}

type RuntimeBootstrapPolicy interface {
	Name() string
	StepNames() []string
	Bootstrap(*Runner) error
}

type runtimeBootstrapPolicy struct {
	name  string
	steps []RuntimeBootstrapStep
}

func (policy runtimeBootstrapPolicy) Name() string {
	return policy.name
}

func (policy runtimeBootstrapPolicy) StepNames() []string {
	names := make([]string, 0, len(policy.steps))
	for i, step := range policy.steps {
		if step == nil {
			names = append(names, fmt.Sprintf("step[%d]", i))
			continue
		}
		name := step.Name()
		if name == "" {
			name = fmt.Sprintf("step[%d]", i)
		}
		names = append(names, name)
	}
	return names
}

func (policy runtimeBootstrapPolicy) Bootstrap(r *Runner) error {
	for i, step := range policy.steps {
		if step == nil {
			return fmt.Errorf("runtime bootstrap policy %q: nil step[%d]", policy.name, i)
		}
		name := step.Name()
		if name == "" {
			name = fmt.Sprintf("step[%d]", i)
		}
		if err := step.Run(r); err != nil {
			return fmt.Errorf("runtime bootstrap policy %q step %q: %w", policy.name, name, err)
		}
	}
	return nil
}

func NewRuntimeBootstrapPolicy(name string, steps ...RuntimeBootstrapStep) RuntimeBootstrapPolicy {
	cloned := make([]RuntimeBootstrapStep, len(steps))
	copy(cloned, steps)
	return runtimeBootstrapPolicy{name: name, steps: cloned}
}

func DefaultRuntimeBootstrapPolicy() RuntimeBootstrapPolicy {
	return NewRuntimeBootstrapPolicy(
		"default-core-bootstrap",
		NewRuntimeBootstrapStep("install-core-runtime-contract", func(r *Runner) error {
			return r.installRuntimeBootstrapPhase(runtimeBootstrapPhaseCoreContract)
		}),
		NewRuntimeBootstrapStep("load-doggo-core", func(r *Runner) error {
			_, err := r.Run(`(require '[doggo.core :refer :all])`)
			return err
		}),
	)
}
