package cli

import (
	"testing"
)

func TestBuildCommandPassesSyncFlagValue(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		withSync bool
	}{
		{name: "build", args: []string{"build"}, withSync: false},
		{name: "build sync", args: []string{"build", "--sync"}, withSync: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			app := NewApp("1.0.0", tt.args)
			app.services.Build = func(gotWithSync bool) error {
				called = true
				if gotWithSync != tt.withSync {
					t.Fatalf("withSync = %v, want %v", gotWithSync, tt.withSync)
				}
				return nil
			}

			if err := app.runBuildCommand(); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("expected build service to be called")
			}
		})
	}
}

func TestBuildCommandIgnoresPositionalArguments(t *testing.T) {
	called := false
	app := NewApp("1.0.0", []string{"build", "ignored-target"})
	app.services.Build = func(withSync bool) error {
		called = true
		if withSync {
			t.Fatal("unexpected withSync=true")
		}
		return nil
	}

	if err := app.runBuildCommand(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected build service to be called")
	}
}
