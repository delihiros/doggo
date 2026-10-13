package cli

import (
	"fmt"
	"strings"
	"sync"
)

type cliCommand struct {
	Name      string
	Usage     string
	Summary   string
	Aliases   []string
	Run       func(*App) error
	HelpLines []cliHelpEntry
	// WritesGeneration marks commands that require an exclusive generation lease.
	WritesGeneration bool
}

func (command *cliCommand) HelpUsage() string {
	if strings.TrimSpace(command.Usage) != "" {
		return command.Usage
	}
	return command.Name
}

type cliHelpEntry struct {
	Usage   string
	Summary string
}

type cliCommandRegistry struct {
	commands []*cliCommand
	lookup   map[string]*cliCommand
}

func newCLICommandRegistry(commands []*cliCommand) *cliCommandRegistry {
	registry := &cliCommandRegistry{
		commands: make([]*cliCommand, 0, len(commands)),
		lookup:   make(map[string]*cliCommand, len(commands)),
	}
	for _, command := range commands {
		if command == nil {
			panic("cli command must not be nil")
		}
		if strings.TrimSpace(command.Name) == "" {
			panic("cli command name must not be empty")
		}
		tokens := append([]string{command.Name}, command.Aliases...)
		for _, token := range tokens {
			trimmed := strings.TrimSpace(token)
			if trimmed == "" {
				panic(fmt.Sprintf("cli command alias for %q must not be empty", command.Name))
			}
			if existing := registry.lookup[trimmed]; existing != nil {
				panic(fmt.Sprintf("duplicate cli command token %q for %q and %q", trimmed, existing.Name, command.Name))
			}
			registry.lookup[trimmed] = command
		}
		registry.commands = append(registry.commands, command)
	}
	return registry
}

func (registry *cliCommandRegistry) Commands() []*cliCommand {
	return append([]*cliCommand(nil), registry.commands...)
}

func (registry *cliCommandRegistry) Lookup(token string) *cliCommand {
	return registry.lookup[token]
}

type cliSubcommand struct {
	Name             string
	Usage            string
	Summary          string
	Aliases          []string
	Run              func(*App) error
	WritesGeneration bool
}

func (subcommand *cliSubcommand) helpUsage(parent string) string {
	usage := subcommand.Name
	if strings.TrimSpace(subcommand.Usage) != "" {
		usage = subcommand.Usage
	}
	return "doggo " + parent + " " + usage
}

type cliSubcommandRegistry struct {
	parent      string
	subcommands []*cliSubcommand
	lookup      map[string]*cliSubcommand
}

func newSubcommandRegistry(parent string, subcommands []cliSubcommand) *cliSubcommandRegistry {
	registry := &cliSubcommandRegistry{
		parent:      parent,
		subcommands: make([]*cliSubcommand, 0, len(subcommands)),
		lookup:      make(map[string]*cliSubcommand, len(subcommands)),
	}
	for i := range subcommands {
		subcommand := subcommands[i]
		if strings.TrimSpace(subcommand.Name) == "" {
			panic(fmt.Sprintf("subcommand name for %q must not be empty", parent))
		}
		current := new(cliSubcommand)
		*current = subcommand
		tokens := append([]string{current.Name}, current.Aliases...)
		for _, token := range tokens {
			trimmed := strings.TrimSpace(token)
			if trimmed == "" {
				panic(fmt.Sprintf("subcommand alias for %q %q must not be empty", parent, current.Name))
			}
			if existing := registry.lookup[trimmed]; existing != nil {
				panic(fmt.Sprintf("duplicate %s subcommand token %q for %q and %q", parent, trimmed, existing.Name, current.Name))
			}
			registry.lookup[trimmed] = current
		}
		registry.subcommands = append(registry.subcommands, current)
	}
	return registry
}

func (registry *cliSubcommandRegistry) Lookup(token string) *cliSubcommand {
	return registry.lookup[token]
}

func (registry *cliSubcommandRegistry) AvailableList() string {
	names := make([]string, 0, len(registry.subcommands))
	for _, subcommand := range registry.subcommands {
		names = append(names, subcommand.Name)
	}
	return strings.Join(names, ", ")
}

func (registry *cliSubcommandRegistry) HelpEntries() []cliHelpEntry {
	entries := make([]cliHelpEntry, 0, len(registry.subcommands))
	for _, subcommand := range registry.subcommands {
		entries = append(entries, cliHelpEntry{
			Usage:   subcommand.helpUsage(registry.parent),
			Summary: subcommand.Summary,
		})
	}
	return entries
}

var (
	defaultCLIRegistryOnce        sync.Once
	defaultCLIRegistryInstance    *cliCommandRegistry
	modSubcommandRegistryOnce     sync.Once
	modSubcommandRegistry         *cliSubcommandRegistry
	depsSubcommandRegistryOnce    sync.Once
	depsSubcommandRegistry        *cliSubcommandRegistry
	releaseSubcommandRegistryOnce sync.Once
	releaseSubcommandRegistry     *cliSubcommandRegistry
	benchSubcommandRegistryOnce   sync.Once
	benchSubcommandRegistry       *cliSubcommandRegistry
)

func DefaultCLIRegistry() *cliCommandRegistry {
	defaultCLIRegistryOnce.Do(func() {
		defaultCLIRegistryInstance = newCLICommandRegistry([]*cliCommand{
			{
				Name:      "exec",
				Usage:     "exec -- <command> [arguments...]",
				Summary:   "Run the active generation binary",
				Run:       func(app *App) error { return app.runExecCommand() },
				HelpLines: execCommandSchema.HelpLines,
			},
			{
				Name:    "repl",
				Summary: "Start the REPL (default if no command specified)",
				Run: func(app *App) error {
					return app.RunREPL()
				},
			},
			{
				Name:    "new",
				Usage:   "new <template> <name> [options]",
				Summary: "Create a new Doggo project from a built-in template",
				Run: func(app *App) error {
					return app.runNewCommand()
				},
				HelpLines: []cliHelpEntry{
					{Usage: "doggo new app my-app", Summary: "Create an application project"},
					{Usage: "doggo new lib my-lib", Summary: "Create a library project"},
					{Usage: "doggo new cli my-tool", Summary: "Create a CLI project"},
					{Usage: "doggo new app my-app --module github.com/acme/my-app", Summary: "Override generated :module metadata"},
					{Usage: "doggo new app my-app --dry-run", Summary: "Preview generated files without writing them"},
				},
			},
			{
				Name:    "mod",
				Usage:   "mod <subcommand>",
				Summary: "Module management commands",
				Run: func(app *App) error {
					return app.runModCommand()
				},
			},
			{
				Name:    "deps",
				Usage:   "deps <subcommand>",
				Summary: "Dependency management commands",
				Run: func(app *App) error {
					return app.runDepsCommand()
				},
			},
			{
				Name:    "test",
				Summary: "Run Doggo tests",
				Run: func(app *App) error {
					return app.runTestCommand()
				},
				HelpLines: testCommandSchema.HelpLines,
			},
			{
				Name:             "build",
				WritesGeneration: true,
				Summary:          "Build the current generation's doggo binary",
				Run: func(app *App) error {
					return app.runBuildCommand()
				},
				HelpLines: buildCommandSchema.HelpLines,
			},
			{
				Name:    "publish",
				Summary: "Publish package metadata and archive to registry index",
				Run: func(app *App) error {
					return app.runPublishCommand()
				},
				HelpLines: publishCommandSchema.HelpLines,
			},
			{
				Name:    "release",
				Usage:   "release <subcommand>",
				Summary: "Release integrity and policy commands",
				Run: func(app *App) error {
					return app.runReleaseCommand()
				},
			},
			{
				Name:    "fmt",
				Summary: "Format Doggo files (use --check to verify)",
				Run: func(app *App) error {
					return app.runFmtCommand()
				},
				HelpLines: []cliHelpEntry{
					{Usage: "doggo fmt [paths...]", Summary: "Rewrite Doggo code using structural formatter"},
					{Usage: "doggo fmt --check [paths...]", Summary: "Check format without rewriting"},
				},
			},
			{
				Name:    "lint",
				Summary: "Run static lint checks",
				Run: func(app *App) error {
					return app.runLintCommand()
				},
				HelpLines: []cliHelpEntry{
					{Usage: "doggo lint [paths...]", Summary: "Check syntax + require/import rules"},
				},
			},
			{
				Name:    "lsp",
				Summary: "Start Language Server Protocol server (stdio)",
				Run: func(app *App) error {
					return app.runLSPCommand()
				},
			},
			{
				Name:    "bench",
				Usage:   "bench <subcommand>",
				Summary: "Benchmark commands",
				Run: func(app *App) error {
					return app.runBenchCommand()
				},
			},
			{
				Name:    "version",
				Summary: "Show version information",
				Run: func(app *App) error {
					PrintVersion(app.version)
					return nil
				},
			},
			{
				Name:    "help",
				Summary: "Show this help message",
				Aliases: []string{"-h", "--help", "-?", "-help"},
				Run: func(app *App) error {
					PrintHelp()
					return nil
				},
			},
		})
	})
	return defaultCLIRegistryInstance
}

func defaultModSubcommandRegistry() *cliSubcommandRegistry {
	modSubcommandRegistryOnce.Do(func() {
		modSubcommandRegistry = newSubcommandRegistry("mod", []cliSubcommand{
			{
				Name:             "init",
				WritesGeneration: true,
				Summary:          "Initialize doggo.mod in current directory",
				Run: func(app *App) error {
					return app.runModInitSubcommand()
				},
			},
			{
				Name:             "sync",
				WritesGeneration: true,
				Summary:          "Snapshot dependencies and generate lock/codegen artifacts",
				Run: func(app *App) error {
					return app.runModSyncSubcommand()
				},
			},
		})
	})
	return modSubcommandRegistry
}

func defaultDepsSubcommandRegistry() *cliSubcommandRegistry {
	depsSubcommandRegistryOnce.Do(func() {
		depsSubcommandRegistry = newSubcommandRegistry("deps", []cliSubcommand{
			{Name: "list", Summary: "List dependency sources in doggo.mod", Run: func(app *App) error { return app.runDepsListSubcommand() }},
			{Name: "add", Usage: "add <source>", Summary: "Add a dependency source", WritesGeneration: true, Run: func(app *App) error { return app.runDepsAddSubcommand() }},
			{Name: "remove", Usage: "remove <source>", Summary: "Remove a dependency source", WritesGeneration: true, Run: func(app *App) error { return app.runDepsRemoveSubcommand() }},
			{Name: "outdated", Summary: "Check whether locked dependencies are outdated", Run: func(app *App) error { return app.runDepsOutdatedSubcommand() }},
			{Name: "update", Usage: "update [--all|<source>]", Summary: "Re-resolve dependencies and update locked revisions/digests", WritesGeneration: true, Run: func(app *App) error { return app.runDepsUpdateSubcommand() }},
			{Name: "audit", Summary: "Audit dependency risks and known vulnerabilities", Run: func(app *App) error { return app.runDepsAuditSubcommand() }},
		})
	})
	return depsSubcommandRegistry
}

func defaultReleaseSubcommandRegistry() *cliSubcommandRegistry {
	releaseSubcommandRegistryOnce.Do(func() {
		subcommands := make([]cliSubcommand, 0, len(releaseSubcommandSchemas))
		for _, schema := range releaseSubcommandSchemas {
			current := cliSubcommand{
				Name:    schema.Name,
				Usage:   schema.Usage,
				Summary: schema.Summary,
				Aliases: append([]string(nil), schema.Aliases...),
			}
			switch schema.Name {
			case "keygen":
				current.Run = func(app *App) error { return app.runReleaseKeygenSubcommand() }
			case "bundle":
				current.Run = func(app *App) error { return app.runReleaseBundleSubcommand() }
			case "verify":
				current.Run = func(app *App) error { return app.runReleaseVerifySubcommand() }
			case "gate":
				current.Run = func(app *App) error { return app.runReleaseGateSubcommand() }
			default:
				panic(fmt.Sprintf("unknown release subcommand schema: %s", schema.Name))
			}
			subcommands = append(subcommands, current)
		}
		releaseSubcommandRegistry = newSubcommandRegistry("release", subcommands)
	})
	return releaseSubcommandRegistry
}

func defaultBenchSubcommandRegistry() *cliSubcommandRegistry {
	benchSubcommandRegistryOnce.Do(func() {
		subcommands := make([]cliSubcommand, 0, len(benchSubcommandSchemas))
		for _, schema := range benchSubcommandSchemas {
			current := cliSubcommand{
				Name:    schema.Name,
				Usage:   schema.Usage,
				Summary: schema.Summary,
				Aliases: append([]string(nil), schema.Aliases...),
			}
			switch schema.Name {
			case "list":
				current.Run = func(app *App) error { return app.benchList() }
			case "run":
				current.Run = func(app *App) error { return app.benchRun() }
			case "help":
				current.Run = func(app *App) error { return app.printBenchHelp() }
			default:
				panic(fmt.Sprintf("unknown bench subcommand schema: %s", schema.Name))
			}
			subcommands = append(subcommands, current)
		}
		benchSubcommandRegistry = newSubcommandRegistry("bench", subcommands)
	})
	return benchSubcommandRegistry
}

func renderCLIHelp() string {
	var builder strings.Builder
	builder.WriteString(`Doggo - Clojure-like Lisp (Bytecode VM)

Usage:
  doggo [options] [command] [arguments]

Options:
  -vm-frame-profile     正常終了したVMフレームの実行時間を表示
  -cpu-profile <file>   Go CPUプロファイルを指定ファイルに出力
  -mem-profile <file>   Goヒーププロファイルを指定ファイルに出力

Commands:
`)
	builder.WriteString(renderHelpEntries(commandHelpEntries(DefaultCLIRegistry().Commands())))
	builder.WriteString("\n")
	builder.WriteString(renderHelpSection("Module Management", defaultModSubcommandRegistry().HelpEntries()))
	builder.WriteString("\n")
	builder.WriteString(renderHelpSection("Dependency Management", defaultDepsSubcommandRegistry().HelpEntries()))
	builder.WriteString("\n")
	builder.WriteString(renderCommandSchemaSection("Testing", "test", testCommandSchema))
	builder.WriteString("\n")
	builder.WriteString(renderCommandSchemaSection("Build", "build", buildCommandSchema))
	builder.WriteString("\n")
	builder.WriteString(renderCommandSchemaSection("Execution", "exec", execCommandSchema))
	builder.WriteString("\n")
	builder.WriteString(renderCommandSchemaSection("Publish", "publish", publishCommandSchema))
	builder.WriteString("\n")
	builder.WriteString(renderSubcommandSchemaSection("Release", "release", releaseSubcommandSchemas))
	builder.WriteString("\n")
	builder.WriteString(renderHelpSection("Formatting / Lint", append(helpEntriesForCommand("fmt"), helpEntriesForCommand("lint")...)))
	builder.WriteString("\n")
	builder.WriteString(renderSubcommandSchemaSection("Benchmarking", "bench", benchSubcommandSchemas))
	builder.WriteString("\n")
	builder.WriteString(renderExampleSection("General Examples", []string{
		"doggo build                         # Build the current project generation",
		"doggo exec -- path/to/script.dog    # Run through the active built generation",
		"doggo exec -- repl                 # Start the built project REPL",
		"doggo exec -- -vm-frame-profile path/to/script.dog",
		"                                   # Report completed VM-frame timings",
		"doggo exec -- -cpu-profile cpu.prof path/to/script.dog",
		"                                   # Run with CPU profiling",
		"doggo new app my-app               # Create an application project",
		"doggo mod init                     # Initialize module",
		"doggo deps add ../my-lib           # Add dependency source",
		"doggo deps add pkg:acme/math@1.2.3 # Resolve package name from registry and add source",
		"doggo deps update --all            # Re-resolve all direct dependencies",
		"doggo deps audit                   # Audit dependency risks",
		"doggo fmt src/                     # Rewrite format",
		"doggo fmt --check src/             # Check only",
		"doggo lint src/                    # Run lint rules",
		"doggo exec -- lsp                  # Start the built LSP server on stdio",
	}))
	return builder.String()
}

func renderCommandSchemaSection(title, name string, schema commandSchema) string {
	parts := []string{renderHelpSection(title, helpEntriesForCommand(name))}
	if len(schema.Options) > 0 {
		parts = append(parts, renderHelpSection(title+" Options", commandOptionHelpEntries(name, schema.Options)))
	}
	if len(schema.Examples) > 0 {
		parts = append(parts, renderExampleSection(title+" Examples", schema.Examples))
	}
	return strings.Join(parts, "\n")
}

func renderSubcommandSchemaSection(title, parent string, schemas []subcommandSchema) string {
	parts := []string{renderHelpSection(title, subcommandSchemaHelpEntries(parent, schemas))}
	if optionEntries := subcommandOptionHelpEntries(parent, schemas); len(optionEntries) > 0 {
		parts = append(parts, renderHelpSection(title+" Options", optionEntries))
	}
	if examples := subcommandExampleLines(schemas); len(examples) > 0 {
		parts = append(parts, renderExampleSection(title+" Examples", examples))
	}
	return strings.Join(parts, "\n")
}

func commandOptionHelpEntries(name string, options []commandOptionSpec) []cliHelpEntry {
	entries := make([]cliHelpEntry, 0, len(options))
	for _, option := range options {
		entries = append(entries, cliHelpEntry{
			Usage:   "doggo " + name + " " + optionUsage(option),
			Summary: option.Description,
		})
	}
	return entries
}

func subcommandOptionHelpEntries(parent string, schemas []subcommandSchema) []cliHelpEntry {
	total := 0
	for _, schema := range schemas {
		total += len(schema.Options)
	}
	entries := make([]cliHelpEntry, 0, total)
	for _, schema := range schemas {
		usage := "doggo " + parent + " " + schemaUsage(schema)
		for _, option := range schema.Options {
			entries = append(entries, cliHelpEntry{
				Usage:   usage + " " + optionUsage(option),
				Summary: option.Description,
			})
		}
	}
	return entries
}

func subcommandExampleLines(schemas []subcommandSchema) []string {
	total := 0
	for _, schema := range schemas {
		total += len(schema.Examples)
	}
	lines := make([]string, 0, total)
	for _, schema := range schemas {
		lines = append(lines, schema.Examples...)
	}
	return lines
}

func subcommandSchemaHelpEntries(parent string, schemas []subcommandSchema) []cliHelpEntry {
	entries := make([]cliHelpEntry, 0, len(schemas))
	for _, schema := range schemas {
		entries = append(entries, cliHelpEntry{
			Usage:   "doggo " + parent + " " + schemaUsage(schema),
			Summary: schema.Summary,
		})
	}
	return entries
}

func schemaUsage(schema subcommandSchema) string {
	if strings.TrimSpace(schema.Usage) != "" {
		return schema.Usage
	}
	return schema.Name
}

func optionUsage(option commandOptionSpec) string {
	if strings.TrimSpace(option.ValueName) == "" {
		return option.Name
	}
	return option.Name + " <" + option.ValueName + ">"
}

func renderExampleSection(title string, examples []string) string {
	var builder strings.Builder
	builder.WriteString(title)
	builder.WriteString(":\n")
	for _, example := range examples {
		builder.WriteString("  ")
		builder.WriteString(example)
		builder.WriteString("\n")
	}
	return builder.String()
}

func commandHelpEntries(commands []*cliCommand) []cliHelpEntry {
	entries := make([]cliHelpEntry, 0, len(commands))
	for _, command := range commands {
		entries = append(entries, cliHelpEntry{
			Usage:   command.HelpUsage(),
			Summary: command.Summary,
		})
	}
	return entries
}

func helpEntriesForCommand(name string) []cliHelpEntry {
	command := DefaultCLIRegistry().Lookup(name)
	if command == nil {
		return nil
	}
	return append([]cliHelpEntry(nil), command.HelpLines...)
}

func renderHelpSection(title string, entries []cliHelpEntry) string {
	var builder strings.Builder
	builder.WriteString(title)
	builder.WriteString(":\n")
	builder.WriteString(renderHelpEntries(entries))
	return builder.String()
}

func renderHelpEntries(entries []cliHelpEntry) string {
	maxUsageLen := 0
	for _, entry := range entries {
		if len(entry.Usage) > maxUsageLen {
			maxUsageLen = len(entry.Usage)
		}
	}
	var builder strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&builder, "  %-*s  %s\n", maxUsageLen, entry.Usage, entry.Summary)
	}
	return builder.String()
}
