package deps

import (
	"fmt"
	"strings"

	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
)

type AuditSummary struct {
	Blocking    []DependencyAuditFinding
	NonBlocking []DependencyAuditFinding
}

type Service struct {
	ResolveProjectRoot  func() (string, bool, error)
	PrepareSync         func(projectRoot string, sources []string) (*generation.PreparedGeneration, error)
	PrepareSelectedSync func(projectRoot, selected string, previous *project.LockFile, direct []project.LockDep) (*generation.PreparedGeneration, error)
	ResolvePackage      func(projectRoot, packageSource, registryOverride string) (string, project.RegistryPackage, error)
	AddDependency       func(projectRoot, source string) error
	RemoveDependency    func(projectRoot, source string) error
	ListDependencies    func(projectRoot string) ([]string, error)
	Outdated            func(projectRoot string) ([]DependencyOutdatedStatus, error)
	Auditor             Auditor
}

func (s *Service) Add(source string) error {
	projectRoot, err := s.projectRoot()
	if err != nil {
		return err
	}
	addDependency := s.AddDependency
	if addDependency == nil {
		addDependency = AddDependency
	}
	return addDependency(projectRoot, source)
}

func (s *Service) Remove(source string) error {
	projectRoot, err := s.projectRoot()
	if err != nil {
		return err
	}
	removeDependency := s.RemoveDependency
	if removeDependency == nil {
		removeDependency = RemoveDependency
	}
	return removeDependency(projectRoot, source)
}

func (s *Service) List() ([]string, error) {
	projectRoot, err := s.projectRoot()
	if err != nil {
		return nil, err
	}
	listDependencies := s.ListDependencies
	if listDependencies == nil {
		listDependencies = ListDependencies
	}
	return listDependencies(projectRoot)
}

func (s *Service) ResolveSource(packageSource, registryOverride string) (string, project.RegistryPackage, error) {
	projectRoot, err := s.projectRoot()
	if err != nil {
		return "", project.RegistryPackage{}, err
	}
	resolvePackage := s.ResolvePackage
	if resolvePackage == nil {
		resolvePackage = ResolvePackageSource
	}
	return resolvePackage(projectRoot, packageSource, registryOverride)
}

func (s *Service) OutdatedStatus() ([]DependencyOutdatedStatus, error) {
	projectRoot, err := s.projectRoot()
	if err != nil {
		return nil, err
	}
	outdated := s.Outdated
	if outdated == nil {
		outdated = OutdatedDependencies
	}
	return outdated(projectRoot)
}

func (s *Service) UpdateDependencies(targetSource string, updateAll bool) (*project.LockFile, []DependencyUpdateResult, error) {
	projectRoot, err := s.projectRoot()
	if err != nil {
		return nil, nil, err
	}
	return runUpdateWorkflow(projectRoot, updateConfig{
		targetSource:        targetSource,
		updateAll:           updateAll,
		loadState:           loadModAndMatchingLock,
		prepareSync:         s.PrepareSync,
		prepareSelectedSync: s.PrepareSelectedSync,
		audit:               s.Auditor.AuditPreparedLock,
	})
}

func (s *Service) Audit() (*AuditSummary, error) {
	projectRoot, err := s.projectRoot()
	if err != nil {
		return nil, err
	}
	findings, err := s.Auditor.Audit(projectRoot)
	if err != nil {
		return nil, err
	}

	summary := &AuditSummary{
		Blocking:    make([]DependencyAuditFinding, 0, len(findings)),
		NonBlocking: make([]DependencyAuditFinding, 0, len(findings)),
	}
	for _, finding := range findings {
		if isHighSeverity(finding) {
			summary.Blocking = append(summary.Blocking, finding)
			continue
		}
		summary.NonBlocking = append(summary.NonBlocking, finding)
	}
	return summary, nil
}

func (s *Service) AuditPreparedLock(projectRoot, stagedRoot string, lock *project.LockFile) ([]DependencyAuditFinding, error) {
	return s.Auditor.AuditPreparedLock(projectRoot, stagedRoot, lock)
}

func (s *Service) projectRoot() (string, error) {
	resolveProjectRoot := s.ResolveProjectRoot
	if resolveProjectRoot == nil {
		resolveProjectRoot = func() (string, bool, error) {
			return project.ResolveRoot("")
		}
	}
	projectRoot, found, err := resolveProjectRoot()
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%s not found. Run 'doggo mod init' first", project.ModFileName)
	}
	return projectRoot, nil
}

func isHighSeverity(finding DependencyAuditFinding) bool {
	return strings.EqualFold(strings.TrimSpace(finding.Severity), "high")
}
