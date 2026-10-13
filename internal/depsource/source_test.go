package depsource

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		declaration string
		want        Source
	}{
		{"./deps/alias/../math", Source{Declaration: "./deps/math", Kind: Path}},
		{"https://github.com/acme/math.git@v1.2.3", Source{Declaration: "https://github.com/acme/math.git@v1.2.3", Kind: Git, Repository: "https://github.com/acme/math.git", Ref: "v1.2.3"}},
		{"HTTPS://example.com/repo.git", Source{Declaration: "HTTPS://example.com/repo.git", Kind: Git, Repository: "https://example.com/repo.git"}},
		{"file:///tmp/math.git?ref=v1.2.3", Source{Declaration: "file:///tmp/math.git?ref=v1.2.3", Kind: Git, Repository: "file:///tmp/math.git", Ref: "v1.2.3"}},
		{"git@github.com:acme/math.git@v1.2.3", Source{Declaration: "git@github.com:acme/math.git@v1.2.3", Kind: Git, Repository: "git@github.com:acme/math.git", Ref: "v1.2.3"}},
		{"github.com/acme/math", Source{Declaration: "github.com/acme/math", Kind: Go, ModulePath: "github.com/acme/math"}},
		{"github.com/acme/math@latest", Source{Declaration: "github.com/acme/math@latest", Kind: Go, ModulePath: "github.com/acme/math", Query: "latest"}},
		{"github.com/acme/math@v1.2.3", Source{Declaration: "github.com/acme/math@v1.2.3", Kind: Go, ModulePath: "github.com/acme/math", Query: "v1.2.3"}},
		{"github.com/acme/math@v0.0.0-20240102150405-0123456789ab", Source{Declaration: "github.com/acme/math@v0.0.0-20240102150405-0123456789ab", Kind: Go, ModulePath: "github.com/acme/math", Query: "v0.0.0-20240102150405-0123456789ab"}},
	}

	for _, tt := range tests {
		t.Run(tt.declaration, func(t *testing.T) {
			got, err := Parse(tt.declaration)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse(%q) = %#v, want %#v", tt.declaration, got, tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidDeclarations(t *testing.T) {
	for _, declaration := range []string{
		"",
		"/tmp/math",
		"./deps\\math",
		"https:repo.git",
		"file:repo.git",
		"https://",
		"https://github.com",
		"file://",
		"ftp://example.com/math.git",
		"github.com/acme/math@",
		"github.com/acme/math@main",
		"github.com/acme/math@upgrade",
		"github.com/acme/math@patch",
		"github.com/acme/math@1.2.3",
		"github.com/acme/math@v1.2",
	} {
		t.Run(declaration, func(t *testing.T) {
			if _, err := Parse(declaration); err == nil {
				t.Fatalf("Parse(%q) succeeded unexpectedly", declaration)
			}
		})
	}
}

func TestParseCanonicalizesGitQueryParameters(t *testing.T) {
	tests := []struct {
		declaration string
		wantRepo    string
		wantRef     string
	}{
		{"https://example.com/math.git?z=2&a=1", "https://example.com/math.git?a=1&z=2", ""},
		{"https://example.com/math.git@main?z=2&a=1", "https://example.com/math.git?a=1&z=2", "main"},
		{"https://example.com/math.git?z=2&ref=main&a=1", "https://example.com/math.git?a=1&z=2", "main"},
		{"https://example.com/math.git?z=2&ref=&a=1", "https://example.com/math.git?a=1&z=2", ""},
		{"https://example.com/repo%40name.git", "https://example.com/repo%40name.git", ""},
		{"https://example.com/repo%40name.git?ref=main", "https://example.com/repo%40name.git", "main"},
		{"https://example.com/org%2Frepo.git@main", "https://example.com/org%2Frepo.git", "main"},
		{"https://example.com/org%2Frepo.git@main?z=2&a=1", "https://example.com/org%2Frepo.git?a=1&z=2", "main"},
		{"https://example.com/repo.git@feature%2Ffoo", "https://example.com/repo.git", "feature%2Ffoo"},
		{"https://example.com/repo.git@release%20candidate", "https://example.com/repo.git", "release%20candidate"},
		{"https://example.com/repo.git@ブランチ", "https://example.com/repo.git", "ブランチ"},
		{"file:///tmp/repo.git%40main", "file:///tmp/repo.git%40main", ""},
	}
	for _, tt := range tests {
		t.Run(tt.declaration, func(t *testing.T) {
			got, err := Parse(tt.declaration)
			if err != nil {
				t.Fatal(err)
			}
			if got.Repository != tt.wantRepo || got.Ref != tt.wantRef {
				t.Fatalf("Parse(%q) = repo %q, ref %q; want repo %q, ref %q", tt.declaration, got.Repository, got.Ref, tt.wantRepo, tt.wantRef)
			}
		})
	}
}

func TestSourceFloatingAndGoQuery(t *testing.T) {
	tests := []struct {
		source       Source
		wantFloating bool
		wantQuery    string
	}{
		{Source{Kind: Git}, true, "latest"},
		{Source{Kind: Git, Ref: "main"}, false, "latest"},
		{Source{Kind: Go}, true, "latest"},
		{Source{Kind: Go, Query: "latest"}, true, "latest"},
		{Source{Kind: Go, Query: "v1.2.3"}, false, "v1.2.3"},
		{Source{Kind: Path}, false, "latest"},
	}
	for _, tt := range tests {
		if got := tt.source.Floating(); got != tt.wantFloating {
			t.Errorf("%#v.Floating() = %t, want %t", tt.source, got, tt.wantFloating)
		}
		if got := tt.source.GoQuery(); got != tt.wantQuery {
			t.Errorf("%#v.GoQuery() = %q, want %q", tt.source, got, tt.wantQuery)
		}
	}
}
