package vm

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestDoggoReplPstSupportsFrameFilterOptions(t *testing.T) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(require '[doggo.repl :as repl])
		(defn boom [] (throw (ex-info "boom" {:kind :inner})))
		(defn outer [] (boom))
	`)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if _, err := runner.Run(`(outer)`); err == nil {
		t.Fatal("expected outer to raise exception")
	}

	output := captureStdout(t, func() {
		if _, runErr := runner.Run(`
			(repl/pst {:depth 1
			           :filter (fn [frame] (doggo.string/includes? frame "boom"))})`); runErr != nil {
			t.Fatalf("repl/pst with options failed: %v", runErr)
		}
	})

	if !strings.Contains(output, "[0] :inner: boom") {
		t.Fatalf("expected root-cause header in repl/pst output, got %q", output)
	}
	if !strings.Contains(output, "in boom") {
		t.Fatalf("expected filtered boom frame in repl/pst output, got %q", output)
	}
	if strings.Contains(output, "in outer") {
		t.Fatalf("expected repl/pst filter to drop outer frame, got %q", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = original }()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return buf.String()
}
