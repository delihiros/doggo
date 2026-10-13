package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	releasebundle "doggo/internal/release"
	toolchaindeps "doggo/internal/toolchain/deps"
	"doggo/internal/toolchain/generation"
	toolchainrelease "doggo/internal/toolchain/release"
)

func TestDefaultReleaseServiceUsesConfiguredGenerationSessionResolver(t *testing.T) {
	projectRoot := t.TempDir()
	want := errors.New("open session")
	originalResolve := defaultResolveProjectRoot
	originalOpen := defaultOpenGenerationSession
	t.Cleanup(func() {
		defaultResolveProjectRoot = originalResolve
		defaultOpenGenerationSession = originalOpen
	})
	defaultResolveProjectRoot = func() (string, bool, error) { return projectRoot, true, nil }
	defaultOpenGenerationSession = func(path string) (*project.GenerationSession, error) {
		if path != project.ModPath(projectRoot) {
			t.Fatalf("session path = %q, want %q", path, project.ModPath(projectRoot))
		}
		return nil, want
	}

	err := NewApp("test", []string{"release", "bundle", "--allow-unsigned"}).Run()
	if !errors.Is(err, want) {
		t.Fatalf("release bundle error = %v, want configured session error", err)
	}
}

func TestRunReleaseCommandUsesToolchainReleaseService(t *testing.T) {
	projectRoot := writeCLIRuntimeGeneration(t, []byte("doggo"), true)
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{
		"release", "bundle",
		"--allow-unsigned",
		"--artifacts", "editors/vscode/dist/doggo-vscode.vsix",
		"--out-dir", "dist/release",
	})
	var gotProjectRoot string
	var gotArtifacts []string
	var gotOutDir string
	var gotPrivateKey string
	var gotBinary project.GenerationBinarySnapshot
	app.services.Release = &toolchainrelease.Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return "", false, nil
		},
		GenerateReleaseBundle: func(projectRoot string, binary project.GenerationBinarySnapshot, artifactPaths []string, outDir, privateKeyPath string) (*releasebundle.ReleaseBundleOutputs, error) {
			gotProjectRoot = projectRoot
			gotBinary = binary
			gotArtifacts = append([]string(nil), artifactPaths...)
			gotOutDir = outDir
			gotPrivateKey = privateKeyPath
			return &releasebundle.ReleaseBundleOutputs{
				ManifestPath: filepath.Join(projectRoot, outDir, "release-bundle.json"),
			}, nil
		},
	}

	if err := app.Run(); err != nil {
		t.Fatalf("release bundle failed: %v", err)
	}
	if gotProjectRoot != projectRoot {
		t.Fatalf("project root = %q, want %q", gotProjectRoot, projectRoot)
	}
	if gotOutDir != "dist/release" {
		t.Fatalf("out dir = %q", gotOutDir)
	}
	if gotPrivateKey != "" {
		t.Fatalf("private key = %q, want empty for allow-unsigned", gotPrivateKey)
	}
	if len(gotArtifacts) != 1 || gotArtifacts[0] != "editors/vscode/dist/doggo-vscode.vsix" {
		t.Fatalf("artifacts = %#v", gotArtifacts)
	}
	if !strings.HasPrefix(gotBinary.ProjectPath, ".doggo/generations/") || gotBinary.SHA256 == "" || string(gotBinary.Content) != "doggo" {
		t.Fatalf("generation binary snapshot = %#v", gotBinary)
	}
}

func TestRunReleaseGateCommand_UsesToolchainDepsAuditorByDefault(t *testing.T) {
	projectRoot := t.TempDir()
	mod := &project.ModFile{Module: "example.com/demo", Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod file: %v", err)
	}
	prepared, err := (generation.Service{}).PrepareSync(projectRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	lock := prepared.Lock

	calledLoadModFile := false
	calledAuditResolvedLock := false
	app := NewApp("1.0.0", []string{"release", "gate", "--skip-verify"})
	originalResolve := defaultResolveProjectRoot
	originalDepsLoadModFile := defaultDepsLoadModFile
	originalDepsAuditResolvedLock := defaultDepsAuditResolvedLock
	t.Cleanup(func() {
		defaultResolveProjectRoot = originalResolve
		defaultDepsLoadModFile = originalDepsLoadModFile
		defaultDepsAuditResolvedLock = originalDepsAuditResolvedLock
	})
	defaultResolveProjectRoot = func() (string, bool, error) {
		return projectRoot, true, nil
	}
	defaultDepsLoadModFile = func(path string) (*project.ModFile, error) {
		calledLoadModFile = true
		if path != project.ModPath(projectRoot) {
			t.Fatalf("mod path = %q", path)
		}
		return mod, nil
	}
	defaultDepsAuditResolvedLock = func(contentRoot, sourceRoot string, modFile *project.ModFile, lockFile *project.LockFile) ([]toolchaindeps.DependencyAuditFinding, error) {
		calledAuditResolvedLock = true
		if sourceRoot != projectRoot || strings.HasPrefix(contentRoot, filepath.Join(projectRoot, ".doggo")+string(filepath.Separator)) {
			t.Fatalf("audit roots = %q %q", contentRoot, sourceRoot)
		}
		if _, err := os.Stat(filepath.Join(contentRoot, "toolchain", "go.mod")); err != nil {
			t.Fatalf("copied audit toolchain: %v", err)
		}
		if modFile.Module != mod.Module {
			t.Fatalf("mod module = %q", modFile.Module)
		}
		if lockFile == nil || lockFile.ModLockInputsSHA256 != lock.ModLockInputsSHA256 {
			t.Fatalf("unexpected lock: %+v", lockFile)
		}
		return nil, nil
	}

	if err := app.Run(); err != nil {
		t.Fatalf("release gate failed: %v", err)
	}
	if !calledLoadModFile || !calledAuditResolvedLock {
		t.Fatalf("expected default release gate path to use toolchain deps auditor, got loadMod=%v audit=%v", calledLoadModFile, calledAuditResolvedLock)
	}
}

func TestRunReleaseVerifyCommandUsesParsedServiceOptions(t *testing.T) {
	projectRoot := t.TempDir()
	app := NewApp("1.0.0", []string{
		"release", "verify",
		"--out-dir", "dist/custom",
		"--public-key", "keys/release.pub",
		"--allow-unsigned",
	})
	var gotOpts toolchainrelease.VerifyOptions
	app.services.Release = &toolchainrelease.Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return projectRoot, true, nil
		},
		CheckReleaseIntegrityFiles: func(projectRoot, outDir, publicKeyPath string, requireSignature bool) error {
			gotOpts = toolchainrelease.VerifyOptions{
				OutDir:        outDir,
				PublicKeyPath: publicKeyPath,
				AllowUnsigned: !requireSignature,
			}
			return nil
		},
	}

	if err := app.Run(); err != nil {
		t.Fatalf("release verify failed: %v", err)
	}
	if gotOpts.OutDir != "dist/custom" {
		t.Fatalf("out dir = %q", gotOpts.OutDir)
	}
	if gotOpts.PublicKeyPath != filepath.Join(projectRoot, "keys/release.pub") {
		t.Fatalf("public key = %q", gotOpts.PublicKeyPath)
	}
	if !gotOpts.AllowUnsigned {
		t.Fatal("expected allow unsigned")
	}
}

func TestReleaseVerifySuccessDoesNotClaimUnsignedSignatureVerification(t *testing.T) {
	got := renderReleaseVerifySuccess()
	if strings.Contains(got, "signature verified") || strings.Contains(got, "checksums/signature") {
		t.Fatalf("unsigned verification output claims signature verification: %q", got)
	}
	if !strings.Contains(got, "artifact checksums verified") {
		t.Fatalf("verification output = %q", got)
	}
}

func TestRunReleaseVerifyCommandAllowsExplicitEmptyPublicKeyOverride(t *testing.T) {
	t.Setenv("DOGGO_RELEASE_PUBLIC_KEY", "keys/from-env.pub")

	projectRoot := t.TempDir()
	app := NewApp("1.0.0", []string{
		"release", "verify",
		"--public-key", "",
		"--allow-unsigned",
	})
	var gotOpts toolchainrelease.VerifyOptions
	app.services.Release = &toolchainrelease.Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return projectRoot, true, nil
		},
		CheckReleaseIntegrityFiles: func(projectRoot, outDir, publicKeyPath string, requireSignature bool) error {
			gotOpts = toolchainrelease.VerifyOptions{
				OutDir:        outDir,
				PublicKeyPath: publicKeyPath,
				AllowUnsigned: !requireSignature,
			}
			return nil
		},
	}

	if err := app.Run(); err != nil {
		t.Fatalf("release verify failed: %v", err)
	}
	if gotOpts.PublicKeyPath != "" {
		t.Fatalf("public key = %q, want explicit empty override", gotOpts.PublicKeyPath)
	}
}

func TestRunReleaseBundleCommandAllowsExplicitEmptyPrivateKeyOverride(t *testing.T) {
	t.Setenv("DOGGO_RELEASE_PRIVATE_KEY", "keys/from-env.pem")

	projectRoot := writeCLIRuntimeGeneration(t, []byte("doggo"), true)
	app := NewApp("1.0.0", []string{
		"release", "bundle",
		"--private-key", "",
		"--allow-unsigned",
	})
	var gotOpts toolchainrelease.BundleOptions
	app.services.Release = &toolchainrelease.Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return projectRoot, true, nil
		},
		GenerateReleaseBundle: func(projectRoot string, _ project.GenerationBinarySnapshot, artifactPaths []string, outDir, privateKeyPath string) (*releasebundle.ReleaseBundleOutputs, error) {
			gotOpts = toolchainrelease.BundleOptions{
				Artifacts:      strings.Join(artifactPaths, ","),
				OutDir:         outDir,
				PrivateKeyPath: privateKeyPath,
				AllowUnsigned:  true,
			}
			return &releasebundle.ReleaseBundleOutputs{
				ManifestPath: filepath.Join(projectRoot, outDir, "release-bundle.json"),
			}, nil
		},
	}

	if err := app.Run(); err != nil {
		t.Fatalf("release bundle failed: %v", err)
	}
	if gotOpts.PrivateKeyPath != "" {
		t.Fatalf("private key = %q, want explicit empty override", gotOpts.PrivateKeyPath)
	}
}

func TestRunReleaseKeygenCommandUsesParsedServiceOptions(t *testing.T) {
	app := NewApp("1.0.0", []string{
		"release", "keygen",
		"--key-dir", "keys/release",
	})
	var gotOpts toolchainrelease.KeygenOptions
	app.services.Release = &toolchainrelease.Service{
		GenerateKeyPair: func(keyDir string) error {
			gotOpts = toolchainrelease.KeygenOptions{KeyDir: keyDir}
			return nil
		},
		ResolveProjectRoot: func() (string, bool, error) {
			return "", false, nil
		},
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	projectRoot := t.TempDir()
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := app.Run(); err != nil {
		t.Fatalf("release keygen failed: %v", err)
	}
	if gotOpts.KeyDir != filepath.Join(projectRoot, "keys/release") {
		t.Fatalf("key directory = %q", gotOpts.KeyDir)
	}
}
