package vm

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

const (
	urlParseFunction       = "doggo.url/parse"
	urlFormatFunction      = "doggo.url/format"
	urlResolveFunction     = "doggo.url/resolve"
	urlEncodeQueryFunction = "doggo.url/encode-query"
	urlDecodeQueryFunction = "doggo.url/decode-query"
)

var (
	urlSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*$`)
	urlComponentKeys = map[atom.Keyword]struct{}{
		atom.NewKeyword("scheme"):   {},
		atom.NewKeyword("host"):     {},
		atom.NewKeyword("path"):     {},
		atom.NewKeyword("query"):    {},
		atom.NewKeyword("fragment"): {},
	}
)

var builtinsURL = []BuiltinDefinition{
	{
		Name:       "url-parse*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("url-parse*", args, 1); err != nil {
				return nil, err
			}
			parsed, err := urlParseReference(urlParseFunction, "url", args[0])
			if err != nil {
				return nil, err
			}
			return urlValue(parsed), nil
		}},
	},
	{
		Name:       "url-format*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("url-format*", args, 1); err != nil {
				return nil, err
			}
			formatted, err := urlFormatComponents(args[0])
			if err != nil {
				return nil, err
			}
			return atom.NewString(formatted), nil
		}},
	},
	{
		Name:       "url-resolve*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("url-resolve*", args, 2); err != nil {
				return nil, err
			}
			base, err := urlParseReference(urlResolveFunction, "base", args[0])
			if err != nil {
				return nil, err
			}
			if !base.IsAbs() {
				return nil, urlArgumentError(urlResolveFunction, "base", "absolute hierarchical URL", args[0], "base must be absolute")
			}
			reference, err := urlParseReference(urlResolveFunction, "ref", args[1])
			if err != nil {
				return nil, err
			}
			resolved := base.ResolveReference(reference)
			if err := urlValidateReference(resolved); err != nil {
				return nil, urlArgumentError(urlResolveFunction, "ref", "hierarchical URL reference", args[1], "resolved URL is invalid: "+err.Error())
			}
			if err := urlValidateRoundTripEquivalence(resolved); err != nil {
				return nil, urlArgumentError(urlResolveFunction, "ref", "hierarchical URL reference", args[1], "resolved URL is ambiguous: "+err.Error())
			}
			return atom.NewString(resolved.String()), nil
		}},
	},
	{
		Name:       "url-encode-query*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("url-encode-query*", args, 1); err != nil {
				return nil, err
			}
			values, err := urlValuesFromDoggo(args[0])
			if err != nil {
				return nil, err
			}
			return atom.NewString(values.Encode()), nil
		}},
	},
	{
		Name:       "url-decode-query*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("url-decode-query*", args, 1); err != nil {
				return nil, err
			}
			query, ok := args[0].(atom.String)
			if !ok || !utf8.ValidString(string(query)) {
				return nil, urlArgumentError(urlDecodeQueryFunction, "query", "valid UTF-8 string", args[0], "query must be a valid UTF-8 string")
			}
			values, err := url.ParseQuery(string(query))
			if err != nil {
				return nil, urlArgumentError(urlDecodeQueryFunction, "query", "valid raw query", args[0], "query is invalid: "+err.Error())
			}
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			pairs := make([]types.Value, 0, len(values)*2)
			for _, key := range keys {
				if !utf8.ValidString(key) {
					return nil, urlArgumentError(urlDecodeQueryFunction, "query", "query decoding to valid UTF-8", args[0], "query key is not valid UTF-8")
				}
				items := values[key]
				decoded := make([]types.Value, len(items))
				for i, item := range items {
					if !utf8.ValidString(item) {
						return nil, urlArgumentError(urlDecodeQueryFunction, "query", "query decoding to valid UTF-8", args[0], "query value is not valid UTF-8")
					}
					decoded[i] = atom.NewString(item)
				}
				pairs = append(pairs, atom.NewString(key), coll.NewVector(decoded...))
			}
			return coll.NewMapFromPairs(pairs), nil
		}},
	},
}

func urlArgumentError(function, argument, expected string, actual types.Value, message string) error {
	return argumentVMError(function, argument, expected, actual, function+": "+message)
}

func urlParseReference(function, argument string, value types.Value) (*url.URL, error) {
	text, ok := value.(atom.String)
	if !ok || !utf8.ValidString(string(text)) {
		return nil, urlArgumentError(function, argument, "valid UTF-8 string", value, argument+" must be a valid UTF-8 string")
	}
	parsed, err := url.Parse(string(text))
	if err != nil {
		return nil, urlArgumentError(function, argument, "valid hierarchical URL reference", value, argument+" is invalid: "+err.Error())
	}
	if err := urlValidateReference(parsed); err != nil {
		return nil, urlArgumentError(function, argument, "hierarchical userinfo-free URL reference", value, argument+" is invalid: "+err.Error())
	}
	return parsed, nil
}

func urlValidateReference(parsed *url.URL) error {
	if parsed.Opaque != "" {
		return fmt.Errorf("opaque URLs are not supported")
	}
	if parsed.User != nil {
		return fmt.Errorf("userinfo is not supported")
	}
	if !utf8.ValidString(parsed.Scheme) || !utf8.ValidString(parsed.Host) {
		return fmt.Errorf("scheme or host is not valid UTF-8")
	}
	if !utf8.ValidString(parsed.Path) {
		return fmt.Errorf("decoded path is not valid UTF-8")
	}
	if !utf8.ValidString(parsed.Fragment) {
		return fmt.Errorf("decoded fragment is not valid UTF-8")
	}
	decodedQuery, err := url.QueryUnescape(parsed.RawQuery)
	if err != nil {
		return fmt.Errorf("query has invalid escaping: %w", err)
	}
	if !utf8.ValidString(decodedQuery) {
		return fmt.Errorf("decoded query is not valid UTF-8")
	}
	return nil
}

func urlValue(parsed *url.URL) types.Value {
	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("scheme"), atom.NewString(parsed.Scheme),
		atom.NewKeyword("host"), atom.NewString(parsed.Host),
		atom.NewKeyword("path"), atom.NewString(parsed.EscapedPath()),
		atom.NewKeyword("query"), atom.NewString(parsed.RawQuery),
		atom.NewKeyword("fragment"), atom.NewString(parsed.Fragment),
	})
}

func urlFormatComponents(value types.Value) (string, error) {
	components, ok := value.(types.Map)
	if !ok {
		return "", urlArgumentError(urlFormatFunction, "components", "map with keyword URL component keys", value, "components must be a map")
	}
	for _, rawKey := range sequableToSlice(components.Keys()) {
		key, ok := rawKey.(atom.Keyword)
		if !ok || !utf8.ValidString(string(key)) {
			return "", urlArgumentError(urlFormatFunction, "components", "map with valid UTF-8 keyword URL component keys", rawKey, "component keys must be valid UTF-8 keywords")
		}
		if _, ok := urlComponentKeys[key]; !ok {
			return "", urlArgumentError(urlFormatFunction, "components", "map containing only :scheme, :host, :path, :query, and :fragment", rawKey, "unsupported component "+key.String())
		}
	}

	values := make(map[string]string, len(urlComponentKeys))
	for key := range urlComponentKeys {
		entry := components.Find(key)
		if entry == nil {
			continue
		}
		text, ok := entry.Value.(atom.String)
		name := string(key)
		if !ok || !utf8.ValidString(string(text)) {
			return "", urlArgumentError(urlFormatFunction, name, "valid UTF-8 string", entry.Value, name+" must be a valid UTF-8 string")
		}
		values[name] = string(text)
	}

	if values["scheme"] != "" && !urlSchemePattern.MatchString(values["scheme"]) {
		return "", urlArgumentError(urlFormatFunction, "scheme", "[A-Za-z][A-Za-z0-9+.-]*", atom.NewString(values["scheme"]), "scheme is invalid")
	}
	if err := urlValidateHost(values["host"]); err != nil {
		return "", urlArgumentError(urlFormatFunction, "host", "host without userinfo, path, query, or fragment", atom.NewString(values["host"]), "host is invalid: "+err.Error())
	}

	path, err := url.PathUnescape(values["path"])
	if err != nil || !utf8.ValidString(path) {
		return "", urlArgumentError(urlFormatFunction, "path", "escaped path decoding to valid UTF-8", atom.NewString(values["path"]), "path is invalid")
	}
	formatted := &url.URL{
		Scheme:   values["scheme"],
		Host:     values["host"],
		Path:     path,
		RawPath:  values["path"],
		RawQuery: values["query"],
		Fragment: values["fragment"],
	}
	if formatted.EscapedPath() != values["path"] {
		return "", urlArgumentError(urlFormatFunction, "path", "exact escaped path text", atom.NewString(values["path"]), "path cannot be preserved exactly")
	}
	decodedQuery, err := url.QueryUnescape(values["query"])
	if err != nil || !utf8.ValidString(decodedQuery) {
		return "", urlArgumentError(urlFormatFunction, "query", "raw query decoding to valid UTF-8", atom.NewString(values["query"]), "query is invalid")
	}
	if strings.Contains(values["query"], "#") {
		return "", urlArgumentError(urlFormatFunction, "query", "raw query without fragment delimiter", atom.NewString(values["query"]), "query must not contain #")
	}
	if formatted.Host == "" && len(formatted.Path) >= 2 && formatted.Path[:2] == "//" {
		return "", urlArgumentError(urlFormatFunction, "components", "unambiguous hierarchical URL components", value, "hostless path must not start with //")
	}

	if err := urlValidateRoundTripEquivalence(formatted); err != nil {
		return "", urlArgumentError(urlFormatFunction, "components", "unambiguous hierarchical URL components", value, "components are invalid: "+err.Error())
	}
	return formatted.String(), nil
}

func urlValidateHost(host string) error {
	if host == "" {
		return nil
	}
	parsed, err := url.Parse("//" + host)
	if err != nil {
		return err
	}
	if parsed.User != nil || parsed.Host != host || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("value is not a host-only component")
	}
	return nil
}

func urlValidateRoundTripEquivalence(original *url.URL) error {
	serialized := original.String()
	if !utf8.ValidString(serialized) {
		return fmt.Errorf("serialized URL is not valid UTF-8")
	}
	reparsed, err := url.Parse(serialized)
	if err != nil {
		return err
	}
	if err := urlValidateReference(reparsed); err != nil {
		return err
	}
	if reparsed.Scheme != original.Scheme ||
		reparsed.Host != original.Host ||
		reparsed.EscapedPath() != original.EscapedPath() ||
		reparsed.RawQuery != original.RawQuery ||
		reparsed.Fragment != original.Fragment {
		return fmt.Errorf("components change when serialized")
	}
	return nil
}

func urlValuesFromDoggo(value types.Value) (url.Values, error) {
	query, ok := value.(types.Map)
	if !ok {
		return nil, urlArgumentError(urlEncodeQueryFunction, "query", "map from strings to strings or string vectors", value, "query must be a map")
	}
	values := make(url.Values, query.Count())
	for _, rawKey := range sequableToSlice(query.Keys()) {
		key, ok := rawKey.(atom.String)
		if !ok || !utf8.ValidString(string(key)) {
			return nil, urlArgumentError(urlEncodeQueryFunction, "query", "map with valid UTF-8 string keys", rawKey, "query keys must be valid UTF-8 strings")
		}
		rawValue := query.Get(key)
		switch item := rawValue.(type) {
		case atom.String:
			if !utf8.ValidString(string(item)) {
				return nil, urlArgumentError(urlEncodeQueryFunction, string(key), "valid UTF-8 string or vector of strings", item, "query value must be valid UTF-8")
			}
			values[string(key)] = []string{string(item)}
		case *coll.Vector:
			items := make([]string, len(item.Items))
			for i, rawItem := range item.Items {
				text, ok := rawItem.(atom.String)
				if !ok || !utf8.ValidString(string(text)) {
					return nil, urlArgumentError(urlEncodeQueryFunction, string(key), "valid UTF-8 string or vector of strings", rawItem, "query vector values must be valid UTF-8 strings")
				}
				items[i] = string(text)
			}
			values[string(key)] = items
		default:
			return nil, urlArgumentError(urlEncodeQueryFunction, string(key), "valid UTF-8 string or vector of strings", rawValue, "query values must be strings or vectors of strings")
		}
	}
	return values, nil
}
