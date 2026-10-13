package gitdep

import (
	"strings"
	"testing"
)

func TestParseSource(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		wantRepo string
		wantRef  string
	}{
		{
			name:     "https repo",
			source:   "https://github.com/acme/lib.git",
			wantRepo: "https://github.com/acme/lib.git",
		},
		{
			name:     "uppercase https repo",
			source:   "HTTPS://example.com/repo.git",
			wantRepo: "https://example.com/repo.git",
		},
		{
			name:     "http repo",
			source:   "http://github.com/acme/lib.git",
			wantRepo: "http://github.com/acme/lib.git",
		},
		{
			name:     "ssh repo",
			source:   "ssh://git@github.com/acme/lib.git",
			wantRepo: "ssh://git@github.com/acme/lib.git",
		},
		{
			name:     "git repo",
			source:   "git://github.com/acme/lib.git",
			wantRepo: "git://github.com/acme/lib.git",
		},
		{
			name:     "at ref",
			source:   "https://github.com/acme/lib.git@v1.2.3",
			wantRepo: "https://github.com/acme/lib.git",
			wantRef:  "v1.2.3",
		},
		{
			name:     "escaped slash ref",
			source:   "https://example.com/repo.git@feature%2Ffoo",
			wantRepo: "https://example.com/repo.git",
			wantRef:  "feature%2Ffoo",
		},
		{
			name:     "escaped space ref",
			source:   "https://example.com/repo.git@release%20candidate",
			wantRepo: "https://example.com/repo.git",
			wantRef:  "release%20candidate",
		},
		{
			name:     "unicode ref",
			source:   "https://example.com/repo.git@ブランチ",
			wantRepo: "https://example.com/repo.git",
			wantRef:  "ブランチ",
		},
		{
			name:     "ssh scp style keeps host at",
			source:   "git@github.com:acme/lib.git@main",
			wantRepo: "git@github.com:acme/lib.git",
			wantRef:  "main",
		},
		{
			name:     "query ref",
			source:   "https://github.com/acme/lib.git?ref=main",
			wantRepo: "https://github.com/acme/lib.git",
			wantRef:  "main",
		},
		{
			name:     "file url query ref",
			source:   "file:///tmp/lib.git?ref=main",
			wantRepo: "file:///tmp/lib.git",
			wantRef:  "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSource(tt.source)
			if err != nil {
				t.Fatalf("ParseSource(%q): %v", tt.source, err)
			}
			if got.Repo != tt.wantRepo || got.Ref != tt.wantRef {
				t.Fatalf("ParseSource(%q) = repo=%q ref=%q, want repo=%q ref=%q", tt.source, got.Repo, got.Ref, tt.wantRepo, tt.wantRef)
			}
		})
	}
}

func TestParseSourceRejectsInvalidRefCombinations(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{
			name:    "empty",
			source:  " ",
			wantErr: "source is empty",
		},
		{
			name:    "query ref and at ref",
			source:  "https://github.com/acme/lib.git@v1?ref=main",
			wantErr: "cannot contain both @ref and ?ref=",
		},
		{
			name:    "file url at ref",
			source:  "file:///tmp/lib.git@main",
			wantErr: "file:// git source must use ?ref=...",
		},
		{
			name:    "bare module",
			source:  "github.com/acme/lib",
			wantErr: "not a git source",
		},
		{
			name:    "unsupported scheme",
			source:  "ftp://github.com/acme/lib.git",
			wantErr: "unsupported transport",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSource(tt.source)
			if err == nil {
				t.Fatalf("ParseSource(%q) succeeded unexpectedly", tt.source)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseSource(%q) error = %q, want to contain %q", tt.source, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestIdentityKey(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{source: "", want: ""},
		{source: " https://github.com/acme/lib.git@main ", want: "https://github.com/acme/lib.git"},
		{source: "git@github.com:acme/lib.git@main", want: "git@github.com:acme/lib.git"},
		{source: "https://github.com/acme/lib.git?ref=main", want: "https://github.com/acme/lib.git"},
		{source: "https://github.com/acme/lib.git?z=2&a=1", want: "https://github.com/acme/lib.git?a=1&z=2"},
		{source: "https://github.com/acme/lib.git?a=1&z=2", want: "https://github.com/acme/lib.git?a=1&z=2"},
		{source: "https://github.com/acme/lib.git@main?z=2&a=1", want: "https://github.com/acme/lib.git?a=1&z=2"},
		{source: "https://github.com/acme/lib.git?a=1&ref=&z=2", want: "https://github.com/acme/lib.git?a=1&z=2"},
		{source: "https://example.com/repo%40name.git", want: "https://example.com/repo%40name.git"},
		{source: "https://example.com/org%2Frepo.git@main?z=2&a=1", want: "https://example.com/org%2Frepo.git?a=1&z=2"},
		{source: "github.com/acme/lib", want: ""},
	}

	for _, tt := range tests {
		if got := IdentityKey(tt.source); got != tt.want {
			t.Fatalf("IdentityKey(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}
