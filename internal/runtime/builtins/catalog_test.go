package builtins

import (
	"testing"

	"doggo/internal/runtime/profile"
)

func TestMinimalProfileInstallsOnlyRequestedBuiltins(t *testing.T) {
	p := profile.New("minimal").SetFeatures([]profile.Feature{profile.FeaturePureCore})
	cat := FromProfile(p)
	for _, name := range []string{
		"*",
		"+",
		"-",
		"/",
		"<",
		"<=",
		"=",
		">",
		">=",
		"contains?",
		"count",
		"dec",
		"empty?",
		"first",
		"reduce",
		"map",
		"map?",
		"filter",
		"identity",
		"inc",
		"ifn?",
		"keyword?",
		"list?",
		"range",
		"last",
		"nil?",
		"not",
		"nth",
		"number?",
		"rest",
		"seq?",
		"set?",
		"some?",
		"string?",
		"symbol?",
		"take",
		"vector?",
	} {
		if cat.Lookup(name) == nil {
			t.Fatalf("expected %q in pure core profile", name)
		}
	}
	for _, name := range []string{"slurp", "spit", "print", "println", "prn", "log!", "io-file*", "os-getenv*", "time-now*", "time-sleep*", "process-start*", "process-run*"} {
		if cat.Lookup(name) != nil {
			t.Fatalf("unexpected effectful builtin %q in pure core profile", name)
		}
	}
}

func TestHostProfileInstallsOnlyPublicHostBuiltins(t *testing.T) {
	p := profile.Host()
	cat := FromProfile(p)
	for _, name := range []string{"println", "print", "prn", "slurp", "spit"} {
		if cat.Lookup(name) == nil {
			t.Fatalf("expected public host builtin %q in host profile", name)
		}
	}
	for _, name := range []string{"io-file*", "os-getenv*", "time-now*", "process-start*"} {
		if cat.Lookup(name) != nil {
			t.Fatalf("did not expect private host helper %q in public host profile", name)
		}
	}
}
