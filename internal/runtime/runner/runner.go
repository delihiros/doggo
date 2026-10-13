package runner

import (
	"os"
	"strings"

	"doggo/internal/project"
	"doggo/internal/runtime/builtins"
	"doggo/internal/runtime/composition"
	"doggo/internal/runtime/profile"
	"doggo/internal/runtime/services"
	"doggo/internal/types"
)

type Runner struct {
	profile  *profile.Profile
	services *services.ServiceSet
	vmRunner any
}

type Config struct {
	ProjectRoot       string
	LoadPaths         []string
	GenerationSources []*project.GenerationSource
	Lock              *project.LockFile
	BootstrapCore     bool
	Profile           *profile.Profile
}

func defaultProjectRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func defaultLoadPaths(projectRoot string) []string {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		root = defaultProjectRoot()
	}
	paths := make([]string, 0, 1)
	if root != "" {
		paths = append(paths, root)
	} else {
		paths = append(paths, ".")
	}
	return paths
}

func New(cfg Config) (*Runner, error) {
	if cfg.Profile == nil {
		cfg.Profile = profile.Default()
	}
	if err := builtins.ValidateProfile(cfg.Profile); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ProjectRoot) == "" {
		cfg.ProjectRoot = defaultProjectRoot()
	}
	if cfg.LoadPaths == nil {
		cfg.LoadPaths = defaultLoadPaths(cfg.ProjectRoot)
	}
	engineCfg := composition.StandardEngineConfig(composition.StandardConfig{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         cfg.LoadPaths,
		GenerationSources: cfg.GenerationSources,
		Lock:              cfg.Lock,
		BootstrapCore:     cfg.BootstrapCore,
		Profile:           cfg.Profile,
	})
	return newWithGraph(cfg.Profile, services.NewGraph(engineCfg))
}

func (r *Runner) EvalString(src string) (types.Value, error) {
	return r.services.Execution.EvalString(src)
}

func (r *Runner) VMRunner() any {
	if r == nil {
		return nil
	}
	return r.vmRunner
}

func (r *Runner) Namespaces() services.NamespaceServices {
	if r == nil || r.services == nil {
		return nil
	}
	return r.services.Namespaces
}

func (r *Runner) Sources() services.SourceRegistry {
	if r == nil || r.services == nil {
		return nil
	}
	return r.services.Sources
}

func (r *Runner) Basis() services.BasisServices {
	if r == nil || r.services == nil {
		return nil
	}
	return r.services.Basis
}
