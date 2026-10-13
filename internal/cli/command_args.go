package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"doggo/internal/release"
	toolchainbench "doggo/internal/toolchain/bench"
	toolchainrelease "doggo/internal/toolchain/release"
)

type buildCommandArgs struct {
	WithSync bool
}

type publishCommandArgs struct {
	RegistryPath   string
	Source         string
	AllowOverwrite bool
}

type testCommandOptions struct {
	Paths        []string
	Namespaces   []string
	ReportFormat string
	ReportFile   string
}

type newCommandArgs struct {
	Template string
	Name     string
	Module   string
	DryRun   bool
}

func parseNewCommandArgs(args []string) (*newCommandArgs, error) {
	opts := &newCommandArgs{}
	positionals := make([]string, 0, 2)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--module":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option --module requires a value")
			}
			i++
			if strings.TrimSpace(args[i]) == "" {
				return nil, fmt.Errorf("option --module requires a non-empty value")
			}
			opts.Module = args[i]
		case "--dry-run":
			opts.DryRun = true
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, fmt.Errorf("unknown doggo new option: %s", arg)
			}
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) != 2 {
		return nil, fmt.Errorf("usage: doggo new <template> <name> [--module <module>] [--dry-run]")
	}
	opts.Template = positionals[0]
	opts.Name = positionals[1]
	return opts, nil
}

func parseBenchRunArgs(args []string) (*toolchainbench.RunOptions, error) {
	opts := toolchainbench.DefaultRunOptions()
	flagSet := flag.NewFlagSet("bench run", flag.ContinueOnError)
	flagSet.IntVar(&opts.Warmup, "w", opts.Warmup, "warmup iterations")
	flagSet.IntVar(&opts.Warmup, "warmup", opts.Warmup, "warmup iterations")
	flagSet.IntVar(&opts.Iterations, "n", opts.Iterations, "measured iterations")
	flagSet.IntVar(&opts.Iterations, "iterations", opts.Iterations, "measured iterations")
	flagSet.StringVar(&opts.OutputDir, "o", opts.OutputDir, "output directory")
	flagSet.StringVar(&opts.OutputDir, "output", opts.OutputDir, "output directory")
	flagSet.BoolVar(&opts.Quiet, "q", opts.Quiet, "quiet mode")
	flagSet.BoolVar(&opts.Quiet, "quiet", opts.Quiet, "quiet mode")
	flagSet.BoolVar(&opts.DisableLoopFusion, "no-loop-fusion", opts.DisableLoopFusion, "disable loop fusion")
	flagSet.BoolVar(&opts.CompileOnce, "compile-once", opts.CompileOnce, "compile once, run many")
	flagSet.BoolVar(&opts.RunAll, "all", opts.RunAll, "run all scenarios")

	flagArgs := make([]string, 0, len(args))
	positionalArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			if (arg == "-n" || arg == "-w" || arg == "-o" || arg == "--iterations" || arg == "--warmup" || arg == "--output") && i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
			continue
		}
		positionalArgs = append(positionalArgs, arg)
	}
	if err := flagSet.Parse(flagArgs); err != nil {
		return nil, err
	}
	if opts.RunAll {
		return &opts, nil
	}
	if len(positionalArgs) < 1 {
		return nil, fmt.Errorf("bench run requires a scenario name or --all flag")
	}
	opts.ScenarioName = positionalArgs[0]
	return &opts, nil
}

func parseBuildCommandArgs(args []string) (*buildCommandArgs, error) {
	return parseBuildCommandArgsWithOutput(args, os.Stderr)
}

func parseBuildCommandArgsWithOutput(args []string, output io.Writer) (*buildCommandArgs, error) {
	opts := &buildCommandArgs{}
	flagSet := flag.NewFlagSet("build", flag.ContinueOnError)
	flagSet.SetOutput(output)
	flagSet.BoolVar(&opts.WithSync, "sync", false, "run sync before build")
	if err := flagSet.Parse(args); err != nil {
		return nil, err
	}
	return opts, nil
}

func parsePublishCommandArgs(args []string) (*publishCommandArgs, error) {
	opts := &publishCommandArgs{}
	flagSet := flag.NewFlagSet("publish", flag.ContinueOnError)
	flagSet.StringVar(&opts.RegistryPath, "registry", "", "registry index file path")
	flagSet.StringVar(&opts.Source, "source", "", "required published dependency source")
	flagSet.BoolVar(&opts.AllowOverwrite, "allow-overwrite", false, "allow replacing an existing package version in registry")
	if err := flagSet.Parse(args); err != nil {
		return nil, err
	}
	if flagSet.NArg() != 0 {
		return nil, fmt.Errorf("publish does not accept positional arguments")
	}
	return opts, nil
}

func parseTestCommandArgs(args []string) (*testCommandOptions, error) {
	opts := &testCommandOptions{
		Paths:        make([]string, 0, len(args)),
		Namespaces:   make([]string, 0, 2),
		ReportFormat: testReportFormatHuman,
	}

	appendValue := func(i *int) (string, error) {
		if *i+1 >= len(args) {
			return "", fmt.Errorf("option %s requires a value", args[*i])
		}
		*i = *i + 1
		return args[*i], nil
	}

	paths := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--ns":
			ns, err := appendValue(&i)
			if err != nil {
				return nil, err
			}
			opts.Namespaces = append(opts.Namespaces, ns)
		case "--report-format":
			format, err := appendValue(&i)
			if err != nil {
				return nil, err
			}
			switch format {
			case testReportFormatHuman, testReportFormatJSON, testReportFormatJUnit:
				opts.ReportFormat = format
			default:
				return nil, fmt.Errorf("unknown report format: %s (available: human, json, junit)", format)
			}
		case "--report-file":
			path, err := appendValue(&i)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(path) == "" {
				return nil, fmt.Errorf("--report-file requires a non-empty path")
			}
			opts.ReportFile = path
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, fmt.Errorf("unknown test option: %s", arg)
			}
			paths = append(paths, arg)
		}
	}

	opts.Paths = normalizeStringList(paths)
	opts.Namespaces = normalizeStringList(opts.Namespaces)
	return opts, nil
}

func parseReleaseBundleCommandArgs(args []string) (*toolchainrelease.BundleOptions, error) {
	opts := &toolchainrelease.BundleOptions{
		OutDir:         releaseBundleDir(),
		PrivateKeyPath: strings.TrimSpace(os.Getenv("DOGGO_RELEASE_PRIVATE_KEY")),
	}
	flagSet := flag.NewFlagSet("release bundle", flag.ContinueOnError)
	flagSet.StringVar(&opts.OutDir, "out-dir", opts.OutDir, "output directory for release bundle artifacts")
	flagSet.StringVar(&opts.Artifacts, "artifacts", opts.Artifacts, "comma-separated artifact paths to include")
	flagSet.StringVar(&opts.PrivateKeyPath, "private-key", opts.PrivateKeyPath, "ed25519 private key PEM path for SHA256SUMS signature")
	flagSet.BoolVar(&opts.AllowUnsigned, "allow-unsigned", opts.AllowUnsigned, "allow generating release bundles without a signature")
	if err := flagSet.Parse(args); err != nil {
		return nil, err
	}
	flagSet.Visit(func(f *flag.Flag) {
		if f.Name == "private-key" {
			opts.PrivateKeySet = true
		}
	})
	if flagSet.NArg() != 0 {
		return nil, fmt.Errorf("release bundle does not accept positional arguments")
	}
	return opts, nil
}

func parseReleaseVerifyCommandArgs(args []string) (*toolchainrelease.VerifyOptions, error) {
	opts := &toolchainrelease.VerifyOptions{
		OutDir:        releaseBundleDir(),
		PublicKeyPath: strings.TrimSpace(os.Getenv("DOGGO_RELEASE_PUBLIC_KEY")),
	}
	flagSet := flag.NewFlagSet("release verify", flag.ContinueOnError)
	flagSet.StringVar(&opts.OutDir, "out-dir", opts.OutDir, "release bundle output directory")
	flagSet.StringVar(&opts.PublicKeyPath, "public-key", opts.PublicKeyPath, "ed25519 public key PEM path for signature verification")
	flagSet.BoolVar(&opts.AllowUnsigned, "allow-unsigned", opts.AllowUnsigned, "allow unsigned release bundles")
	if err := flagSet.Parse(args); err != nil {
		return nil, err
	}
	flagSet.Visit(func(f *flag.Flag) {
		if f.Name == "public-key" {
			opts.PublicKeySet = true
		}
	})
	if flagSet.NArg() != 0 {
		return nil, fmt.Errorf("release verify does not accept positional arguments")
	}
	return opts, nil
}

func parseReleaseGateCommandArgs(args []string) (*toolchainrelease.GateOptions, error) {
	opts := &toolchainrelease.GateOptions{
		OutDir:        releaseBundleDir(),
		PublicKeyPath: strings.TrimSpace(os.Getenv("DOGGO_RELEASE_PUBLIC_KEY")),
	}
	flagSet := flag.NewFlagSet("release gate", flag.ContinueOnError)
	flagSet.StringVar(&opts.OutDir, "out-dir", opts.OutDir, "release bundle output directory")
	flagSet.StringVar(&opts.PublicKeyPath, "public-key", opts.PublicKeyPath, "ed25519 public key PEM path for signature verification")
	flagSet.BoolVar(&opts.AllowUnsigned, "allow-unsigned", opts.AllowUnsigned, "allow unsigned release bundles")
	flagSet.BoolVar(&opts.SkipVerify, "skip-verify", opts.SkipVerify, "skip release integrity-file checks and run dependency audit gate only")
	if err := flagSet.Parse(args); err != nil {
		return nil, err
	}
	flagSet.Visit(func(f *flag.Flag) {
		if f.Name == "public-key" {
			opts.PublicKeySet = true
		}
	})
	if flagSet.NArg() != 0 {
		return nil, fmt.Errorf("release gate does not accept positional arguments")
	}
	return opts, nil
}

func parseReleaseKeygenCommandArgs(args []string) (*toolchainrelease.KeygenOptions, error) {
	opts := &toolchainrelease.KeygenOptions{
		KeyDir: filepath.ToSlash(toolchainrelease.DefaultKeyDirectory),
	}
	flagSet := flag.NewFlagSet("release keygen", flag.ContinueOnError)
	flagSet.StringVar(&opts.KeyDir, "key-dir", opts.KeyDir, "new output directory for the ed25519 key pair")
	if err := flagSet.Parse(args); err != nil {
		return nil, err
	}
	if flagSet.NArg() != 0 {
		return nil, fmt.Errorf("release keygen does not accept positional arguments")
	}
	return opts, nil
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func releaseBundleDir() string {
	return release.DefaultReleaseBundleDir
}
