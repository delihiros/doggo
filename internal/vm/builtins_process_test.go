package vm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestProcessTimeoutOptionParsing(t *testing.T) {
	tests := []struct {
		name      string
		input     types.Value
		wantMs    int64
		wantError string
	}{
		{name: "integer", input: atom.NewNumberInt(25), wantMs: 25},
		{name: "float_rounds_up", input: atom.NewNumberFloat(0.1), wantMs: 1},
		{name: "duration_map", input: coll.NewMapFromPairs([]types.Value{atom.NewKeyword("millis"), atom.NewNumberInt(25)}), wantMs: 25},
		{name: "zero", input: atom.NewNumberInt(0), wantMs: 0},
		{name: "nan", input: atom.NewNumberFloat(math.NaN()), wantError: "process-run*: option timeout must be finite"},
		{name: "positive_infinity", input: atom.NewNumberFloat(math.Inf(1)), wantError: "process-run*: option timeout must be finite"},
		{name: "too_large", input: atom.NewNumberInt(maxProcessTimeoutMs + 1), wantError: "process-run*: option timeout must be no greater than"},
		{name: "negative", input: atom.NewNumberInt(-1), wantError: "process-run*: option timeout must be non-negative"},
		{name: "wrong_type", input: atom.NewKeyword("bad"), wantError: "process-run*: option timeout must be a number or duration map"},
		{name: "bad_duration", input: coll.NewMapFromPairs([]types.Value{atom.NewKeyword("seconds"), atom.NewNumberInt(1)}), wantError: "process-run*: option timeout must be a number or duration map"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("timeout"), tt.input,
			})
			got, err := processOptionTimeoutMs(options)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected error %q", tt.wantError)
				}
				if !strings.Contains(err.String(), tt.wantError) {
					t.Fatalf("expected error containing %q, got %s", tt.wantError, err.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantMs {
				t.Fatalf("expected %d, got %d", tt.wantMs, got)
			}
		})
	}
}

func TestProcessOptionsRejectInvalidMaps(t *testing.T) {
	runner := MustNewRunner()
	tests := []string{
		`(require '[doggo.process :as proc]) (proc/run "sh" "-lc" "true" {:unknown true})`,
		`(require '[doggo.process :as proc]) (proc/start "sh" "-lc" "true" {:clear-env :yes})`,
		`(require '[doggo.process :as proc]) (proc/output-file-spec "/tmp/x" {:append :yes})`,
	}
	for _, source := range tests {
		if _, err := runner.Run(source); err == nil {
			t.Errorf("expected strict process option error for %s", source)
		}
	}
}

func TestProcessNamespaceTimeoutKillsDescendantProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-tree cleanup is verified in Unix-style shells on non-Windows hosts")
	}

	markerPath := filepath.Join(t.TempDir(), "orphan.txt")
	script := fmt.Sprintf(
		"sh -lc 'sleep 0.2; printf orphan > %s' & sleep 5",
		processShellSingleQuote(markerPath),
	)

	runner := MustNewRunner()
	value, err := runner.Run(fmt.Sprintf(`
		(require '[doggo.process :as proc]
		         '[doggo.string :as str])
		(let [result (proc/run "sh" "-lc" %q {:timeout 50})]
		  [(:exit result) (:out result) (str/includes? (:err result) "timed out")])
	`, script))
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if got := value.String(); got != `[124 "" true]` {
		t.Fatalf("unexpected timeout result: %s", got)
	}

	time.Sleep(400 * time.Millisecond)

	_, statErr := os.Stat(markerPath)
	if statErr == nil {
		t.Fatalf("timeout left descendant process running: %s was created", markerPath)
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat marker: %v", statErr)
	}
}

func TestProcessNamespaceTimeoutDoesNotKillDescendantsAfterSuccessfulExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runtime verification uses Unix-style shells on non-Windows hosts")
	}

	markerPath := filepath.Join(t.TempDir(), "survivor.txt")
	script := fmt.Sprintf(
		"sh -lc 'sleep 0.2; printf survivor > %s' & exit 0",
		processShellSingleQuote(markerPath),
	)

	runner := MustNewRunner()
	value, err := runner.Run(fmt.Sprintf(`
		(require '[doggo.process :as proc])
		(let [result (proc/run "sh" "-lc" %q {:timeout 1000})]
		  [(:exit result) (:out result) (:err result)])
	`, script))
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if got := value.String(); got != `[0 "" ""]` {
		t.Fatalf("unexpected process result: %s", got)
	}

	time.Sleep(400 * time.Millisecond)

	content, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Fatalf("read marker: %v", readErr)
	}
	if string(content) != "survivor" {
		t.Fatalf("unexpected marker contents: %q", string(content))
	}
}

func TestProcessNamespaceCanceledContextDoesNotStartProcess(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), "never-started.txt")

	runner := MustNewRunner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner.SetContext(ctx)

	_, err := runner.Run(fmt.Sprintf(`
		(require '[doggo.process :as proc])
		(proc/run "sh" "-lc" %q)
	`, "printf started > "+processShellSingleQuote(markerPath)))
	if err == nil {
		t.Fatal("expected canceled context error")
	}
	if !strings.Contains(err.Error(), "process-run*: context canceled") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, statErr := os.Stat(markerPath)
	if statErr == nil {
		t.Fatalf("canceled context still started process: %s was created", markerPath)
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat marker: %v", statErr)
	}
}

func TestProcessNamespaceCanceledContextStopsStartedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runtime verification uses Unix-style shells on non-Windows hosts")
	}

	markerPath := filepath.Join(t.TempDir(), "started-then-canceled.txt")

	runner := MustNewRunner()
	ctx, cancel := context.WithCancel(context.Background())
	runner.SetContext(ctx)

	value, err := runner.Run(fmt.Sprintf(`
		(require '[doggo.process :as proc])
		(proc/start "sh" "-lc" %q)
	`, "sleep 0.2; printf started > "+processShellSingleQuote(markerPath)+"; sleep 1"))
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	handle, errValue := processHandleFromValue("process test", value)
	if errValue != nil {
		t.Fatalf("expected process handle, got %s", errValue.String())
	}

	started := time.Now()
	cancel()
	exitValue := handle.exitRef.Deref()
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("canceled process took too long to exit: %v", elapsed)
	}
	if got := exitValue.String(); got != "-1" {
		t.Fatalf("expected canceled exit to be -1, got %s", got)
	}

	time.Sleep(400 * time.Millisecond)

	_, statErr := os.Stat(markerPath)
	if statErr == nil {
		t.Fatalf("canceled started process still ran: %s was created", markerPath)
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat marker: %v", statErr)
	}
}

func TestProcessHandleCancelAfterExitPreservesExitCode(t *testing.T) {
	options := processStartOptions{
		in:  processRedirect{mode: processRedirectClosed},
		out: processRedirect{mode: processRedirectPipe},
		err: processRedirect{mode: processRedirectDiscard},
	}

	ctx, cancel := context.WithCancel(context.Background())
	handle, err := processStartHandle(ctx, processFixtureCommand(t, "exit7"), options)
	if err != nil {
		cancel()
		t.Fatalf("processStartHandle returned error: %v", err)
	}

	data, readErr := handle.stdout.readAll()
	if readErr != nil {
		cancel()
		t.Fatalf("read stdout: %v", readErr)
	}
	if string(data) != "done" {
		cancel()
		t.Fatalf("unexpected stdout: %q", string(data))
	}

	time.Sleep(20 * time.Millisecond)

	cancel()

	if got := handle.exitRef.Deref().String(); got != "7" {
		t.Fatalf("expected exit 7, got %s", got)
	}
}

func TestProcessStartFailureDoesNotLeakSetupDescriptors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fd leak check relies on /proc/self/fd on Unix-style hosts")
	}

	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("/proc/self/fd is unavailable on this host")
	}

	countFDs := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatalf("read fd dir: %v", err)
		}
		return len(entries)
	}

	badPath := filepath.Join(t.TempDir(), "missing", "stderr.txt")
	options := processStartOptions{
		in:  processRedirect{mode: processRedirectPipe},
		out: processRedirect{mode: processRedirectPipe},
		err: processRedirect{mode: processRedirectFile, path: badPath},
	}

	before := countFDs()
	for i := 0; i < 200; i++ {
		_, err := processStartHandle(context.Background(), []string{"sh", "-lc", "printf hi"}, options)
		if err == nil {
			t.Fatal("expected processStartHandle to fail")
		}
	}
	after := countFDs()

	if after > before+2 {
		t.Fatalf("fd leak detected: before=%d after=%d", before, after)
	}
}

func TestProcessSuccessfulStartWithoutSlurpDoesNotLeakOutputDescriptors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fd leak check relies on /proc/self/fd on Unix-style hosts")
	}

	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("/proc/self/fd is unavailable on this host")
	}

	countFDs := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatalf("read fd dir: %v", err)
		}
		return len(entries)
	}

	options := processStartOptions{
		in:  processRedirect{mode: processRedirectClosed},
		out: processRedirect{mode: processRedirectPipe},
		err: processRedirect{mode: processRedirectPipe},
	}

	before := countFDs()
	for i := 0; i < 100; i++ {
		handle, err := processStartHandle(context.Background(), []string{"sh", "-lc", "printf out; printf err >&2"}, options)
		if err != nil {
			t.Fatalf("processStartHandle returned error: %v", err)
		}
		if got := handle.exitRef.Deref().String(); got != "0" {
			t.Fatalf("expected exit 0, got %s", got)
		}
	}
	after := countFDs()

	if after > before+5 {
		t.Fatalf("fd leak detected after successful starts: before=%d after=%d", before, after)
	}
}

func TestProcessHandleSupportsDelayedSlurpAfterExit(t *testing.T) {
	options := processStartOptions{
		in:  processRedirect{mode: processRedirectClosed},
		out: processRedirect{mode: processRedirectPipe},
		err: processRedirect{mode: processRedirectPipe},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handle, err := processStartHandle(ctx, processFixtureCommand(t, "large-output"), options)
	if err != nil {
		t.Fatalf("processStartHandle returned error: %v", err)
	}

	if got := handle.exitRef.Deref().String(); got != "0" {
		t.Fatalf("expected exit 0 before delayed slurp, got %s", got)
	}

	outData, outErr := handle.stdout.readAll()
	if outErr != nil {
		t.Fatalf("read delayed stdout: %v", outErr)
	}
	if len(outData) != 200000 {
		t.Fatalf("expected 200000 stdout bytes, got %d", len(outData))
	}

	errData, errErr := handle.stderr.readAll()
	if errErr != nil {
		t.Fatalf("read delayed stderr: %v", errErr)
	}
	if string(errData) != "err" {
		t.Fatalf("unexpected delayed stderr: %q", string(errData))
	}
}

func TestProcessStartHandleFallsBackWhenTreeControlIsUnsupported(t *testing.T) {
	originalFactory := processTreeControllerFactory
	processTreeControllerFactory = func() (processTreeController, error) {
		return nil, newProcessTreeUnsupportedError("simulated unsupported tree control")
	}
	defer func() {
		processTreeControllerFactory = originalFactory
	}()

	options := processStartOptions{
		in:  processRedirect{mode: processRedirectClosed},
		out: processRedirect{mode: processRedirectDiscard},
		err: processRedirect{mode: processRedirectDiscard},
	}

	handle, err := processStartHandle(context.Background(), processFixtureCommand(t, "sleep"), options)
	if err != nil {
		t.Fatalf("processStartHandle returned error: %v", err)
	}

	if !handle.alive() {
		t.Fatal("expected process to be alive after fallback start")
	}

	exitValue, killTreeErr := handle.killTree()
	if killTreeErr == nil {
		t.Fatal("expected kill to fail without tree control")
	}
	if !errors.Is(killTreeErr, errProcessTreeUnsupported) {
		t.Fatalf("expected unsupported tree-control error, got %v", killTreeErr)
	}

	if got := handle.exitRef.Deref().String(); got != "0" {
		t.Fatalf("expected process to exit naturally with code 0, got %s", got)
	}
	if exitValue != nil {
		t.Fatalf("expected nil exit value on failed kill, got %s", exitValue.String())
	}
}

type delayedProcessStartController struct{ processTreeController }

func (c delayedProcessStartController) afterStart(ctx context.Context, cmd *exec.Cmd) error {
	time.Sleep(50 * time.Millisecond)
	return c.processTreeController.afterStart(ctx, cmd)
}

func TestProcessStartHandleTimeoutBeginsAfterTreeAttachment(t *testing.T) {
	originalFactory := processTreeControllerFactory
	processTreeControllerFactory = func() (processTreeController, error) {
		controller, err := originalFactory()
		if err != nil {
			return nil, err
		}
		return delayedProcessStartController{controller}, nil
	}
	t.Cleanup(func() { processTreeControllerFactory = originalFactory })
	options := processStartOptions{
		timeoutMs: 10,
		in:        processRedirect{mode: processRedirectClosed},
		out:       processRedirect{mode: processRedirectDiscard},
		err:       processRedirect{mode: processRedirectDiscard},
	}
	handle, err := processStartHandle(context.Background(), processFixtureCommand(t, "sleep"), options)
	if err != nil {
		t.Fatalf("start returned no timeout handle after slow tree attachment: %v", err)
	}
	if got := handle.exitRef.Deref().String(); got != "124" {
		t.Fatalf("process exit = %s, want timeout exit 124", got)
	}
}

func processFixtureCommand(t *testing.T, mode string) []string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []string{executable, "-test.run=^TestProcessFixture$", "--", "doggo-process-fixture", mode}
}

// Execute the native test binary so process behavior is covered without a shell.
func TestProcessFixture(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "doggo-process-fixture" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "sleep":
		time.Sleep(time.Second)
	case "exit7":
		fmt.Fprint(os.Stdout, "done")
		os.Exit(7)
	case "large-output":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 200000))
		fmt.Fprint(os.Stderr, "err")
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func TestProcessTreeSupportedReflectsFactoryCapability(t *testing.T) {
	originalFactory := processTreeControllerFactory
	defer func() {
		processTreeControllerFactory = originalFactory
	}()

	processTreeControllerFactory = func() (processTreeController, error) {
		return nil, newProcessTreeUnsupportedError("simulated unsupported tree control")
	}
	supported, err := processTreeSupported()
	if err != nil {
		t.Fatalf("processTreeSupported returned error: %v", err)
	}
	if supported {
		t.Fatal("expected unsupported tree control to report false")
	}

	processTreeControllerFactory = func() (processTreeController, error) {
		return noopProcessTreeController{}, nil
	}
	supported, err = processTreeSupported()
	if err != nil {
		t.Fatalf("processTreeSupported returned error: %v", err)
	}
	if !supported {
		t.Fatal("expected supported tree control to report true")
	}
}

func TestProcessStartHandleRejectsUnsupportedTreeControlWhenTimeoutRequiresIt(t *testing.T) {
	originalFactory := processTreeControllerFactory
	processTreeControllerFactory = func() (processTreeController, error) {
		return nil, newProcessTreeUnsupportedError("simulated unsupported tree control")
	}
	defer func() {
		processTreeControllerFactory = originalFactory
	}()

	options := processStartOptions{
		timeoutMs: 10,
		in:        processRedirect{mode: processRedirectClosed},
		out:       processRedirect{mode: processRedirectDiscard},
		err:       processRedirect{mode: processRedirectDiscard},
	}

	_, err := processStartHandle(context.Background(), []string{"sh", "-lc", "sleep 1"}, options)
	if err == nil {
		t.Fatal("expected processStartHandle to fail")
	}
	if !errors.Is(err, errProcessTreeUnsupported) {
		t.Fatalf("expected unsupported tree-control error, got %v", err)
	}
}

func TestProcessStartHandleRejectsUnsupportedTreeControlWhenContextIsCancelable(t *testing.T) {
	originalFactory := processTreeControllerFactory
	processTreeControllerFactory = func() (processTreeController, error) {
		return nil, newProcessTreeUnsupportedError("simulated unsupported tree control")
	}
	defer func() {
		processTreeControllerFactory = originalFactory
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	options := processStartOptions{
		in:  processRedirect{mode: processRedirectClosed},
		out: processRedirect{mode: processRedirectDiscard},
		err: processRedirect{mode: processRedirectDiscard},
	}

	_, err := processStartHandle(ctx, []string{"sh", "-lc", "sleep 1"}, options)
	if err == nil {
		t.Fatal("expected processStartHandle to fail")
	}
	if !errors.Is(err, errProcessTreeUnsupported) {
		t.Fatalf("expected unsupported tree-control error, got %v", err)
	}
}

func processShellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
