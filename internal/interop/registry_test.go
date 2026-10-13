package interop

import (
	"reflect"
	"sort"
	"sync"
	"testing"

	"doggo/internal/types"
)

type registryAliasTarget interface{ registryAliasMarker() }

type PublicRegistryAlias = registryAliasTarget

type registryDistinctA interface{ registryDistinctMarker() }
type registryDistinctB interface{ registryDistinctMarker() }
type registryDuplicate interface{ registryDuplicateMarker() }
type registryDiagnosticAlpha interface{ registryDiagnosticAlphaMarker() }
type registryDiagnosticZulu interface{ registryDiagnosticZuluMarker() }

func registryTestFactory(interface{}, types.Environment, types.Map) interface{} { return nil }

func TestReifyRegistryUsesExactType(t *testing.T) {
	aliasType := reflect.TypeOf((*PublicRegistryAlias)(nil)).Elem()
	targetType := reflect.TypeOf((*registryAliasTarget)(nil)).Elem()
	if aliasType != targetType {
		t.Fatalf("public alias type = %v, target type = %v", aliasType, targetType)
	}

	RegisterReifyFactory(aliasType, registryTestFactory)
	if _, ok := LookupReifyFactory(targetType); !ok {
		t.Fatalf("LookupReifyFactory(%v) did not find public alias registration", targetType)
	}
}

func TestReifyRegistryDoesNotCollideDistinctInterfaceTypes(t *testing.T) {
	typeA := reflect.TypeOf((*registryDistinctA)(nil)).Elem()
	typeB := reflect.TypeOf((*registryDistinctB)(nil)).Elem()

	RegisterReifyFactory(typeA, registryTestFactory)
	if _, ok := LookupReifyFactory(typeB); ok {
		t.Fatalf("LookupReifyFactory(%v) found registration for distinct type %v", typeB, typeA)
	}
	RegisterReifyFactory(typeB, registryTestFactory)
}

func TestRegisterReifyFactoryRejectsInvalidTypes(t *testing.T) {
	for _, test := range []struct {
		name      string
		typeValue reflect.Type
	}{
		{name: "nil"},
		{name: "non-interface", typeValue: reflect.TypeOf(0)},
		{name: "unnamed interface", typeValue: reflect.TypeOf((*interface{ M() })(nil)).Elem()},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("RegisterReifyFactory(%v) did not panic", test.typeValue)
				}
			}()
			RegisterReifyFactory(test.typeValue, registryTestFactory)
		})
	}
}

func TestRegisterReifyFactoryRejectsDuplicateExactType(t *testing.T) {
	interfaceType := reflect.TypeOf((*registryDuplicate)(nil)).Elem()
	RegisterReifyFactory(interfaceType, registryTestFactory)

	defer func() {
		if recover() == nil {
			t.Fatalf("duplicate RegisterReifyFactory(%v) did not panic", interfaceType)
		}
	}()
	RegisterReifyFactory(interfaceType, registryTestFactory)
}

func TestListReifyFactoriesSortsDiagnosticNames(t *testing.T) {
	RegisterReifyFactory(reflect.TypeOf((*registryDiagnosticZulu)(nil)).Elem(), registryTestFactory)
	RegisterReifyFactory(reflect.TypeOf((*registryDiagnosticAlpha)(nil)).Elem(), registryTestFactory)

	names := ListReifyFactories()
	if !sort.StringsAreSorted(names) {
		t.Fatalf("ListReifyFactories() = %v, want sorted names", names)
	}
}

func TestPackageRegistry(t *testing.T) {
	const packageName = "registry-test"
	RegisterPackage(packageName, map[string]any{
		"Value": 1,
	})

	pkg, ok := LookupPackage(packageName)
	if !ok || pkg["Value"] != 1 {
		t.Fatalf("LookupPackage(%q) = %v, %v", packageName, pkg, ok)
	}
	value, ok := LookupSymbol(packageName, "Value")
	if !ok || value != 1 {
		t.Fatalf("LookupSymbol(%q, Value) = %v, %v", packageName, value, ok)
	}
	if _, ok := LookupSymbol(packageName, "Missing"); ok {
		t.Fatal("missing symbol was found")
	}
	if _, ok := LookupPackage("registry-missing"); ok {
		t.Fatal("missing package was found")
	}

	RegisterPackage(packageName, map[string]any{"Value": 2})
	value, ok = LookupSymbol(packageName, "Value")
	if !ok || value != 2 {
		t.Fatalf("overwritten value = %v, %v", value, ok)
	}
}

func TestResolveInstantiationType(t *testing.T) {
	type testStruct struct{}
	directType := reflect.TypeOf(testStruct{})
	pointerType := reflect.TypeOf((*testStruct)(nil))

	RegisterPackage("registry-type", map[string]any{
		"NewTestStruct": func() *testStruct { return &testStruct{} },
		"TestStruct":    directType,
		"Value":         42,
	})

	tests := []struct {
		name     string
		typeName string
		want     reflect.Type
		found    bool
	}{
		{name: "constructor", typeName: "registry-type.NewTestStruct", want: pointerType, found: true},
		{name: "registered type", typeName: "registry-type.TestStruct", want: directType, found: true},
		{name: "registered value", typeName: "registry-type.Value", want: reflect.TypeOf(42), found: true},
		{name: "missing symbol", typeName: "registry-type.Missing"},
		{name: "invalid name", typeName: "invalid"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := ResolveInstantiationType(test.typeName)
			if found != test.found || got != test.want {
				t.Fatalf("ResolveInstantiationType(%q) = %v, %v; want %v, %v", test.typeName, got, found, test.want, test.found)
			}
		})
	}
}

func TestGeneratedPackagesAreRegistered(t *testing.T) {
	for _, test := range []struct {
		pkg    string
		symbol string
	}{
		{pkg: "fmt", symbol: "Println"},
		{pkg: "math", symbol: "Pi"},
		{pkg: "reflect", symbol: "TypeOf"},
		{pkg: "strings", symbol: "Contains"},
	} {
		t.Run(test.pkg, func(t *testing.T) {
			value, ok := LookupSymbol(test.pkg, test.symbol)
			if !ok || value == nil {
				t.Fatalf("LookupSymbol(%q, %q) = %v, %v", test.pkg, test.symbol, value, ok)
			}
		})
	}
}

func TestPackageRegistryConcurrentAccess(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(value int) {
			defer wg.Done()
			RegisterPackage("registry-concurrent", map[string]any{"Value": value})
			LookupPackage("registry-concurrent")
			LookupSymbol("registry-concurrent", "Value")
		}(i)
	}
	wg.Wait()

	if _, ok := LookupPackage("registry-concurrent"); !ok {
		t.Fatal("concurrently registered package was not found")
	}
}
