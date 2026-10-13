package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTextIngressRejectsInvalidUnicode(t *testing.T) {
	runner := MustNewRunner()
	invalidPath := filepath.ToSlash(filepath.Join(t.TempDir(), "invalid.txt"))
	if err := os.WriteFile(invalidPath, []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, source := range []string{
		`(require '[doggo.json :as json]) (json/read-string "\"\\ud834\"")`,
		`(require '[doggo.io :as io]) (io/slurp "` + invalidPath + `")`,
	} {
		if _, err := runner.Run(source); err == nil {
			t.Errorf("expected invalid Unicode rejection for %s", source)
		}
	}

	value, err := runner.Run(`(require '[doggo.io :as io]) (io/slurp "` + invalidPath + `" {:binary true})`)
	if err != nil {
		t.Fatalf("binary slurp rejected bytes: %v", err)
	}
	if got := value.String(); got != `[255 254]` {
		t.Fatalf("unexpected binary bytes: %s", got)
	}
}
