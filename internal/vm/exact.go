package vm

import (
	"doggo/internal/project"
	"doggo/internal/vm/engine"
)

type ExactEngineExecution struct {
	Runner  *Runner
	Sources *engine.SourceRegistry
}

func NewExactEngineRunner(
	exactProfile *RuntimeProfile,
	bootstrapCore bool,
	namespaces *engine.NamespaceServices,
	generationSources []*project.GenerationSource,
	lock *project.LockFile,
) (ExactEngineExecution, error) {
	return newExactEngineRunner(exactProfile, bootstrapCore, namespaces, generationSources, lock)
}
