package depsource

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/mod/module"
)

const (
	Path = "path"
	Git  = "git"
	Go   = "go"
)

type Source struct {
	Declaration string
	Kind        string
	Repository  string
	Ref         string
	ModulePath  string
	Query       string
}

func (s Source) Floating() bool {
	return s.Kind == Git && s.Ref == "" || s.Kind == Go && (s.Query == "" || s.Query == "latest")
}

func (s Source) GoQuery() string {
	if s.Query == "" {
		return "latest"
	}
	return s.Query
}

func Parse(declaration string) (Source, error) {
	s := strings.TrimSpace(declaration)
	if s == "" {
		return Source{}, fmt.Errorf("dependency source is empty")
	}
	if path.IsAbs(s) || filepath.IsAbs(filepath.FromSlash(s)) {
		return Source{}, fmt.Errorf("dependency source must not be an absolute path: %s", declaration)
	}

	if isRelativePath(s) {
		if strings.Contains(s, "\\") {
			return Source{}, fmt.Errorf("path dependency source must use forward slashes: %s", declaration)
		}
		clean := path.Clean(s)
		if clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") {
			clean = "./" + clean
		}
		return Source{Declaration: clean, Kind: Path}, nil
	}

	if parsedURL, urlErr := url.Parse(s); urlErr == nil && parsedURL.Scheme != "" {
		return parseGitURL(s, declaration)
	}
	if isSCPSource(s) {
		repo, ref := splitRef(s)
		if ref == "" && strings.HasSuffix(s, "@") {
			return Source{}, fmt.Errorf("git source has empty ref: %s", declaration)
		}
		return Source{Declaration: s, Kind: Git, Repository: repo, Ref: ref}, nil
	}

	modulePath, query := s, ""
	if idx := strings.LastIndex(s, "@"); idx >= 0 {
		modulePath, query = s[:idx], s[idx+1:]
		if query == "" {
			return Source{}, fmt.Errorf("Go module query is empty: %s", declaration)
		}
	}
	if err := module.CheckPath(modulePath); err != nil {
		return Source{}, fmt.Errorf("invalid dependency source %q: %w", declaration, err)
	}
	if query != "" && query != "latest" && (module.CanonicalVersion(query) != query || module.Check(modulePath, query) != nil) {
		return Source{}, fmt.Errorf("invalid Go module query %q for %s", query, modulePath)
	}
	return Source{Declaration: s, Kind: Go, ModulePath: modulePath, Query: query}, nil
}

func parseGitURL(s, declaration string) (Source, error) {
	parsed, err := url.Parse(s)
	if err != nil {
		return Source{}, fmt.Errorf("invalid dependency source %q: %w", declaration, err)
	}
	switch parsed.Scheme {
	case "http", "https", "ssh", "git", "file":
	default:
		return Source{}, fmt.Errorf("invalid dependency source %q: unsupported transport %q", declaration, parsed.Scheme)
	}
	colon := strings.IndexByte(s, ':')
	if colon < 0 || !strings.EqualFold(s[:colon], parsed.Scheme) || !strings.HasPrefix(s[colon:], "://") {
		return Source{}, fmt.Errorf("invalid dependency source %q: malformed %s URL", declaration, parsed.Scheme)
	}

	query := parsed.Query()
	queryRef := strings.TrimSpace(query.Get("ref"))
	query.Del("ref")
	canonicalQuery := query.Encode()
	authorityStart := colon + len("://")
	pathStart := len(s)
	if idx := strings.IndexAny(s[authorityStart:], "/?#"); idx >= 0 {
		pathStart = authorityStart + idx
	}
	pathEnd := pathStart
	if pathStart < len(s) && s[pathStart] == '/' {
		pathEnd = len(s)
		if idx := strings.IndexAny(s[pathStart:], "?#"); idx >= 0 {
			pathEnd = pathStart + idx
		}
	}
	rawPath := s[pathStart:pathEnd]
	rawRepositoryPath := rawPath
	if parsed.Scheme == "file" && strings.Contains(rawPath, ".git@") {
		return Source{}, fmt.Errorf("file:// git source must use ?ref=... instead of @ref: %s", declaration)
	}
	if queryRef != "" {
		if parsed.Scheme != "file" && strings.Contains(rawPath, "@") {
			return Source{}, fmt.Errorf("git source cannot contain both @ref and ?ref=: %s", declaration)
		}
	} else if parsed.Scheme != "file" {
		if idx := strings.LastIndex(rawPath, "@"); idx >= 0 {
			queryRef = strings.TrimSpace(rawPath[idx+1:])
			if queryRef == "" {
				return Source{}, fmt.Errorf("git source has empty ref: %s", declaration)
			}
			rawRepositoryPath = rawPath[:idx]
		}
	}

	repositoryPath, err := url.PathUnescape(rawRepositoryPath)
	if err != nil {
		return Source{}, fmt.Errorf("invalid dependency source %q: %w", declaration, err)
	}
	if parsed.Scheme == "file" {
		if repositoryPath == "" {
			return Source{}, fmt.Errorf("invalid dependency source %q: file URL has empty path", declaration)
		}
	} else if parsed.Host == "" || strings.Trim(repositoryPath, "/") == "" {
		return Source{}, fmt.Errorf("invalid dependency source %q: %s URL requires host and repository path", declaration, parsed.Scheme)
	}
	repository := parsed.Scheme + s[colon:pathStart] + rawRepositoryPath
	if canonicalQuery != "" {
		repository += "?" + canonicalQuery
	}
	if fragment := strings.IndexByte(s[pathEnd:], '#'); fragment >= 0 {
		repository += s[pathEnd+fragment:]
	}
	return Source{Declaration: s, Kind: Git, Repository: repository, Ref: queryRef}, nil
}

func isRelativePath(source string) bool {
	return source == "." || source == ".." || strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../")
}

func isSCPSource(source string) bool {
	at := strings.Index(source, "@")
	if at <= 0 {
		return false
	}
	user := source[:at]
	if strings.ContainsAny(user, "/:\\ \t\r\n") {
		return false
	}
	rest := source[at+1:]
	colon := strings.Index(rest, ":")
	return colon > 0 && colon+1 < len(rest) && !strings.ContainsAny(rest[:colon], "/@ \t\r\n")
}

func splitRef(source string) (string, string) {
	idx := refSeparatorIndex(source)
	if idx < 0 {
		return source, ""
	}
	return source[:idx], strings.TrimSpace(source[idx+1:])
}

func refSeparatorIndex(source string) int {
	lastAt := strings.LastIndex(source, "@")
	if lastAt < 0 {
		return -1
	}

	if isSCPSource(source) {
		firstAt := strings.Index(source, "@")
		colon := strings.Index(source[firstAt+1:], ":")
		pathStart := firstAt + colon + 2
		if lastAt <= pathStart {
			return -1
		}
		return lastAt
	}

	schemeIdx := strings.Index(source, "://")
	if schemeIdx >= 0 {
		authorityStart := schemeIdx + 3
		authorityEnd := len(source)
		if slashRel := strings.Index(source[authorityStart:], "/"); slashRel >= 0 {
			authorityEnd = authorityStart + slashRel
		}
		if lastAt < authorityEnd {
			return -1
		}
	}

	return lastAt
}
