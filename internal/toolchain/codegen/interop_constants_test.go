package codegen

import (
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateInteropFileFiltersConstantsByDefaultType(t *testing.T) {
	for _, target := range [][2]string{{"linux", "386"}, {"linux", "amd64"}, {runtime.GOOS, runtime.GOARCH}} {
		t.Run(target[0]+"/"+target[1], func(t *testing.T) {
			t.Setenv("GOOS", target[0])
			t.Setenv("GOARCH", target[1])
			t.Setenv("CGO_ENABLED", "0")
			root := t.TempDir()
			writeTestGoMod(t, root)
			writeTestFiles(t, root, map[string]string{
				"values/values.go": `package values
const Capacity = 1 << 64
const TooNegative = -(1 << 63) - 1
const PlatformWide = 1 << 40
const PlatformNegative = -(1 << 40)
const Min32 = -(1 << 31)
const Max32 = (1 << 31) - 1
const Min64 = -(1 << 63)
const Max64 = (1 << 63) - 1
const Small = 42
const MaxUint = 42
const MaxUint64 uint64 = 1 << 63
const Rune = 'a'
const WideRune = 'a' + (1 << 32)
const HugeFloat = 1e1000
const RoundedFloat = 1.0 / 3
const NearMaxFloat = 1.7976931348623158e308
const HugeComplex = 1 + 1e1000i
const HugeComplexReal = 1e1000 + 1i
const RoundedComplex = (1.0 / 3) + (1.0 / 7) * 1i
func Hello() string { return "hello" }
`,
				"internal/interop/registry.go": "package interop\nvar Packages = map[string]map[string]interface{}{}\nfunc RegisterPackage(path string, exports map[string]interface{}) { Packages[path] = exports }\n",
				"internal/interop/values_test.go": `package interop
import "testing"
func TestValues(t *testing.T) {
 v := Packages["doggo/values"]
 if v["Small"] != 42 || v["MaxUint"] != 42 || v["MaxUint64"] != uint64(1 << 63) || v["Rune"] != rune('a') || v["Min32"] != int(-1 << 31) || v["Max32"] != int((1 << 31) - 1) || v["Hello"].(func() string)() != "hello" { t.Fatalf("exports = %#v", v) }
 if v["RoundedFloat"] != float64(1.0 / 3) || v["NearMaxFloat"] != float64(1.7976931348623158e308) || v["RoundedComplex"] != complex128((1.0 / 3) + (1.0 / 7) * 1i) { t.Fatalf("rounded constants = %#v", v) }
}
`,
			})
			native := exec.Command("go", "test", "./values")
			native.Dir = root
			if out, err := native.CombinedOutput(); err != nil {
				t.Fatalf("valid native Go fixture: %v\n%s", err, out)
			}
			output := filepath.Join(root, "internal", "interop", "generated.go")
			if err := GenerateInteropFile(root, output, []string{"doggo/values"}); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Capacity", "TooNegative", "WideRune", "HugeFloat", "HugeComplex", "HugeComplexReal"} {
				if strings.Contains(string(content), `"`+name+`":`) {
					t.Errorf("unrepresentable %s registered", name)
				}
			}
			for _, name := range []string{"PlatformWide", "PlatformNegative", "Min64", "Max64"} {
				if got, want := strings.Contains(string(content), `"`+name+`":`), types.SizesFor("gc", target[1]).Sizeof(types.Typ[types.Int]) == 8; got != want {
					t.Errorf("%s registered = %t, want %t", name, got, want)
				}
			}
			// Cross-target compilation checks target sizing; values execute natively.
			binary := filepath.Join(t.TempDir(), "interop.test")
			if target[0] == "windows" {
				binary += ".exe"
			}
			cmd := exec.Command("go", "test", "-c", "-o", binary, "./internal/interop")
			cmd.Dir = root
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated values: %v\n%s", err, out)
			}
			if target[0] == runtime.GOOS && target[1] == runtime.GOARCH {
				if out, err := exec.Command(binary).CombinedOutput(); err != nil {
					t.Fatalf("native generated values: %v\n%s", err, out)
				}
			}
		})
	}
}

func TestGenerateInteropFilePreservesEmptyPackageInitialization(t *testing.T) {
	root := t.TempDir()
	writeTestGoMod(t, root)
	writeTestFiles(t, root, map[string]string{
		"state/state.go":               "package state\nvar Count int\n",
		"empty/empty.go":               "package same\nimport \"doggo/state\"\nfunc init() { state.Count++ }\n",
		"generic/generic.go":           "package same\nimport \"doggo/state\"\nfunc Identity[T any](v T) T { return v }\ntype Box[T any] struct { Value T }\ntype Constraint interface { ~int }\nfunc init() { state.Count++ }\n",
		"usable/usable.go":             "package same\nfunc Hello() string { return \"hello\" }\n",
		"internal/interop/registry.go": "package interop\nvar Packages = map[string]map[string]interface{}{}\nfunc RegisterPackage(path string, exports map[string]interface{}) { Packages[path] = exports }\n",
		"internal/interop/empty_test.go": `package interop
import ("testing"; "doggo/state")
func TestEmpty(t *testing.T) {
 for _, p := range []string{"doggo/empty", "doggo/generic"} {
  exports, ok := Packages[p]
  if !ok || len(exports) != 0 { t.Fatalf("%s exports = %#v, present %t", p, exports, ok) }
 }
 if state.Count != 2 || Packages["doggo/usable"]["Hello"].(func() string)() != "hello" { t.Fatalf("init count = %d", state.Count) }
}
`,
	})
	output := filepath.Join(root, "internal", "interop", "generated.go")
	if err := GenerateInteropFile(root, output, []string{"doggo/empty", "doggo/generic", "doggo/usable", "reflect"}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "./internal/interop")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated empty registry: %v\n%s", err, out)
	}
}
