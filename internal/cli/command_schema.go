package cli

type commandOptionSpec struct {
	Name        string
	ValueName   string
	Description string
}

type commandSchema struct {
	HelpLines []cliHelpEntry
	Options   []commandOptionSpec
	Examples  []string
}

type subcommandSchema struct {
	Name     string
	Usage    string
	Summary  string
	Aliases  []string
	Options  []commandOptionSpec
	Examples []string
}

var execCommandSchema = commandSchema{
	HelpLines: []cliHelpEntry{
		{Usage: "doggo exec -- <command> [arguments...]", Summary: "Run the active built generation"},
	},
	Examples: []string{
		"doggo exec -- path/to/script.dog    # Run a script",
		"doggo exec -- lsp                  # Start the built language server",
	},
}

var testCommandSchema = commandSchema{
	HelpLines: []cliHelpEntry{
		{Usage: "doggo test", Summary: "Discover and run *_test.dog files"},
		{Usage: "doggo test --ns my.tests", Summary: "Run tests in a specific namespace"},
		{Usage: "doggo test --report-format json", Summary: "Output machine-readable report"},
		{Usage: "doggo test --report-format junit --report-file report.xml", Summary: "Write JUnit report to file"},
		{Usage: "doggo test tests/", Summary: "Run tests under directory"},
	},
	Options: []commandOptionSpec{
		{Name: "--ns", ValueName: "namespace", Description: "restrict execution to a namespace"},
		{Name: "--report-format", ValueName: "human|json|junit", Description: "select report format"},
		{Name: "--report-file", ValueName: "path", Description: "write report to a file"},
	},
	Examples: []string{
		"doggo test                         # Run tests",
		"doggo test --report-format junit --report-file report.xml",
		"                                   # Write JUnit report to file",
	},
}

var buildCommandSchema = commandSchema{
	HelpLines: []cliHelpEntry{
		{Usage: "doggo build", Summary: "Build the current project generation"},
		{Usage: "doggo build --sync", Summary: "Run sync, dependency audit gate, and build"},
	},
	Options: []commandOptionSpec{
		{Name: "--sync", Description: "run sync before build"},
	},
	Examples: []string{
		"doggo build                         # Build the current project generation",
		"doggo build --sync                 # Sync, audit gate, and build",
	},
}

var publishCommandSchema = commandSchema{
	HelpLines: []cliHelpEntry{
		{Usage: "doggo publish --source <source>", Summary: "Publish current package to registry index"},
		{Usage: "doggo publish --source <source> --registry ./doggo.registry", Summary: "Publish to explicit registry path"},
	},
	Options: []commandOptionSpec{
		{Name: "--registry", ValueName: "path", Description: "registry index file path"},
		{Name: "--source", ValueName: "source", Description: "required published dependency source"},
		{Name: "--allow-overwrite", Description: "replace an existing version"},
	},
	Examples: []string{
		"doggo publish --source https://github.com/acme/math.git@v1.2.3",
		"                                   # Publish package metadata and archive",
	},
}

var releaseSubcommandSchemas = []subcommandSchema{
	{
		Name:    "keygen",
		Summary: "Generate local ed25519 key pair for release bundles",
		Options: []commandOptionSpec{
			{Name: "--key-dir", ValueName: "path", Description: "new directory for the generated key pair"},
		},
		Examples: []string{
			"doggo release keygen                         # Generate keys in dist/keys",
			"doggo release keygen --key-dir dist/keys-v2 # Generate a new versioned key directory",
		},
	},
	{
		Name:    "bundle",
		Summary: "Generate SHA256SUMS + SBOM bundle (signature required by default)",
		Options: []commandOptionSpec{
			{Name: "--out-dir", ValueName: "path", Description: "release bundle output directory"},
			{Name: "--artifacts", ValueName: "paths", Description: "comma-separated extra artifact paths"},
			{Name: "--private-key", ValueName: "path", Description: "ed25519 private key path"},
			{Name: "--allow-unsigned", Description: "allow unsigned bundle generation"},
		},
		Examples: []string{"doggo release bundle               # Generate checksums/SBOM (+ signature by default)"},
	},
	{
		Name:    "verify",
		Summary: "Verify artifact checksums/signature and require SBOM files (signature required by default)",
		Options: []commandOptionSpec{
			{Name: "--out-dir", ValueName: "path", Description: "release bundle output directory"},
			{Name: "--public-key", ValueName: "path", Description: "ed25519 public key path"},
			{Name: "--allow-unsigned", Description: "allow unsigned release bundles"},
		},
		Examples: []string{"doggo release verify               # Verify release integrity files"},
	},
	{
		Name:    "gate",
		Summary: "Enforce high-severity audit gate (+ signature verify by default)",
		Options: []commandOptionSpec{
			{Name: "--out-dir", ValueName: "path", Description: "release bundle output directory"},
			{Name: "--public-key", ValueName: "path", Description: "ed25519 public key path"},
			{Name: "--allow-unsigned", Description: "allow unsigned release bundles"},
			{Name: "--skip-verify", Description: "run audit gate without release integrity-file checks"},
		},
		Examples: []string{"doggo release gate                 # Block release when high-severity issues exist"},
	},
}

var benchSubcommandSchemas = []subcommandSchema{
	{
		Name:    "list",
		Summary: "List available benchmark scenarios",
		Examples: []string{
			"doggo bench list                  # Show built-in benchmark scenarios",
		},
	},
	{
		Name:    "run",
		Usage:   "run [<name>|--all]",
		Summary: "Run one benchmark or all scenarios",
		Options: []commandOptionSpec{
			{Name: "-w, --warmup", ValueName: "n", Description: "number of warmup iterations (default: 3)"},
			{Name: "-n, --iterations", ValueName: "n", Description: "number of measured iterations (default: 10)"},
			{Name: "-o, --output", ValueName: "dir", Description: "output directory for JSON results (default: bench/results)"},
			{Name: "-q, --quiet", Description: "suppress progress output"},
			{Name: "--all", Description: "run all scenarios"},
			{Name: "--no-loop-fusion", Description: "disable loop fusion for comparison"},
			{Name: "--compile-once", Description: "compile once and run many"},
		},
		Examples: []string{
			"doggo bench run loop-fusion/map-reduce",
			"doggo bench run --all -n 100",
			"doggo bench run --all --compile-once  # Exclude repeated compilation",
		},
	},
	{
		Name:    "help",
		Summary: "Show benchmark help",
		Aliases: []string{"-h", "--help", "-?"},
	},
}
