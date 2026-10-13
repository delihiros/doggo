package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestDefaultCLIRegistryRecognizesTopLevelCommandsAndAliases(t *testing.T) {
	registry := DefaultCLIRegistry()

	testCases := []struct {
		token string
		want  string
	}{
		{token: "repl", want: "repl"},
		{token: "mod", want: "mod"},
		{token: "deps", want: "deps"},
		{token: "test", want: "test"},
		{token: "build", want: "build"},
		{token: "publish", want: "publish"},
		{token: "release", want: "release"},
		{token: "fmt", want: "fmt"},
		{token: "lint", want: "lint"},
		{token: "lsp", want: "lsp"},
		{token: "bench", want: "bench"},
		{token: "version", want: "version"},
		{token: "help", want: "help"},
		{token: "-h", want: "help"},
		{token: "--help", want: "help"},
	}

	for _, tt := range testCases {
		t.Run(tt.token, func(t *testing.T) {
			command := registry.Lookup(tt.token)
			if command == nil {
				t.Fatalf("Lookup(%q) = nil", tt.token)
			}
			if command.Name != tt.want {
				t.Fatalf("Lookup(%q).Name = %q, want %q", tt.token, command.Name, tt.want)
			}
		})
	}

	if got := registry.Lookup("script.dog"); got != nil {
		t.Fatalf("Lookup(script.dog) = %q, want nil", got.Name)
	}
}

func TestPrintHelpRendersRegistryCommandMetadata(t *testing.T) {
	output := captureCLIStdout(t, func() {
		PrintHelp()
	})

	registry := DefaultCLIRegistry()
	for _, command := range registry.Commands() {
		if !strings.Contains(output, command.HelpUsage()) {
			t.Fatalf("help output missing command usage %q\n%s", command.HelpUsage(), output)
		}
		if !strings.Contains(output, command.Summary) {
			t.Fatalf("help output missing command summary %q\n%s", command.Summary, output)
		}
	}

	requiredLines := []string{
		"doggo mod init",
		"doggo release keygen",
		"doggo bench run [<name>|--all]",
		"doggo bench run --all",
	}
	for _, line := range requiredLines {
		if !strings.Contains(output, line) {
			t.Fatalf("help output missing section line %q\n%s", line, output)
		}
	}
}

func TestRunNamedSubcommandUsesRegistryErrors(t *testing.T) {
	calls := 0
	registry := newSubcommandRegistry("sample", []cliSubcommand{
		{
			Name:    "alpha",
			Summary: "run alpha",
			Run: func(app *App) error {
				calls++
				return nil
			},
		},
		{
			Name:    "beta",
			Summary: "run beta",
			Run: func(app *App) error {
				t.Fatal("beta should not run")
				return nil
			},
		},
	})

	app := NewApp("1.0.0", []string{"sample", "alpha"})
	if err := app.runNamedSubcommand(registry); err != nil {
		t.Fatalf("runNamedSubcommand(alpha) error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("runNamedSubcommand(alpha) calls = %d, want 1", calls)
	}

	app = NewApp("1.0.0", []string{"sample"})
	err := app.runNamedSubcommand(registry)
	if err == nil || err.Error() != "sample command requires a subcommand (available: alpha, beta)" {
		t.Fatalf("missing subcommand error = %v", err)
	}

	app = NewApp("1.0.0", []string{"sample", "unknown"})
	err = app.runNamedSubcommand(registry)
	if err == nil || err.Error() != "unknown sample subcommand: unknown (available: alpha, beta)" {
		t.Fatalf("unknown subcommand error = %v", err)
	}
}

func TestBuildRegistryHandlerUsesServiceBackedCommand(t *testing.T) {
	called := false
	app := NewApp("dev", []string{"build"})
	app.services.Build = func(withSync bool) error {
		called = true
		return nil
	}

	handler := DefaultCLIRegistry().Lookup("build")
	if handler == nil {
		t.Fatal("expected build handler")
	}
	if err := handler.Run(app); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected build handler to delegate to app.services.Build")
	}
}

func TestPrintHelpUsesSchemaDefinedOptionsAndExamples(t *testing.T) {
	output := renderCLIHelp()

	for _, entry := range commandOptionHelpEntries("test", testCommandSchema.Options) {
		if !strings.Contains(output, entry.Usage) || !strings.Contains(output, entry.Summary) {
			t.Fatalf("help output missing test option entry %#v\n%s", entry, output)
		}
	}
	for _, entry := range commandOptionHelpEntries("build", buildCommandSchema.Options) {
		if !strings.Contains(output, entry.Usage) || !strings.Contains(output, entry.Summary) {
			t.Fatalf("help output missing build option entry %#v\n%s", entry, output)
		}
	}
	for _, entry := range commandOptionHelpEntries("publish", publishCommandSchema.Options) {
		if !strings.Contains(output, entry.Usage) || !strings.Contains(output, entry.Summary) {
			t.Fatalf("help output missing publish option entry %#v\n%s", entry, output)
		}
	}
	for _, entry := range subcommandOptionHelpEntries("release", releaseSubcommandSchemas) {
		if !strings.Contains(output, entry.Usage) || !strings.Contains(output, entry.Summary) {
			t.Fatalf("help output missing release option entry %#v\n%s", entry, output)
		}
	}
	for _, entry := range subcommandOptionHelpEntries("bench", benchSubcommandSchemas) {
		if !strings.Contains(output, entry.Usage) || !strings.Contains(output, entry.Summary) {
			t.Fatalf("help output missing bench option entry %#v\n%s", entry, output)
		}
	}

	for _, line := range append(append(append([]string{}, testCommandSchema.Examples...), buildCommandSchema.Examples...), publishCommandSchema.Examples...) {
		if !strings.Contains(output, line) {
			t.Fatalf("help output missing example %q\n%s", line, output)
		}
	}
	for _, line := range subcommandExampleLines(releaseSubcommandSchemas) {
		if !strings.Contains(output, line) {
			t.Fatalf("help output missing release example %q\n%s", line, output)
		}
	}
	for _, line := range subcommandExampleLines(benchSubcommandSchemas) {
		if !strings.Contains(output, line) {
			t.Fatalf("help output missing bench example %q\n%s", line, output)
		}
	}
}

func TestPublishHelpRequiresExplicitSource(t *testing.T) {
	output := renderCLIHelp()
	for _, text := range []string{
		"required published dependency source",
		"doggo publish --source https://github.com/acme/math.git@v1.2.3",
	} {
		if !strings.Contains(output, text) {
			t.Fatalf("publish help missing %q\n%s", text, output)
		}
	}
}

func TestPrintHelpUsesBuiltGenerationWorkflow(t *testing.T) {
	output := renderCLIHelp()
	for _, line := range []string{
		"doggo build                         # Build the current project generation",
		"doggo exec -- path/to/script.dog    # Run a script",
	} {
		if !strings.Contains(output, line) {
			t.Errorf("help output missing generation workflow example %q\n%s", line, output)
		}
	}
	for _, line := range []string{
		"doggo                              # Start REPL",
		"doggo repl                         # Start REPL",
		"doggo script.dog                   # Run a script file",
		"doggo lsp                          # Start LSP server on stdio",
		"doggo [options] [file]",
	} {
		if strings.Contains(output, line) {
			t.Errorf("help output contains direct runtime example %q\n%s", line, output)
		}
	}
}

func TestPrintHelpDescribesDependencyUpdateResolution(t *testing.T) {
	output := renderCLIHelp()
	if !strings.Contains(output, "Re-resolve dependencies and update locked revisions/digests") {
		t.Fatalf("help output does not describe dependency revision/digest re-resolution:\n%s", output)
	}
	if strings.Contains(output, "Update lock entries to current revisions") {
		t.Fatalf("help output still describes dependency updates as revision-only:\n%s", output)
	}
}

func captureCLIStdout(t *testing.T, fn func()) string {
	t.Helper()
	return captureCLIStream(t, &os.Stdout, fn)
}

func captureCLIStream(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()

	// Help output can exceed Windows' pipe buffer. A temporary file captures
	// the complete synchronous output without requiring a concurrent reader.
	captured, err := os.CreateTemp(t.TempDir(), "cli-output-")
	if err != nil {
		t.Fatalf("create captured output: %v", err)
	}
	original := *stream
	*stream = captured
	defer func() {
		*stream = original
		if err := captured.Close(); err != nil {
			t.Errorf("close captured output: %v", err)
		}
	}()

	fn()

	if _, err := captured.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek captured output: %v", err)
	}
	output, err := io.ReadAll(captured)
	if err != nil {
		t.Fatalf("read captured output: %v", err)
	}
	return string(output)
}
