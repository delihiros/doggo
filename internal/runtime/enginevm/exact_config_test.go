package enginevm

import (
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/vm/engine"
)

func TestResolveExactConfigCopiesGenerationSourceSliceWithIdenticalPointers(t *testing.T) {
	source := &project.GenerationSource{}
	sources := []*project.GenerationSource{source}
	exact, err := ResolveExactConfig(Config{GenerationSources: sources})
	if err != nil {
		t.Fatal(err)
	}
	sources[0] = &project.GenerationSource{}
	if len(exact.Engine.GenerationSources) != 1 || exact.Engine.GenerationSources[0] != source {
		t.Fatalf("generation sources = %#v, want copied slice containing original pointer", exact.Engine.GenerationSources)
	}
}

func TestNewGraphHandlesCopiesRuntimePathSlices(t *testing.T) {
	source := &project.GenerationSource{}
	loadPaths := []string{"live"}
	sources := []*project.GenerationSource{source}
	handles := NewGraphHandles(engine.RuntimeConfig{LoadPaths: loadPaths, GenerationSources: sources})
	loadPaths[0] = "replacement"
	sources[0] = &project.GenerationSource{}
	if len(handles.Engine.LoadPaths) != 1 || handles.Engine.LoadPaths[0] != "live" {
		t.Fatalf("engine load paths = %#v", handles.Engine.LoadPaths)
	}
	if len(handles.Engine.GenerationSources) != 1 || handles.Engine.GenerationSources[0] != source {
		t.Fatalf("engine generation sources = %#v", handles.Engine.GenerationSources)
	}
}

func TestResolveExactConfigInitializesPracticalPureStandardWhenBootstrapCoreRequested(t *testing.T) {
	projectRoot := t.TempDir()
	exact, err := ResolveExactConfig(Config{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot},
		BootstrapCore: true,
		Profile:       nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	initialized, err := NewInitializedExactExecution(NewGraphHandles(exact.Engine), exact)
	if err != nil {
		t.Fatal(err)
	}
	runner := initialized.Runner

	tests := []struct {
		name    string
		expr    string
		want    string
		wantErr string
	}{
		{
			name: "pure builtins and bootstrapped core are available",
			expr: "[(count [1 2 3]) (take 3 (repeat 7))]",
			want: "[3 (7 7 7)]",
		},
		{
			name:    "slurp remains unavailable by default standard resolution",
			expr:    `(slurp "sample.txt")`,
			wantErr: "undefined variable: slurp",
		},
		{
			name:    "print remains unavailable by default standard resolution",
			expr:    `(print "hi")`,
			wantErr: "undefined variable: print",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runner.Run(tt.expr)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tt.want {
				t.Fatalf("result = %s, want %s", got.String(), tt.want)
			}
		})
	}
}
