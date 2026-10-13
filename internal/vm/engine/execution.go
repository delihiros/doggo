package engine

import (
	"doggo/internal/project"
	"doggo/internal/types"
)

// RuntimeFeature identifies an optional runtime capability.
type RuntimeFeature string

const (
	RuntimeFeatureMacroExpand       RuntimeFeature = "macro-expand"
	RuntimeFeatureFileLoad          RuntimeFeature = "file-load"
	RuntimeFeatureREPLIntrospection RuntimeFeature = "repl-introspection"
	RuntimeFeatureLazySeq           RuntimeFeature = "lazy-seq"
	RuntimeFeatureObservability     RuntimeFeature = "observability"
	RuntimeFeatureBasis             RuntimeFeature = "basis"
	RuntimeFeatureCLI               RuntimeFeature = "cli"
)

func DefaultRuntimeFeatures() []RuntimeFeature {
	return []RuntimeFeature{
		RuntimeFeatureMacroExpand,
		RuntimeFeatureFileLoad,
		RuntimeFeatureREPLIntrospection,
		RuntimeFeatureLazySeq,
		RuntimeFeatureObservability,
		RuntimeFeatureBasis,
		RuntimeFeatureCLI,
	}
}

// RuntimeModuleInstaller is the minimal execution host contract needed by runtime modules.
type RuntimeModuleInstaller interface {
	InstallRuntimeBindingGroup(group string) error
}

// RuntimeBootstrapper is the minimal execution host contract needed by bootstrap policies.
type RuntimeBootstrapper interface {
	Run(source string) (types.Value, error)
}

// RunnerModule installs a cohesive runtime feature set into an execution host.
type RunnerModule interface {
	Name() string
	Install(RuntimeModuleInstaller) error
}

// RuntimeBootstrapPolicy installs the core runtime composition steps.
type RuntimeBootstrapPolicy interface {
	Name() string
	StepNames() []string
	Bootstrap(RuntimeBootstrapper) error
}

// RuntimeConfig describes the runtime composition inputs used by engine callers.
type RuntimeConfig struct {
	ProjectRoot       string
	LoadPaths         []string
	GenerationSources []*project.GenerationSource
	Lock              *project.LockFile
	BootstrapCore     bool
	BuiltinCatalog    *BuiltinCatalog
	Features          []RuntimeFeature
	Modules           []RunnerModule
	BootstrapPolicy   RuntimeBootstrapPolicy
}

type BuiltinCatalogMode string

const (
	BuiltinCatalogModeExact           BuiltinCatalogMode = "exact"
	BuiltinCatalogModeStandardRuntime BuiltinCatalogMode = "standard-runtime"
)

type BuiltinCatalogDescriptor struct {
	Mode BuiltinCatalogMode
}

type BuiltinCatalog struct {
	names []string
	data  any
}

func NewBuiltinCatalog(names []string, data any) *BuiltinCatalog {
	return &BuiltinCatalog{
		names: append([]string(nil), names...),
		data:  data,
	}
}

func (c *BuiltinCatalog) Names() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.names...)
}

func (c *BuiltinCatalog) Data() any {
	if c == nil {
		return nil
	}
	return c.data
}
