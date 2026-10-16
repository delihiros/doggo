package project

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeSystemPathDarwinVarAlias(t *testing.T) {
	physical, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(physical, "/private/var/") {
		t.Skip("temporary directory is outside the standard /var system alias")
	}
	logical := strings.TrimPrefix(physical, "/private")
	for _, candidate := range []string{logical, physical} {
		got, err := NormalizeSystemPath(candidate)
		if err != nil || got != physical {
			t.Fatalf("NormalizeSystemPath(%q) = %q, %v; want %q", candidate, got, err, physical)
		}
	}
}
