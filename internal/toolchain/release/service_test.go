package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"doggo/internal/project"
	releasebundle "doggo/internal/release"
	toolchaindeps "doggo/internal/toolchain/deps"
	"doggo/internal/toolchain/generation"
)

func TestReleaseServiceBundleRequiresBuiltGeneration(t *testing.T) {
	projectRoot, _ := commitReleaseGeneration(t, false)
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return projectRoot, true, nil },
		GenerateReleaseBundle: func(string, project.GenerationBinarySnapshot, []string, string, string) (*releasebundle.ReleaseBundleOutputs, error) {
			t.Fatal("bundle generator called for a sync-only generation")
			return nil, nil
		},
	}

	_, err := service.RunBundle(BundleOptions{OutDir: "dist/release", AllowUnsigned: true})
	if err == nil || !strings.Contains(err.Error(), "doggo build") {
		t.Fatalf("RunBundle error = %v, want build-required error", err)
	}
	writer := make(chan *project.GenerationLease, 1)
	writerErr := make(chan error, 1)
	go acquireReleaseWriter(projectRoot, writer, writerErr, nil)
	select {
	case lease := <-writer:
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	case err := <-writerErr:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("sync-only rejection leaked its shared generation session")
	}
}

func TestReleaseServiceBundleRejectsNilGenerationSession(t *testing.T) {
	projectRoot := t.TempDir()
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return projectRoot, true, nil },
		OpenGenerationSession: func(string) (*project.GenerationSession, error) {
			return nil, nil
		},
	}
	_, err := service.RunBundle(BundleOptions{OutDir: "dist/release", AllowUnsigned: true})
	if err == nil || !strings.Contains(err.Error(), "generation session is nil") {
		t.Fatalf("RunBundle error = %v", err)
	}
}

func TestReleaseServiceBundleHoldsAndClosesGenerationSession(t *testing.T) {
	projectRoot, lock := commitReleaseGeneration(t, true)
	want := errors.New("bundle failed")

	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "failure", err: want},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := make(chan *project.GenerationLease, 1)
			writerErr := make(chan error, 1)
			writerStarted := make(chan struct{})
			service := Service{
				ResolveProjectRoot: func() (string, bool, error) { return projectRoot, true, nil },
				GenerateReleaseBundle: func(gotRoot string, binary project.GenerationBinarySnapshot, extras []string, outDir, privateKeyPath string) (*releasebundle.ReleaseBundleOutputs, error) {
					if gotRoot != projectRoot {
						t.Fatalf("project root = %q, want %q", gotRoot, projectRoot)
					}
					if binary.ProjectPath != lock.Build.Binary {
						t.Fatalf("binary project path = %q", binary.ProjectPath)
					}
					if binary.SHA256 != lock.Build.SHA256 {
						t.Fatalf("binary digest = %q, want %q", binary.SHA256, lock.Build.SHA256)
					}
					sum := sha256.Sum256(binary.Content)
					if hex.EncodeToString(sum[:]) != binary.SHA256 {
						t.Fatal("binary snapshot content does not match its digest")
					}
					if len(extras) != 1 || extras[0] != "editors/vscode/dist/doggo-vscode.vsix" {
						t.Fatalf("extras = %#v", extras)
					}
					go acquireReleaseWriter(projectRoot, writer, writerErr, writerStarted)
					<-writerStarted
					select {
					case lease := <-writer:
						_ = lease.Close()
						t.Fatal("exclusive writer acquired while release bundle used the generation")
					case err := <-writerErr:
						t.Fatal(err)
					case <-time.After(100 * time.Millisecond):
					}
					if tc.err != nil {
						return nil, tc.err
					}
					return &releasebundle.ReleaseBundleOutputs{}, nil
				},
			}

			_, err := service.RunBundle(BundleOptions{
				Artifacts:     "editors/vscode/dist/doggo-vscode.vsix",
				OutDir:        "dist/release",
				AllowUnsigned: true,
			})
			if !errors.Is(err, tc.err) {
				t.Fatalf("RunBundle error = %v, want %v", err, tc.err)
			}
			select {
			case lease := <-writer:
				if err := lease.Close(); err != nil {
					t.Fatal(err)
				}
			case err := <-writerErr:
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				t.Fatal("release bundle leaked its shared generation session")
			}
		})
	}
}

func TestReleaseServiceRejectsActiveGenerationInArtifacts(t *testing.T) {
	projectRoot, lock := commitReleaseGeneration(t, true)
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/release\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(projectRoot, "editors", "vscode", "package-lock.json")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(`{"lockfileVersion":3,"packages":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return projectRoot, true, nil },
	}

	_, err := service.RunBundle(BundleOptions{
		Artifacts:     lock.Build.Binary,
		OutDir:        releasebundle.DefaultReleaseBundleDir,
		AllowUnsigned: true,
	})
	if err == nil || !strings.Contains(err.Error(), "active generation binary cannot be listed in --artifacts") {
		t.Fatalf("RunBundle error = %v, want extras-only rejection", err)
	}
	if _, statErr := os.Lstat(filepath.Join(projectRoot, releasebundle.DefaultReleaseBundleDir)); !os.IsNotExist(statErr) {
		t.Fatalf("release output created: %v", statErr)
	}
}

func acquireReleaseWriter(root string, acquired chan<- *project.GenerationLease, errs chan<- error, started chan<- struct{}) {
	if started != nil {
		close(started)
	}
	lease, err := project.AcquireGenerationLease(root, true)
	if err != nil {
		errs <- err
		return
	}
	acquired <- lease
}

func commitReleaseGeneration(t *testing.T, build bool) (string, *project.LockFile) {
	t.Helper()
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, []string{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Abort() })
	if build {
		if err := prepared.Build(); err != nil {
			t.Fatal(err)
		}
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	return root, prepared.Lock
}

func TestReleaseServiceGateUsesDepsAuditorAndVerifier(t *testing.T) {
	projectRoot := t.TempDir()
	if err := project.WriteModFile(project.ModPath(projectRoot), &project.ModFile{Module: "example.com/demo"}); err != nil {
		t.Fatalf("write mod file: %v", err)
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(&project.ModFile{Module: "example.com/demo"}),
	}); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	auditCalled := false
	verifyCalled := false
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return projectRoot, true, nil
		},
		AuditDependencies: func(gotProjectRoot string) ([]toolchaindeps.DependencyAuditFinding, error) {
			auditCalled = true
			if gotProjectRoot != projectRoot {
				t.Fatalf("project root = %q", gotProjectRoot)
			}
			return []toolchaindeps.DependencyAuditFinding{
				{Severity: "medium"},
				{Severity: "high"},
				{Severity: "high"},
			}, nil
		},
		CheckReleaseIntegrityFiles: func(gotProjectRoot, outDir, publicKeyPath string, requireSignature bool) error {
			verifyCalled = true
			t.Fatalf("verify should not be called when dependency audit blocks gate")
			return nil
		},
	}

	err := service.RunGate(GateOptions{
		OutDir:        "dist/release",
		PublicKeyPath: "keys/public.pem",
	})
	if err == nil {
		t.Fatal("expected gate to fail")
	}
	if !strings.Contains(err.Error(), "2 high-severity") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !auditCalled {
		t.Fatal("expected audit to be called")
	}
	if verifyCalled {
		t.Fatal("expected verify not to be called")
	}
}

func TestReleaseServiceGateUsesDepsAuditorAndVerifierWhenAuditPasses(t *testing.T) {
	projectRoot := t.TempDir()
	if err := project.WriteModFile(project.ModPath(projectRoot), &project.ModFile{Module: "example.com/demo"}); err != nil {
		t.Fatalf("write mod file: %v", err)
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(&project.ModFile{Module: "example.com/demo"}),
	}); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	auditCalled := false
	verifyCalled := false
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return projectRoot, true, nil
		},
		AuditDependencies: func(gotProjectRoot string) ([]toolchaindeps.DependencyAuditFinding, error) {
			auditCalled = true
			if gotProjectRoot != projectRoot {
				t.Fatalf("project root = %q", gotProjectRoot)
			}
			return []toolchaindeps.DependencyAuditFinding{{Severity: "low"}}, nil
		},
		CheckReleaseIntegrityFiles: func(gotProjectRoot, outDir, publicKeyPath string, requireSignature bool) error {
			verifyCalled = true
			if gotProjectRoot != projectRoot {
				t.Fatalf("project root = %q", gotProjectRoot)
			}
			if outDir != "dist/release" {
				t.Fatalf("out dir = %q", outDir)
			}
			expectedKey := filepath.Join(projectRoot, "keys/public.pem")
			if publicKeyPath != expectedKey {
				t.Fatalf("public key = %q, want %q", publicKeyPath, expectedKey)
			}
			if !requireSignature {
				t.Fatal("expected requireSignature=true")
			}
			return nil
		},
	}

	if err := service.RunGate(GateOptions{
		OutDir:        "dist/release",
		PublicKeyPath: "keys/public.pem",
	}); err != nil {
		t.Fatalf("gate failed: %v", err)
	}
	if !auditCalled {
		t.Fatal("expected audit to be called")
	}
	if !verifyCalled {
		t.Fatal("expected verify to be called")
	}
}

func TestReleaseServiceGateFailsClosedWhenVulnerabilityScannerIsUnavailable(t *testing.T) {
	projectRoot := t.TempDir()
	if err := project.WriteModFile(project.ModPath(projectRoot), &project.ModFile{Module: "example.com/demo"}); err != nil {
		t.Fatalf("write mod file: %v", err)
	}

	verifyCalled := false
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return projectRoot, true, nil
		},
		AuditDependencies: func(string) ([]toolchaindeps.DependencyAuditFinding, error) {
			return []toolchaindeps.DependencyAuditFinding{{
				Source:   "go-modules",
				Kind:     "go",
				Severity: "medium",
				Code:     "vuln-scan-unavailable",
			}}, nil
		},
		CheckReleaseIntegrityFiles: func(string, string, string, bool) error {
			verifyCalled = true
			return nil
		},
	}

	err := service.RunGate(GateOptions{SkipVerify: true})
	if err == nil || !strings.Contains(err.Error(), "vulnerability scan unavailable") {
		t.Fatalf("gate error = %v, want unavailable vulnerability scan failure", err)
	}
	if verifyCalled {
		t.Fatal("verify should not be called when vulnerability scanning is unavailable")
	}
}

func TestReleaseServiceKeygenFallsBackToCwdWhenProjectRootMissing(t *testing.T) {
	projectRoot := t.TempDir()
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
	projectRoot, err = os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	var gotKeyDir string
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return "", false, nil
		},
		GenerateKeyPair: func(keyDir string) error {
			gotKeyDir = keyDir
			return nil
		},
	}

	result, err := service.RunKeygen(KeygenOptions{KeyDir: DefaultKeyDirectory})
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	if gotKeyDir != filepath.Join(projectRoot, DefaultKeyDirectory) {
		t.Fatalf("key directory = %q", gotKeyDir)
	}
	if result.PrivateKeyPath != filepath.Join(gotKeyDir, releasebundle.PrivateKeyFileName) || result.PublicKeyPath != filepath.Join(gotKeyDir, releasebundle.PublicKeyFileName) {
		t.Fatalf("result paths = %+v", result)
	}
}

func TestDefaultReleaseKeyPathsAreOutsidePublishedBundle(t *testing.T) {
	for _, path := range []string{DefaultPrivateKeyPath, DefaultPublicKeyPath} {
		clean := filepath.ToSlash(filepath.Clean(path))
		if clean == releasebundle.DefaultReleaseBundleDir || strings.HasPrefix(clean, releasebundle.DefaultReleaseBundleDir+"/") {
			t.Fatalf("release key path %q is inside published bundle %q", path, releasebundle.DefaultReleaseBundleDir)
		}
	}
}
