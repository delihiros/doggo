package publish

import (
	"doggo/internal/project"
	toolchaindeps "doggo/internal/toolchain/deps"
)

type Result struct {
	Package      project.RegistryPackage
	RegistryPath string
	ArchivePath  string
}

type Service struct {
	LoadModFile     func(path string) (*project.ModFile, error)
	ResolveRegistry func(projectRoot, override string, forWrite bool) (string, error)
	LoadRegistry    func(path string) (*project.RegistryFile, error)
	WriteRegistry   func(path string, registry *project.RegistryFile) error
	CreateArchive   func(projectRoot, name, version string) (string, error)
}

func (s Service) Run(projectRoot string, opts Options) (*Result, error) {
	loadModFile := s.LoadModFile
	if loadModFile == nil {
		loadModFile = project.LoadModFile
	}
	resolveRegistry := s.ResolveRegistry
	if resolveRegistry == nil {
		resolveRegistry = toolchaindeps.ResolveRegistryPath
	}
	loadRegistry := s.LoadRegistry
	if loadRegistry == nil {
		loadRegistry = project.LoadRegistryFile
	}
	writeRegistry := s.WriteRegistry
	if writeRegistry == nil {
		writeRegistry = project.WriteRegistryFile
	}
	createArchive := s.CreateArchive
	if createArchive == nil {
		createArchive = createPublishArchive
	}
	return runPublishWorkflow(projectRoot, opts, publishConfig{
		loadModFile:     loadModFile,
		resolveRegistry: resolveRegistry,
		loadRegistry:    loadRegistry,
		writeRegistry:   writeRegistry,
		createArchive:   createArchive,
	})
}
