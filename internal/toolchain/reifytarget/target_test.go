package reifytarget

import (
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestResolvePreservesSourceAndRuntimeIdentity(t *testing.T) {
	pkg := loadFixturePackage(t, `package facade

type hidden interface { Render() string }
type Public = hidden
type Reader[T any] interface { Read(T) error }
type IntReader = Reader[int]
type Constraint interface { ~int }
type Anonymous = interface { Close() error }
type Error = error
`)

	public, err := Resolve(pkg, "Public")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := public.Target.Source, "example.com/facade.Public"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	if got, want := public.Target.Runtime, "example.com/facade.hidden"; got != want {
		t.Fatalf("runtime = %q, want %q", got, want)
	}
	if _, ok := public.SourceType.(*types.Alias); !ok {
		t.Fatalf("source type = %T, want *types.Alias", public.SourceType)
	}
	if public.Interface.NumMethods() != 1 || public.Interface.Method(0).Name() != "Render" {
		t.Fatalf("interface = %v, want Render method", public.Interface)
	}

	intReader, err := Resolve(pkg, "IntReader")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := intReader.Target.Runtime, "example.com/facade.Reader[int]"; got != want {
		t.Fatalf("runtime = %q, want %q", got, want)
	}

	for _, name := range []string{"Reader", "Constraint", "Anonymous", "Error"} {
		t.Run(name, func(t *testing.T) {
			if _, err := Resolve(pkg, name); err == nil {
				t.Fatalf("Resolve(%q) succeeded", name)
			}
		})
	}
}

func TestCanonicalTypeIdentityMatchesTypesIdentical(t *testing.T) {
	pkg := loadFixturePackage(t, "package facade\n\n"+strings.Join([]string{
		"type Original struct { Value int }",
		"type Alias = Original",
		"type Box[T any] struct { Value T }",
		"type IntBox = Box[int]",
		"type IntBoxAgain = Box[int]",
		"type StringBox = Box[string]",
		"type ArrayA = [2]int",
		"type ArrayB = [2]int",
		"type ArrayC = [3]int",
		"type SendA = chan<- int",
		"type SendB = chan<- int",
		"type Receive = <-chan int",
		"type VariadicA = func(string, ...int) error",
		"type VariadicB = func(name string, values ...int) (err error)",
		"type SliceFunc = func(string, []int) error",
		"type Embedded struct { Value int }",
		"type StructA = struct { Embedded; Name string `json:\"name\"` }",
		"type StructB = struct { Embedded; Name string `json:\"name\"` }",
		"type StructTag = struct { Embedded; Name string `json:\"other\"` }",
		"type StructField = struct { Embedded Embedded; Name string `json:\"name\"` }",
		"type InterfaceA = interface { Read([]byte) (int, error); Close() error }",
		"type InterfaceB = interface { Close() error; Read(p []byte) (n int, err error) }",
		"type InterfaceC = interface { Read([]byte) (int, error) }",
		"type UnionA = interface { ~int | string }",
		"type UnionB = interface { string | ~int }",
		"type UnionC = interface { int | string }",
		"type IntersectionA = interface { int | string; int }",
		"type IntersectionB = interface { int }",
		"type IntersectionC = interface { string }",
		"type ComparableIntersection = interface { comparable; int | []byte }",
	}, "\n")+"\n")

	pairs := []struct {
		a, b      string
		identical bool
	}{
		{"Original", "Alias", true},
		{"IntBox", "IntBoxAgain", true},
		{"IntBox", "StringBox", false},
		{"ArrayA", "ArrayB", true},
		{"ArrayA", "ArrayC", false},
		{"SendA", "SendB", true},
		{"SendA", "Receive", false},
		{"VariadicA", "VariadicB", true},
		{"VariadicA", "SliceFunc", false},
		{"StructA", "StructB", true},
		{"StructA", "StructTag", false},
		{"StructA", "StructField", false},
		{"InterfaceA", "InterfaceB", true},
		{"InterfaceA", "InterfaceC", false},
		{"UnionA", "UnionB", true},
		{"UnionA", "UnionC", false},
		{"IntersectionA", "IntersectionB", true},
		{"IntersectionA", "IntersectionC", false},
		{"ComparableIntersection", "IntersectionB", true},
	}

	for _, pair := range pairs {
		t.Run(pair.a+"_"+pair.b, func(t *testing.T) {
			a := pkg.Scope().Lookup(pair.a).Type()
			b := pkg.Scope().Lookup(pair.b).Type()
			aID, err := CanonicalTypeIdentity(a)
			if err != nil {
				t.Fatal(err)
			}
			bID, err := CanonicalTypeIdentity(b)
			if err != nil {
				t.Fatal(err)
			}
			if got := types.Identical(a, b); got != pair.identical {
				t.Fatalf("fixture types.Identical(%s, %s) = %v, want %v", pair.a, pair.b, got, pair.identical)
			}
			if got := aID == bID; got != types.Identical(a, b) {
				t.Fatalf("identity equality = %v, types.Identical = %v\n%s: %s\n%s: %s", got, types.Identical(a, b), pair.a, aID, pair.b, bID)
			}
		})
	}

	reader := pkg.Scope().Lookup("Box").Type().(*types.Named)
	if _, err := CanonicalTypeIdentity(reader.TypeParams().At(0)); err == nil {
		t.Fatal("CanonicalTypeIdentity accepted an unbound type parameter")
	}
}

func TestCanonicalTypeIdentityNormalizesRedundantUnionTerms(t *testing.T) {
	intType := types.Typ[types.Int]
	redundant := types.NewUnion([]*types.Term{
		types.NewTerm(false, intType),
		types.NewTerm(true, intType),
	})
	tilde := types.NewUnion([]*types.Term{types.NewTerm(true, intType)})
	if !types.Identical(redundant, tilde) {
		t.Fatal("fixture unions are not identical")
	}

	redundantID, err := CanonicalTypeIdentity(redundant)
	if err != nil {
		t.Fatal(err)
	}
	tildeID, err := CanonicalTypeIdentity(tilde)
	if err != nil {
		t.Fatal(err)
	}
	if redundantID != tildeID {
		t.Fatalf("identical union identities differ:\nredundant: %s\ntilde: %s", redundantID, tildeID)
	}
}

func TestCanonicalTypeIdentityUsesBasicKind(t *testing.T) {
	for _, pair := range [][2]string{{"byte", "uint8"}, {"rune", "int32"}} {
		t.Run(pair[0], func(t *testing.T) {
			a := types.Universe.Lookup(pair[0]).Type()
			b := types.Universe.Lookup(pair[1]).Type()
			assertIdentityAgreement(t, a, b, true)
		})
	}

	pkg := loadFixturePackage(t, `package facade

type Box[T any] struct{}
type ByteBox = Box[byte]
type Uint8Box = Box[uint8]
type RuneBox = Box[rune]
type Int32Box = Box[int32]
`)
	for _, pair := range [][2]string{{"ByteBox", "Uint8Box"}, {"RuneBox", "Int32Box"}} {
		t.Run(pair[0]+"Instantiation", func(t *testing.T) {
			a := pkg.Scope().Lookup(pair[0]).Type()
			b := pkg.Scope().Lookup(pair[1]).Type()
			assertIdentityAgreement(t, a, b, true)
		})
	}
}

func TestCanonicalTypeIdentityHandlesRecursiveAnonymousInterfaces(t *testing.T) {
	const childEnv = "DOGGO_REIFY_RECURSIVE_IDENTITY_CHILD"
	if os.Getenv(childEnv) == "1" {
		pkg := types.NewPackage("example.com/fixture", "fixture")
		x := recursiveAnonymousInterface(pkg, "R", false)
		y := recursiveAnonymousInterface(pkg, "S", false)
		z := recursiveAnonymousInterface(pkg, "Different", true)
		assertIdentityAgreement(t, x, y, true)
		assertIdentityAgreement(t, x, z, false)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestCanonicalTypeIdentityHandlesRecursiveAnonymousInterfaces$")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 2000 {
			output = output[len(output)-2000:]
		}
		t.Fatalf("recursive identity subprocess failed: %v\n%s", err, output)
	}
}

// recursiveAnonymousInterface constructs the graph in the requested fixture:
// `type R interface{ M() interface{ R } }; type G[T any] interface{};
// type X = G[interface{ R }]`. Go 1.25's checker/exporter currently panics
// while loading X, so constructors isolate the encoder from that toolchain bug.
func recursiveAnonymousInterface(pkg *types.Package, name string, extraMethod bool) *types.Interface {
	named := types.NewNamed(types.NewTypeName(0, pkg, name, nil), nil, nil)
	resultInterface := types.NewInterfaceType(nil, []types.Type{named})
	result := types.NewTuple(types.NewVar(0, pkg, "", resultInterface))
	methods := []*types.Func{
		types.NewFunc(0, pkg, "M", types.NewSignatureType(nil, nil, nil, nil, result, false)),
	}
	if extraMethod {
		methods = append(methods, types.NewFunc(0, pkg, "N", types.NewSignatureType(nil, nil, nil, nil, nil, false)))
	}
	underlying := types.NewInterfaceType(methods, nil)
	named.SetUnderlying(underlying)
	underlying.Complete()
	resultInterface.Complete()
	rootInterface := types.NewInterfaceType(nil, []types.Type{named})
	rootInterface.Complete()
	return rootInterface
}

func assertIdentityAgreement(t *testing.T, a, b types.Type, identical bool) {
	t.Helper()
	if got := types.Identical(a, b); got != identical {
		t.Fatalf("fixture types.Identical = %v, want %v", got, identical)
	}
	aID, err := CanonicalTypeIdentity(a)
	if err != nil {
		t.Fatal(err)
	}
	bID, err := CanonicalTypeIdentity(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := aID == bID; got != identical {
		t.Fatalf("identity equality = %v, want %v\nA: %s\nB: %s", got, identical, aID, bID)
	}
}

func loadFixturePackage(t *testing.T, source string) *types.Package {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GOPROXY", "off")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/facade\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "facade.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesSizes,
		Dir:  root,
		Env:  append(os.Environ(), "GOWORK=off"),
	}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || len(loaded[0].Errors) > 0 || loaded[0].Types == nil {
		t.Fatalf("load fixture: %+v", loaded)
	}
	return loaded[0].Types
}
