package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestSourceRegistryTracksPrecompiledSources(t *testing.T) {
	reg := NewSourceRegistry(nil)
	reg.RegisterPrecompiled(&Program{
		SourceFile: "app/demo.dog",
		Source:     "(def answer 42)\nanswer",
	})

	line, ok := reg.LookupLine("app/demo.dog", 2)
	if !ok || line != "answer" {
		t.Fatalf("line = %q ok=%v", line, ok)
	}
}

func TestSourceRegistryLookupContentFallsBackToProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	file := filepath.Join(projectRoot, "app", "sample.dog")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(file, []byte("(+ 1 2)\n42\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	reg := NewSourceRegistry(&testProjectRootProvider{projectRoot: projectRoot})
	content, ok := reg.LookupContent("app/sample.dog")
	if !ok || content != "(+ 1 2)\n42\n" {
		t.Fatalf("content = %q ok=%v", content, ok)
	}
}

func TestSourceRegistryLookupContentReadsStdlib(t *testing.T) {
	reg := NewSourceRegistry(nil)
	content, ok := reg.LookupContent("stdlib://doggo/core.dog")
	if !ok {
		t.Fatal("expected stdlib content")
	}
	if !strings.Contains(content, "(ns doggo.core)") {
		t.Fatalf("stdlib content missing namespace declaration: %q", content[:64])
	}
}

func TestSourceRegistryConcurrentRegisterAndLookup(t *testing.T) {
	reg := NewSourceRegistry(nil)
	reg.Register("repl.dog", "first")
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10_000; i++ {
			reg.Register("repl.dog", "first\nsecond")
			runtime.Gosched()
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10_000; i++ {
			_, _ = reg.LookupLine("repl.dog", 1)
			runtime.Gosched()
		}
	}()

	close(start)
	wg.Wait()
}

type testProjectRootProvider struct {
	projectRoot string
}

func (r *testProjectRootProvider) ProjectRoot() string {
	if r == nil {
		return ""
	}
	return r.projectRoot
}
