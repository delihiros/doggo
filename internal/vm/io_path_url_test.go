package vm

import (
	"fmt"
	"runtime"
	"testing"

	"doggo/internal/types/atom"
)

func TestIOAsURLWindowsNativePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native Windows volume paths")
	}
	runner := MustNewRunner()
	for _, tc := range []struct{ path, want string }{
		{`C:\Users\Example\space # percent%.txt`, "file:///C:/Users/Example/space%20%23%20percent%25.txt"},
		{`C:/Users/Example/space #.txt`, "file:///C:/Users/Example/space%20%23.txt"},
		{`\\server\share\space #.txt`, "file://server/share/space%20%23.txt"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			value, err := runner.Run(fmt.Sprintf("(require '[doggo.io :as io]) (io/as-url %q)", tc.path))
			if err != nil {
				t.Fatal(err)
			}
			if value != atom.NewString(tc.want) {
				t.Fatalf("io/as-url(%q) = %v, want %q", tc.path, value, tc.want)
			}
		})
	}
}

func TestIOAsURLPreservesURLs(t *testing.T) {
	runner := MustNewRunner()
	for _, path := range []string{
		"https://example.com/space%20name?q=x#fragment",
		"http://example.com/path",
		"file:///C:/Users/Example/space%20%23.txt",
		"file://server/share/name.txt",
	} {
		t.Run(path, func(t *testing.T) {
			value, err := runner.Run(fmt.Sprintf("(require '[doggo.io :as io]) (io/as-url %q)", path))
			if err != nil {
				t.Fatal(err)
			}
			if value != atom.NewString(path) {
				t.Fatalf("io/as-url(%q) = %v, want unchanged URL", path, value)
			}
		})
	}
}
