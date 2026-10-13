package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"doggo/internal/project"
)

func TestExecUsesChildScriptProjectHint(t *testing.T) {
	script := filepath.Join(t.TempDir(), "outside", "src", "main.dog")
	want := errors.New("stop after project hint")
	oldOpen := defaultOpenGenerationSession
	t.Cleanup(func() { defaultOpenGenerationSession = oldOpen })

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"plain", []string{script, "script-argument"}},
		{"frame_profile", []string{"-vm-frame-profile", script, "script-argument"}},
		{"cpu_profile", []string{"--cpu-profile", "cpu.prof", script}},
		{"mem_profile", []string{"-mem-profile=mem.prof", script}},
		{"separator", []string{"--", script}},
		{"nested_exec", []string{"exec", "--", script}},
		{"nested_flags", []string{"-vm-frame-profile", "exec", "--", "--mem-profile=mem.prof", "exec", "--", "--", script}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			defaultOpenGenerationSession = func(hint string) (*project.GenerationSession, error) {
				got = hint
				return nil, want
			}
			args := append([]string{"exec", "--"}, tc.args...)
			original := append([]string(nil), args...)
			stderr := captureCLIStream(t, &os.Stderr, func() {
				if err := NewApp("test", args).Run(); !errors.Is(err, want) {
					t.Fatalf("Run error = %v, want project hint sentinel", err)
				}
			})
			if got != script {
				t.Errorf("generation hint = %q, want child script %q", got, script)
			}
			if !reflect.DeepEqual(args, original) {
				t.Errorf("child arguments changed: got %#v, want %#v", args, original)
			}
			if stderr != "" {
				t.Errorf("project hint parsing emitted child diagnostics: %q", stderr)
			}
		})
	}
}

func TestExecInvalidChildArgumentsRemainForChildDiagnostics(t *testing.T) {
	want := errors.New("child generation validation")
	oldOpen := defaultOpenGenerationSession
	defaultOpenGenerationSession = func(string) (*project.GenerationSession, error) { return nil, want }
	t.Cleanup(func() { defaultOpenGenerationSession = oldOpen })
	for _, args := range [][]string{
		{"--invalid", "script.dog"},
		{"-cpu-profile"},
		{"-vm-frame-profile=invalid", "script.dog"},
		{"exec", "script.dog"},
		{"exec", "--", "--invalid", "script.dog"},
		{"--help"},
	} {
		t.Run(args[0]+"_"+filepath.Base(args[len(args)-1]), func(t *testing.T) {
			stderr := captureCLIStream(t, &os.Stderr, func() {
				if err := NewApp("test", append([]string{"exec", "--"}, args...)).Run(); !errors.Is(err, want) {
					t.Fatalf("error = %v, want generation validation before child execution", err)
				}
			})
			if stderr != "" {
				t.Fatalf("hint classification emitted child diagnostics: %q", stderr)
			}
		})
	}
}
