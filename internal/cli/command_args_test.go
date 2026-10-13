package cli

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	releasebundle "doggo/internal/release"
	toolchainbench "doggo/internal/toolchain/bench"
	toolchaindeps "doggo/internal/toolchain/deps"
	toolchainrelease "doggo/internal/toolchain/release"
)

func TestParseBuildCommandArgs(t *testing.T) {
	opts, err := parseBuildCommandArgs([]string{"--sync"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.WithSync {
		t.Fatal("expected --sync to be captured")
	}
}

func TestParseBenchRunArgs(t *testing.T) {
	opts, err := parseBenchRunArgs([]string{
		"--all",
		"-n", "100",
		"--warmup", "5",
		"--no-loop-fusion",
		"--compile-once",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.RunAll {
		t.Fatal("expected --all to be captured")
	}
	if opts.Iterations != 100 || opts.Warmup != 5 {
		t.Fatalf("unexpected run counts: %+v", opts)
	}
	if !opts.DisableLoopFusion || !opts.CompileOnce {
		t.Fatalf("unexpected bench flags: %+v", opts)
	}

	opts, err = parseBenchRunArgs([]string{"loop-fusion/map-reduce"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.RunAll {
		t.Fatal("unexpected run-all option")
	}
	if opts.ScenarioName != "loop-fusion/map-reduce" {
		t.Fatalf("scenario = %q", opts.ScenarioName)
	}
	defaults := toolchainbench.DefaultRunOptions()
	if opts.OutputDir != defaults.OutputDir {
		t.Fatalf("output dir = %q", opts.OutputDir)
	}
	if opts.OnProgress != nil {
		t.Fatal("expected parser to leave progress callback unset")
	}
}

func TestParsePublishCommandArgs(t *testing.T) {
	opts, err := parsePublishCommandArgs([]string{
		"--registry", "./doggo.registry",
		"--source", "https://github.com/acme/math.git@v1.2.3",
		"--allow-overwrite",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.RegistryPath != "./doggo.registry" {
		t.Fatalf("registry = %q", opts.RegistryPath)
	}
	if opts.Source != "https://github.com/acme/math.git@v1.2.3" {
		t.Fatalf("source = %q", opts.Source)
	}
	if !opts.AllowOverwrite {
		t.Fatal("expected allow overwrite")
	}
}

func TestParsePublishCommandArgsHelpMarksSourceRequired(t *testing.T) {
	var parseErr error
	help := captureCLIStream(t, &os.Stderr, func() {
		_, parseErr = parsePublishCommandArgs([]string{"--help"})
	})
	if !errors.Is(parseErr, flag.ErrHelp) {
		t.Fatalf("error = %v", parseErr)
	}
	if !strings.Contains(help, "required published dependency source") {
		t.Fatalf("publish help does not mark --source required:\n%s", help)
	}
}

func TestParseReleaseBundleCommandArgs(t *testing.T) {
	opts, err := parseReleaseBundleCommandArgs([]string{
		"--out-dir", "dist/release",
		"--artifacts", "editors/vscode/dist/doggo-vscode.vsix",
		"--private-key", "keys/release.pem",
		"--allow-unsigned",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutDir != "dist/release" {
		t.Fatalf("out dir = %q", opts.OutDir)
	}
	if opts.Artifacts != "editors/vscode/dist/doggo-vscode.vsix" {
		t.Fatalf("artifacts = %q", opts.Artifacts)
	}
	if opts.PrivateKeyPath != "keys/release.pem" {
		t.Fatalf("private key = %q", opts.PrivateKeyPath)
	}
	if !opts.AllowUnsigned {
		t.Fatal("expected allow unsigned")
	}
}

func TestParseReleaseBundleCommandArgs_UsesEnvDefaultAndAllowsExplicitEmptyOverride(t *testing.T) {
	t.Setenv("DOGGO_RELEASE_PRIVATE_KEY", "keys/from-env.pem")

	opts, err := parseReleaseBundleCommandArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.PrivateKeyPath != "keys/from-env.pem" {
		t.Fatalf("private key = %q", opts.PrivateKeyPath)
	}
	if opts.Artifacts != "" {
		t.Fatalf("default extras = %q, want none", opts.Artifacts)
	}

	opts, err = parseReleaseBundleCommandArgs([]string{"--private-key", ""})
	if err != nil {
		t.Fatal(err)
	}
	if opts.PrivateKeyPath != "" {
		t.Fatalf("private key = %q, want explicit empty override", opts.PrivateKeyPath)
	}
	if !opts.PrivateKeySet {
		t.Fatal("expected explicit private-key flag to be tracked")
	}
}

func TestParseReleaseVerifyCommandArgs(t *testing.T) {
	opts, err := parseReleaseVerifyCommandArgs([]string{
		"--out-dir", "dist/release",
		"--public-key", "keys/release.pub",
		"--allow-unsigned",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutDir != "dist/release" {
		t.Fatalf("out dir = %q", opts.OutDir)
	}
	if opts.PublicKeyPath != "keys/release.pub" {
		t.Fatalf("public key = %q", opts.PublicKeyPath)
	}
	if !opts.AllowUnsigned {
		t.Fatal("expected allow unsigned")
	}
}

func TestParseReleaseVerifyCommandArgs_UsesEnvDefaultAndAllowsExplicitEmptyOverride(t *testing.T) {
	t.Setenv("DOGGO_RELEASE_PUBLIC_KEY", "keys/from-env.pub")

	opts, err := parseReleaseVerifyCommandArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.PublicKeyPath != "keys/from-env.pub" {
		t.Fatalf("public key = %q", opts.PublicKeyPath)
	}

	opts, err = parseReleaseVerifyCommandArgs([]string{"--public-key", ""})
	if err != nil {
		t.Fatal(err)
	}
	if opts.PublicKeyPath != "" {
		t.Fatalf("public key = %q, want explicit empty override", opts.PublicKeyPath)
	}
	if !opts.PublicKeySet {
		t.Fatal("expected explicit public-key flag to be tracked")
	}
}

func TestParseReleaseGateCommandArgs(t *testing.T) {
	opts, err := parseReleaseGateCommandArgs([]string{
		"--out-dir", "dist/release",
		"--public-key", "keys/release.pub",
		"--allow-unsigned",
		"--skip-verify",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutDir != "dist/release" {
		t.Fatalf("out dir = %q", opts.OutDir)
	}
	if opts.PublicKeyPath != "keys/release.pub" {
		t.Fatalf("public key = %q", opts.PublicKeyPath)
	}
	if !opts.AllowUnsigned {
		t.Fatal("expected allow unsigned")
	}
	if !opts.SkipVerify {
		t.Fatal("expected skip verify")
	}
}

func TestParseReleaseGateCommandArgs_UsesEnvDefaultAndAllowsExplicitEmptyOverride(t *testing.T) {
	t.Setenv("DOGGO_RELEASE_PUBLIC_KEY", "keys/from-env.pub")

	opts, err := parseReleaseGateCommandArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.PublicKeyPath != "keys/from-env.pub" {
		t.Fatalf("public key = %q", opts.PublicKeyPath)
	}

	opts, err = parseReleaseGateCommandArgs([]string{"--public-key", ""})
	if err != nil {
		t.Fatal(err)
	}
	if opts.PublicKeyPath != "" {
		t.Fatalf("public key = %q, want explicit empty override", opts.PublicKeyPath)
	}
	if !opts.PublicKeySet {
		t.Fatal("expected explicit public-key flag to be tracked")
	}
}

func TestParseReleaseKeygenCommandArgs(t *testing.T) {
	opts, err := parseReleaseKeygenCommandArgs([]string{"--key-dir", "keys/release"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.KeyDir != "keys/release" {
		t.Fatalf("key directory = %q", opts.KeyDir)
	}
}

func TestParseReleaseBundleCommandArgsExplicitEmptySkipsProjectDefaultKey(t *testing.T) {
	projectRoot := writeCLIRuntimeGeneration(t, []byte("doggo"), true)
	defaultKeyPath := filepath.Join(projectRoot, filepath.FromSlash(toolchainrelease.DefaultPrivateKeyPath))
	if err := os.MkdirAll(filepath.Dir(defaultKeyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultKeyPath, []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts, err := parseReleaseBundleCommandArgs([]string{"--private-key", "", "--allow-unsigned"})
	if err != nil {
		t.Fatal(err)
	}

	gotKeyPath := "__unset__"
	service := toolchainrelease.Service{
		ResolveProjectRoot: func() (string, bool, error) {
			return projectRoot, true, nil
		},
		GenerateReleaseBundle: func(projectRoot string, _ project.GenerationBinarySnapshot, artifactPaths []string, outDir, privateKeyPath string) (*releasebundle.ReleaseBundleOutputs, error) {
			gotKeyPath = privateKeyPath
			return &releasebundle.ReleaseBundleOutputs{}, nil
		},
	}

	if _, err := service.RunBundle(*opts); err != nil {
		t.Fatal(err)
	}
	if gotKeyPath != "" {
		t.Fatalf("private key path = %q, want explicit empty override to skip project default", gotKeyPath)
	}
}

func TestParseReleaseVerifyAndGateCommandArgsExplicitEmptySkipsProjectDefaultKey(t *testing.T) {
	projectRoot := t.TempDir()
	defaultKeyPath := filepath.Join(projectRoot, filepath.FromSlash(toolchainrelease.DefaultPublicKeyPath))
	if err := os.MkdirAll(filepath.Dir(defaultKeyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultKeyPath, []byte("public"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		parse func([]string) (*toolchainrelease.VerifyOptions, error)
		run   func(toolchainrelease.Service, toolchainrelease.VerifyOptions) error
		args  []string
	}{
		{
			name:  "verify",
			parse: parseReleaseVerifyCommandArgs,
			run: func(service toolchainrelease.Service, opts toolchainrelease.VerifyOptions) error {
				return service.RunVerify(opts)
			},
			args: []string{"--public-key", "", "--allow-unsigned"},
		},
		{
			name: "gate",
			parse: func(args []string) (*toolchainrelease.VerifyOptions, error) {
				gateOpts, err := parseReleaseGateCommandArgs(args)
				if err != nil {
					return nil, err
				}
				return &toolchainrelease.VerifyOptions{
					OutDir:        gateOpts.OutDir,
					PublicKeyPath: gateOpts.PublicKeyPath,
					PublicKeySet:  gateOpts.PublicKeySet,
					AllowUnsigned: gateOpts.AllowUnsigned,
				}, nil
			},
			run: func(service toolchainrelease.Service, opts toolchainrelease.VerifyOptions) error {
				return service.RunGate(toolchainrelease.GateOptions{
					OutDir:        opts.OutDir,
					PublicKeyPath: opts.PublicKeyPath,
					PublicKeySet:  opts.PublicKeySet,
					AllowUnsigned: opts.AllowUnsigned,
				})
			},
			args: []string{"--public-key", "", "--allow-unsigned"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := tc.parse(tc.args)
			if err != nil {
				t.Fatal(err)
			}

			gotKeyPath := "__unset__"
			service := toolchainrelease.Service{
				ResolveProjectRoot: func() (string, bool, error) {
					return projectRoot, true, nil
				},
				AuditDependencies: func(projectRoot string) ([]toolchaindeps.DependencyAuditFinding, error) {
					return nil, nil
				},
				CheckReleaseIntegrityFiles: func(projectRoot, outDir, publicKeyPath string, requireSignature bool) error {
					gotKeyPath = publicKeyPath
					return nil
				},
			}

			if err := tc.run(service, *opts); err != nil {
				t.Fatal(err)
			}
			if gotKeyPath != "" {
				t.Fatalf("public key path = %q, want explicit empty override to skip project default", gotKeyPath)
			}
		})
	}
}
