package engine

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

type stubNamespaceLoader struct {
	loadPaths []string
	root      string
	value     types.Value
	required  []string
	resource  string
}

func (l *stubNamespaceLoader) LoadFile(string) (types.Value, error) { return l.value, nil }
func (l *stubNamespaceLoader) Require(ns string) error {
	l.required = append(l.required, ns)
	return nil
}
func (l *stubNamespaceLoader) ResolveResourcePath(string) (string, bool) {
	return l.resource, l.resource != ""
}
func (l *stubNamespaceLoader) GetLoadPaths() []string { return append([]string{}, l.loadPaths...) }
func (l *stubNamespaceLoader) ProjectRoot() string    { return l.root }

type stubNamespaceState struct{ current string }

func (s *stubNamespaceState) CurrentNamespace() string { return s.current }
func (s *stubNamespaceState) SetCurrentNamespace(ns string) {
	s.current = ns
}

func TestNamespaceServicesDelegateToLoaderAndState(t *testing.T) {
	loader := &stubNamespaceLoader{
		loadPaths: []string{"lib"},
		root:      "/project",
		value:     atom.NewString("loaded"),
		resource:  "/project/resources/demo.txt",
	}
	state := &stubNamespaceState{current: "user"}
	svcs := NewNamespaceServices(NamespaceConfig{
		Loader: loader,
		State:  state,
	})

	value, err := svcs.LoadFile("demo.dog")
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != "\"loaded\"" {
		t.Fatalf("unexpected load result: %s", value.String())
	}
	if err := svcs.Require("app.demo"); err != nil {
		t.Fatal(err)
	}
	if len(loader.required) != 1 || loader.required[0] != "app.demo" {
		t.Fatalf("unexpected required namespaces: %#v", loader.required)
	}
	if resource, ok := svcs.ResolveResourcePath("demo.txt"); !ok || resource != "/project/resources/demo.txt" {
		t.Fatalf("resource = %q ok=%v", resource, ok)
	}
	if got := svcs.CurrentNamespace(); got != "user" {
		t.Fatalf("current namespace = %q", got)
	}

	svcs.SetCurrentNamespace("app.core")
	if got := svcs.CurrentNamespace(); got != "app.core" {
		t.Fatalf("current namespace after set = %q", got)
	}
}
