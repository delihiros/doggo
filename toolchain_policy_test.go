package doggo

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestReleaseToolchainsMatchModuleMinimum(t *testing.T) {
	module, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	minimum := regexp.MustCompile(`(?m)^go (\S+)$`).FindSubmatch(module)
	if len(minimum) != 2 {
		t.Fatal("missing Go module minimum")
	}
	version := string(minimum[1])
	for path, declaration := range map[string]string{
		".github/workflows/ci.yml":      "GO_VERSION: '" + version + "'",
		".github/workflows/release.yml": "GO_VERSION: \"" + version + "\"",
		"Makefile":                      "GO_TOOLCHAIN ?= go" + version,
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), declaration) {
			t.Errorf("%s must select module minimum Go %s", path, version)
		}
	}
}
