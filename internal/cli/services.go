package cli

import (
	"fmt"
	"strings"

	benchdom "doggo/internal/bench"
	"doggo/internal/project"
	releasebundle "doggo/internal/release"
	toolchainbench "doggo/internal/toolchain/bench"
	toolchainbuild "doggo/internal/toolchain/build"
	toolchaindeps "doggo/internal/toolchain/deps"
	"doggo/internal/toolchain/generation"
	toolchainnewproject "doggo/internal/toolchain/newproject"
	toolchainpublish "doggo/internal/toolchain/publish"
	toolchainrelease "doggo/internal/toolchain/release"
	toolchainsync "doggo/internal/toolchain/sync"
	toolchaintester "doggo/internal/toolchain/tester"
)

var (
	defaultResolveProjectRoot = func() (string, bool, error) {
		return project.ResolveRoot("")
	}
	defaultOpenGenerationSession = project.OpenGenerationSession
	defaultInitProject           = project.InitModFile
	defaultDepsLoadModFile       = project.LoadModFile
	defaultDepsAuditResolvedLock = toolchaindeps.AuditResolvedLock
	defaultGenerationService     = generationService{
		PrepareSync:         (generation.Service{}).PrepareSync,
		PrepareSelectedSync: (generation.Service{}).PrepareSelectedSync,
		PrepareCurrentBuild: (generation.Service{}).PrepareCurrentBuild,
	}
)

type generationService struct {
	PrepareSync         func(projectRoot string, sources []string) (*generation.PreparedGeneration, error)
	PrepareSelectedSync func(projectRoot, selected string, previous *project.LockFile, direct []project.LockDep) (*generation.PreparedGeneration, error)
	PrepareCurrentBuild func(projectRoot string) (*generation.PreparedGeneration, error)
}

func defaultDepsAuditor() toolchaindeps.Auditor {
	return toolchaindeps.Auditor{
		LoadModFile: defaultDepsLoadModFile,
		AuditLock:   defaultDepsAuditResolvedLock,
	}
}

type appServices struct {
	InitProject func() error
	Build       func(withSync bool) error
	Sync        func() error
	Deps        *toolchaindeps.Service
	Release     *toolchainrelease.Service
	NewProject  func(toolchainnewproject.Options) (*toolchainnewproject.Result, error)
	Publish     func(registryPath, source string, allowOverwrite bool) (*toolchainpublish.Result, error)
	Test        func(paths []string, namespaces []string, reportFormat, reportFile string) (*toolchaintester.Report, error)
	BenchList   func() ([]benchdom.Scenario, error)
	BenchRun    func(opts toolchainbench.RunOptions) (*toolchainbench.RunReport, error)
}

func defaultServices() appServices {
	syncService := toolchainsync.Service{PrepareSync: defaultGenerationService.PrepareSync}
	depsAuditor := defaultDepsAuditor()
	depsService := &toolchaindeps.Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return defaultResolveProjectRoot()
		},
		ResolvePackage:      toolchaindeps.ResolvePackageSource,
		AddDependency:       toolchaindeps.AddDependency,
		RemoveDependency:    toolchaindeps.RemoveDependency,
		ListDependencies:    toolchaindeps.ListDependencies,
		Outdated:            toolchaindeps.OutdatedDependencies,
		PrepareSync:         defaultGenerationService.PrepareSync,
		PrepareSelectedSync: defaultGenerationService.PrepareSelectedSync,
		Auditor:             depsAuditor,
	}
	publishService := toolchainpublish.Service{
		ResolveRegistry: toolchaindeps.ResolveRegistryPath,
	}
	testService := toolchaintester.Service{
		OpenGenerationSession: defaultOpenGenerationSession,
	}
	benchService := toolchainbench.Service{
		OpenGenerationSession: defaultOpenGenerationSession,
	}
	newProjectService := toolchainnewproject.Service{}
	releaseService := &toolchainrelease.Service{
		OpenGenerationSession: defaultOpenGenerationSession,
		ResolveProjectRoot: func() (string, bool, error) {
			return defaultResolveProjectRoot()
		},
		GenerateReleaseBundle:      releasebundle.GenerateReleaseBundle,
		CheckReleaseIntegrityFiles: releasebundle.CheckReleaseIntegrityFiles,
		GenerateKeyPair:            releasebundle.GenerateKeyPair,
		AuditDependencies: func(projectRoot string) ([]toolchaindeps.DependencyAuditFinding, error) {
			return defaultDepsAuditor().Audit(projectRoot)
		},
	}
	buildService := toolchainbuild.Service{
		PrepareSync:         defaultGenerationService.PrepareSync,
		PrepareCurrentBuild: defaultGenerationService.PrepareCurrentBuild,
		AuditPrepared: func(projectRoot, stagedRoot string, lock *project.LockFile) (int, error) {
			findings, err := depsAuditor.AuditPreparedLock(projectRoot, stagedRoot, lock)
			if err != nil {
				return 0, err
			}
			return countHighSeverityFindings(findings), nil
		},
	}
	return appServices{
		InitProject: func() error {
			projectRoot, found, err := defaultResolveProjectRoot()
			if err != nil {
				return err
			}
			if found {
				return fmt.Errorf("%s already exists at %s", project.ModFileName, projectRoot)
			}
			return defaultInitProject(projectRoot)
		},
		Build: func(withSync bool) error {
			projectRoot, found, err := defaultResolveProjectRoot()
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("%s not found. Run 'doggo mod init' first", project.ModFileName)
			}
			_, err = buildService.Run(projectRoot, withSync)
			return err
		},
		Sync: func() error {
			projectRoot, found, err := defaultResolveProjectRoot()
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("%s not found. Run 'doggo mod init' first", project.ModFileName)
			}
			_, err = syncService.Run(projectRoot)
			return err
		},
		Deps:    depsService,
		Release: releaseService,
		NewProject: func(opts toolchainnewproject.Options) (*toolchainnewproject.Result, error) {
			return newProjectService.Run(opts)
		},
		Publish: func(registryPath, source string, allowOverwrite bool) (*toolchainpublish.Result, error) {
			projectRoot, found, err := defaultResolveProjectRoot()
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, fmt.Errorf("%s not found. Run 'doggo mod init' first", project.ModFileName)
			}
			return publishService.Run(projectRoot, toolchainpublish.Options{
				RegistryPath:   registryPath,
				Source:         source,
				AllowOverwrite: allowOverwrite,
			})
		},
		Test: func(paths []string, namespaces []string, reportFormat, reportFile string) (*toolchaintester.Report, error) {
			return testService.Run(toolchaintester.RunOptions{
				Paths:      paths,
				Namespaces: namespaces,
			})
		},
		BenchList: func() ([]benchdom.Scenario, error) {
			return benchService.ListScenarios(), nil
		},
		BenchRun: func(opts toolchainbench.RunOptions) (*toolchainbench.RunReport, error) {
			return benchService.Run(opts)
		},
	}
}

func countHighSeverityFindings(findings []toolchaindeps.DependencyAuditFinding) int {
	highCount := 0
	for _, finding := range findings {
		if strings.EqualFold(strings.TrimSpace(finding.Severity), "high") {
			highCount++
		}
	}
	return highCount
}
