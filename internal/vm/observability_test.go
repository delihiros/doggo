package vm

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/internal/vm/engine"
)

func TestLogBuiltinEmitsStructuredJSON(t *testing.T) {
	runner := MustNewRunnerWithoutCore()
	var out bytes.Buffer
	runner.SetLogOutput(&out)

	_, err := runner.Run(`(doggo.observability/log! :info "hello" {:correlation-id "manual-42" :user "alice"})`)
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &payload); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"level":          "info",
		"message":        "hello",
		"correlation_id": "manual-42",
		"user":           "alice",
	} {
		if got := payload[key]; got != want {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func TestLogCorrelationIDSurvivesLoadedAndLazyExecution(t *testing.T) {
	runner := MustNewRunnerWithoutCore()
	var out bytes.Buffer
	runner.SetLogOutput(&out)

	libFile := filepath.Join(t.TempDir(), "lib.dog")
	if err := os.WriteFile(libFile, []byte(`(doggo.observability/log! :info "loaded")`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := runner.Run(`
		(do
		  (doggo.observability/log! :info "before")
		  (load-file "` + filepath.ToSlash(libFile) + `")
		  (def s (new-lazy-seq
		    (fn [] (do (doggo.observability/log! :info "lazy") (list 1)))))
		  (first s)
		  (doggo.observability/log! :info "after"))
	`)
	if err != nil {
		t.Fatal(err)
	}
	assertSameCorrelationID(t, out.String(), 4)
}

func TestWithSpanEmitsTraceLog(t *testing.T) {
	runner := MustNewRunnerWithoutCore()
	var out bytes.Buffer
	runner.SetLogOutput(&out)

	if _, err := runner.Run(`(doggo.observability/with-span "calc" (fn [] nil))`); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["event"] != "span" || payload["status"] != "ok" {
		t.Fatalf("span log = %#v", payload)
	}
	if payload["trace_id"] == nil || payload["trace_id"] == "" {
		t.Fatalf("missing trace_id: %#v", payload)
	}
}

func TestRuntimeErrorIncludesCorrelationID(t *testing.T) {
	runner := MustNewRunnerWithoutCore()
	_, err := runner.RunWithOrigin("(/ 1 0)", engine.SourceOrigin{File: "app/runtime.dog"})
	if err == nil {
		t.Fatal("division by zero succeeded")
	}

	runtimeErr, ok := err.(*RuntimeError)
	if !ok || runtimeErr.CorrelationID == "" {
		t.Fatalf("runtime error = %T %#v", err, err)
	}
	if !strings.Contains(err.Error(), "correlation-id: "+runtimeErr.CorrelationID) {
		t.Fatalf("error string = %s", err)
	}
}

func mustMap(t *testing.T, value types.Value) *coll.Map {
	t.Helper()
	mapped, ok := value.(*coll.Map)
	if !ok {
		t.Fatalf("expected map, got %T", value)
	}
	return mapped
}

func assertSameCorrelationID(t *testing.T, raw string, wantLines int) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != wantLines {
		t.Fatalf("log lines = %d, want %d", len(lines), wantLines)
	}

	var correlationID string
	for i, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatal(err)
		}
		id, _ := payload["correlation_id"].(string)
		if id == "" {
			t.Fatalf("missing correlation_id at line %d", i)
		}
		if i == 0 {
			correlationID = id
		} else if id != correlationID {
			t.Fatalf("correlation_id at line %d = %q, want %q", i, id, correlationID)
		}
	}
}
