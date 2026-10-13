package release

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"doggo/internal/project"
	releasebundle "doggo/internal/release"
	toolchaindeps "doggo/internal/toolchain/deps"
)

const (
	DefaultKeyDirectory   = "dist/keys"
	DefaultPrivateKeyPath = DefaultKeyDirectory + "/" + releasebundle.PrivateKeyFileName
	DefaultPublicKeyPath  = DefaultKeyDirectory + "/" + releasebundle.PublicKeyFileName
)

type Service struct {
	ResolveProjectRoot         func() (string, bool, error)
	OpenGenerationSession      func(scriptPath string) (*project.GenerationSession, error)
	GenerateReleaseBundle      func(projectRoot string, binary project.GenerationBinarySnapshot, artifactPaths []string, outDir, privateKeyPath string) (*releasebundle.ReleaseBundleOutputs, error)
	CheckReleaseIntegrityFiles func(projectRoot, outDir, publicKeyPath string, requireSignature bool) error
	GenerateKeyPair            func(keyDir string) error
	AuditDependencies          func(projectRoot string) ([]toolchaindeps.DependencyAuditFinding, error)
}

type BundleOptions struct {
	Artifacts      string
	OutDir         string
	PrivateKeyPath string
	PrivateKeySet  bool
	AllowUnsigned  bool
}

type VerifyOptions struct {
	OutDir        string
	PublicKeyPath string
	PublicKeySet  bool
	AllowUnsigned bool
}

type GateOptions struct {
	OutDir        string
	PublicKeyPath string
	PublicKeySet  bool
	AllowUnsigned bool
	SkipVerify    bool
}

type KeygenOptions struct {
	KeyDir string
}

type KeyPairPaths struct {
	PrivateKeyPath string
	PublicKeyPath  string
}

func (s Service) RunBundle(opts BundleOptions) (_ *releasebundle.ReleaseBundleOutputs, err error) {
	projectRoot, err := s.projectRootWithFallback()
	if err != nil {
		return nil, err
	}

	artifactList := releasebundle.ParseArtifactList(opts.Artifacts)
	requireSignature := !opts.AllowUnsigned
	privateKeyPath := s.resolveKeyPathWithProjectDefault(projectRoot, opts.PrivateKeyPath, DefaultPrivateKeyPath, opts.PrivateKeySet)
	if requireSignature && strings.TrimSpace(privateKeyPath) == "" {
		return nil, fmt.Errorf("release bundle signing requires a private key. Set DOGGO_RELEASE_PRIVATE_KEY or run 'doggo release keygen'")
	}
	openSession := s.OpenGenerationSession
	if openSession == nil {
		openSession = project.OpenGenerationSession
	}
	session, err := openSession(project.ModPath(projectRoot))
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("generation session is nil")
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	binary, err := session.BinarySnapshot()
	if err != nil {
		return nil, err
	}

	generateReleaseBundle := s.GenerateReleaseBundle
	if generateReleaseBundle == nil {
		generateReleaseBundle = releasebundle.GenerateReleaseBundle
	}
	return generateReleaseBundle(projectRoot, binary, artifactList, opts.OutDir, privateKeyPath)
}

func (s Service) RunVerify(opts VerifyOptions) error {
	projectRoot, err := s.projectRootWithFallback()
	if err != nil {
		return err
	}

	requireSignature := !opts.AllowUnsigned
	publicKeyPath := s.resolveKeyPathWithProjectDefault(projectRoot, opts.PublicKeyPath, DefaultPublicKeyPath, opts.PublicKeySet)
	if requireSignature && strings.TrimSpace(publicKeyPath) == "" {
		return fmt.Errorf("signature verification requires a public key. Set DOGGO_RELEASE_PUBLIC_KEY or run 'doggo release keygen'")
	}

	checkReleaseIntegrityFiles := s.CheckReleaseIntegrityFiles
	if checkReleaseIntegrityFiles == nil {
		checkReleaseIntegrityFiles = releasebundle.CheckReleaseIntegrityFiles
	}
	return checkReleaseIntegrityFiles(projectRoot, opts.OutDir, publicKeyPath, requireSignature)
}

func (s Service) RunGate(opts GateOptions) error {
	projectRoot, err := s.projectRootRequired()
	if err != nil {
		return err
	}

	auditDependencies := s.AuditDependencies
	if auditDependencies == nil {
		auditDependencies = toolchaindeps.Auditor{AuditLock: toolchaindeps.AuditResolvedLock}.Audit
	}
	findings, err := auditDependencies(projectRoot)
	if err != nil {
		return err
	}
	if countAuditFindingsByCode(findings, "vuln-scan-unavailable") > 0 {
		return fmt.Errorf("release gate blocked: required vulnerability scan unavailable. Install golang.org/x/vuln/cmd/govulncheck and rerun 'doggo release gate'")
	}
	highCount := countHighSeverityAuditFindings(findings)
	if highCount > 0 {
		return fmt.Errorf("release gate blocked by dependency audit: %d high-severity issue(s). Run 'doggo deps audit'", highCount)
	}

	requireSignature := !opts.AllowUnsigned
	publicKeyPath := s.resolveKeyPathWithProjectDefault(projectRoot, opts.PublicKeyPath, DefaultPublicKeyPath, opts.PublicKeySet)
	if !opts.SkipVerify && requireSignature && strings.TrimSpace(publicKeyPath) == "" {
		return fmt.Errorf("release gate requires a public key. Set DOGGO_RELEASE_PUBLIC_KEY or run 'doggo release keygen'")
	}

	if opts.SkipVerify {
		return nil
	}

	checkReleaseIntegrityFiles := s.CheckReleaseIntegrityFiles
	if checkReleaseIntegrityFiles == nil {
		checkReleaseIntegrityFiles = releasebundle.CheckReleaseIntegrityFiles
	}
	return checkReleaseIntegrityFiles(projectRoot, opts.OutDir, publicKeyPath, requireSignature)
}

func (s Service) RunKeygen(opts KeygenOptions) (*KeyPairPaths, error) {
	projectRoot, err := s.projectRootWithFallback()
	if err != nil {
		return nil, err
	}

	keyInput := opts.KeyDir
	if strings.TrimSpace(keyInput) == "" {
		keyInput = DefaultKeyDirectory
	}
	keyDir := s.resolvePathFromProjectRoot(projectRoot, keyInput)

	generateKeyPair := s.GenerateKeyPair
	if generateKeyPair == nil {
		generateKeyPair = releasebundle.GenerateKeyPair
	}
	if err := generateKeyPair(keyDir); err != nil {
		return nil, err
	}
	return &KeyPairPaths{
		PrivateKeyPath: filepath.Join(keyDir, releasebundle.PrivateKeyFileName),
		PublicKeyPath:  filepath.Join(keyDir, releasebundle.PublicKeyFileName),
	}, nil
}

func (s Service) resolveProjectRoot() (string, bool, error) {
	if s.ResolveProjectRoot != nil {
		return s.ResolveProjectRoot()
	}
	return project.ResolveRoot("")
}

func (s Service) projectRootWithFallback() (string, error) {
	projectRoot, found, err := s.resolveProjectRoot()
	if err != nil {
		return "", err
	}
	if found {
		return projectRoot, nil
	}
	projectRoot, err = os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w", err)
	}
	return projectRoot, nil
}

func (s Service) projectRootRequired() (string, error) {
	projectRoot, found, err := s.resolveProjectRoot()
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%s not found. Run 'doggo mod init' first", project.ModFileName)
	}
	return projectRoot, nil
}

func (s Service) resolvePathFromProjectRoot(projectRoot, path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	return filepath.Join(projectRoot, filepath.FromSlash(trimmed))
}

func (s Service) resolveKeyPathWithProjectDefault(projectRoot, keyPath, defaultRelativePath string, explicit bool) string {
	resolved := s.resolvePathFromProjectRoot(projectRoot, keyPath)
	if strings.TrimSpace(resolved) != "" {
		return resolved
	}
	if explicit {
		return ""
	}
	defaultPath := filepath.Join(projectRoot, filepath.FromSlash(defaultRelativePath))
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}
	return ""
}

func countHighSeverityAuditFindings(findings []toolchaindeps.DependencyAuditFinding) int {
	count := 0
	for _, finding := range findings {
		if strings.EqualFold(strings.TrimSpace(finding.Severity), "high") {
			count++
		}
	}
	return count
}

func countAuditFindingsByCode(findings []toolchaindeps.DependencyAuditFinding, code string) int {
	count := 0
	for _, finding := range findings {
		if strings.EqualFold(strings.TrimSpace(finding.Code), code) {
			count++
		}
	}
	return count
}
