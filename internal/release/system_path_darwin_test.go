package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseKeysDarwinSystemAliasPreservesSuffixChecks(t *testing.T) {
	physical, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(physical, "/private/var/") {
		t.Skip("temporary directory is outside the standard /var system alias")
	}
	logical := strings.TrimPrefix(physical, "/private")
	if err := GenerateKeyPair(filepath.Join(logical, "keys")); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{logical, physical} {
		if _, err := loadEd25519PrivateKey(filepath.Join(root, "keys", PrivateKeyFileName)); err != nil {
			t.Fatalf("read key through %q: %v", root, err)
		}
	}
	for _, pair := range [][2]string{{logical, physical}, {physical, logical}} {
		got, err := resolveProjectRelativePath(pair[0], filepath.Join(pair[1], "keys", PublicKeyFileName))
		if err != nil || got != "keys/"+PublicKeyFileName {
			t.Fatalf("mixed root/candidate: got %q, %v", got, err)
		}
	}
	if err := os.Symlink(filepath.Join(physical, "keys"), filepath.Join(physical, "key-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEd25519PrivateKey(filepath.Join(logical, "key-link", PrivateKeyFileName)); err == nil {
		t.Fatal("accepted a key symlink below the system alias")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(physical, "output-link")); err != nil {
		t.Fatal(err)
	}
	if err := GenerateKeyPair(filepath.Join(logical, "output-link", "keys")); err == nil {
		t.Fatal("accepted key generation through an untrusted suffix symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "keys")); !os.IsNotExist(err) {
		t.Fatalf("outside key directory was touched: %v", err)
	}
}
