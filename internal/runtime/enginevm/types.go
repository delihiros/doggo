package enginevm

import (
	"doggo/internal/project"
	"doggo/internal/runtime/profile"
	"doggo/internal/vm"
	"doggo/internal/vm/engine"
)

type Config struct {
	ProjectRoot       string
	LoadPaths         []string
	GenerationSources []*project.GenerationSource
	Lock              *project.LockFile
	BootstrapCore     bool
	Profile           *profile.Profile
}

type ExactConfig struct {
	Engine       engine.RuntimeConfig
	ExactProfile *vm.RuntimeProfile
}

type GraphHandles struct {
	Engine     engine.RuntimeConfig
	Namespaces *engine.NamespaceServices
	Sources    *engine.SourceRegistry
	Basis      *engine.BasisServices
}
