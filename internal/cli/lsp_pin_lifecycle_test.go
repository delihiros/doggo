package cli

import (
	"strings"
	"testing"
	"time"

	"doggo/internal/project"
)

func TestLSPReleasesGenerationPinOnEveryTransportExit(t *testing.T) {
	for _, mode := range []string{"shutdown", "eof", "invalid framing"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			writeModAndLock(t, root)
			withWorkingDirectory(t, root)
			old := defaultOpenGenerationSession
			var session *project.GenerationSession
			defaultOpenGenerationSession = func(path string) (*project.GenerationSession, error) {
				var err error
				session, err = project.OpenGenerationSession(path)
				return session, err
			}
			t.Cleanup(func() { defaultOpenGenerationSession = old })
			input, output, runErr := startLSPApp(t)
			writeLSPMessage(t, input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"rootPath": root}})
			waitForLSPOutput(t, output, `"capabilities"`, runErr)
			switch mode {
			case "shutdown":
				writeLSPMessage(t, input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"})
				writeLSPMessage(t, input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
			case "eof":
				if err := input.Close(); err != nil {
					t.Fatal(err)
				}
			case "invalid framing":
				if _, err := input.WriteString("Content-Length: 9223372036854775807\r\n\r\n"); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-runErr:
				if (err != nil) != (mode == "invalid framing") {
					t.Fatalf("transport exit: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("LSP did not exit")
			}
			if session == nil {
				t.Fatal("session was not opened")
			}
			if _, err := session.BinarySnapshot(); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("LSP leaked its session: %v", err)
			}
		})
	}
}
