package vm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

const (
	httpDefaultTimeoutMs  = int64(30_000)
	httpDefaultMaxBody    = int64(8 * 1024 * 1024)
	httpMaxBodyBytes      = int64(1<<63 - 2)
	httpPublicRequestName = "doggo.http/request"
)

type httpRequestOptions struct {
	method       string
	headers      http.Header
	body         string
	hasBody      bool
	timeoutMs    int64
	maxBodyBytes int64
}

var builtinsHTTP = []BuiltinDefinition{{
	Name:       "http-request*",
	Visibility: BuiltinPrivate,
	Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
		if err := CheckArgCountCtx("http-request*", args, 2); err != nil {
			return nil, err
		}
		return httpRequest(ctx, args[0], args[1])
	}},
}}

func httpRequest(ctx context.Context, urlValue, optionsValue types.Value) (types.Value, error) {
	rawURL, ok := urlValue.(atom.String)
	if !ok {
		return nil, httpArgumentError("url", "string", urlValue, "URL must be a string")
	}
	if !utf8.ValidString(string(rawURL)) {
		return nil, httpArgumentError("url", "valid UTF-8 HTTP(S) URL with a host", rawURL, "URL must be valid UTF-8")
	}

	parsedURL, err := url.Parse(string(rawURL))
	if err != nil {
		return nil, httpArgumentError("url", "valid HTTP(S) URL with a host", rawURL, "invalid URL: %v", err)
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return nil, httpArgumentError("url", "HTTP(S) URL with a host", rawURL, "URL scheme must be http or https")
	}
	if parsedURL.Hostname() == "" {
		return nil, httpArgumentError("url", "HTTP(S) URL with a host", rawURL, "URL must include a host")
	}

	options, err := httpParseRequestOptions(optionsValue)
	if err != nil {
		return nil, err
	}

	requestCtx := ctx
	if options.timeoutMs > 0 {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(options.timeoutMs)*time.Millisecond)
		defer cancel()
	}

	var requestBody io.Reader
	if options.hasBody {
		requestBody = strings.NewReader(options.body)
	}

	request, err := http.NewRequestWithContext(requestCtx, options.method, string(rawURL), requestBody)
	if err != nil {
		return nil, httpArgumentError("url", "valid HTTP(S) URL with a host", rawURL, "invalid URL: %v", err)
	}
	httpApplyRequestHeaders(request, options.headers)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, httpRuntimeError("%v", err)
	}
	defer response.Body.Close()

	body, err := httpReadResponseBody(response.Body, response.ContentLength, options.maxBodyBytes)
	if err != nil {
		return nil, err
	}
	return httpResponseValue(response, body)
}

func httpParseRequestOptions(value types.Value) (httpRequestOptions, error) {
	options := httpRequestOptions{
		method:       http.MethodGet,
		headers:      make(http.Header),
		timeoutMs:    httpDefaultTimeoutMs,
		maxBodyBytes: httpDefaultMaxBody,
	}

	optionMap, ok := value.(types.Map)
	if !ok {
		return options, httpArgumentError("options", "map", value, "options must be a map")
	}
	for _, key := range sequableToSlice(optionMap.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok {
			return options, httpArgumentError("options", "map with keyword keys :method, :headers, :body, :timeout, or :max-body-bytes", key, "option keys must be keywords")
		}
		if !utf8.ValidString(string(keyword)) {
			return options, httpArgumentError("options", "valid UTF-8 option keyword", keyword, "option keywords must be valid UTF-8")
		}
		switch keyword {
		case atom.NewKeyword("method"), atom.NewKeyword("headers"), atom.NewKeyword("body"),
			atom.NewKeyword("timeout"), atom.NewKeyword("max-body-bytes"):
		default:
			return options, httpArgumentError("options", "map with only :method, :headers, :body, :timeout, or :max-body-bytes", keyword, "unsupported option %s", keyword)
		}
	}

	if entry := optionMap.Find(atom.NewKeyword("method")); entry != nil {
		method, err := httpRequestMethod(entry.Value)
		if err != nil {
			return options, err
		}
		options.method = method
	}
	if entry := optionMap.Find(atom.NewKeyword("headers")); entry != nil {
		headers, err := httpRequestHeaders(entry.Value)
		if err != nil {
			return options, err
		}
		options.headers = headers
	}
	if entry := optionMap.Find(atom.NewKeyword("body")); entry != nil {
		body, ok := entry.Value.(atom.String)
		if !ok || !utf8.ValidString(string(body)) {
			return options, httpArgumentError("body", "valid UTF-8 string", entry.Value, "body must be a valid UTF-8 string")
		}
		options.body = string(body)
		options.hasBody = true
	}
	if entry := optionMap.Find(atom.NewKeyword("timeout")); entry != nil {
		timeoutMs, errValue := processTimeoutMsValue(httpPublicRequestName, entry.Value)
		if errValue != nil {
			message := errValue.String()
			if builtinErr, ok := errValue.(*Error); ok {
				message = builtinErr.Message
			}
			return options, httpArgumentError("timeout", "non-negative timeout milliseconds or duration map", entry.Value, "%s", message)
		}
		options.timeoutMs = timeoutMs
	}
	if entry := optionMap.Find(atom.NewKeyword("max-body-bytes")); entry != nil {
		number, ok := entry.Value.(types.Number)
		maxBodyBytes, exact := types.Int64Value(number)
		if !ok || !exact || maxBodyBytes < 1 || maxBodyBytes > httpMaxBodyBytes {
			return options, httpArgumentError("max-body-bytes", "exact integer from 1 to 9223372036854775806", entry.Value, "max-body-bytes must be an exact integer from 1 to 9223372036854775806")
		}
		options.maxBodyBytes = maxBodyBytes
	}

	return options, nil
}

func httpRequestMethod(value types.Value) (string, error) {
	keyword, ok := value.(atom.Keyword)
	if !ok || !utf8.ValidString(string(keyword)) || strings.ContainsRune(string(keyword), '/') || !httpValidToken(string(keyword)) {
		return "", httpArgumentError("method", "unqualified keyword naming a valid HTTP method", value, "method must be an unqualified keyword naming a valid HTTP method")
	}
	return strings.ToUpper(string(keyword)), nil
}

func httpRequestHeaders(value types.Value) (http.Header, error) {
	headerMap, ok := value.(types.Map)
	if !ok {
		return nil, httpArgumentError("headers", "map of string names to string or vector-of-string values", value, "headers must be a map")
	}

	headers := make(http.Header)
	sawHost := false
	sawUserAgent := false
	for _, key := range sequableToSlice(headerMap.Keys()) {
		name, ok := key.(atom.String)
		if !ok || !utf8.ValidString(string(name)) || !httpValidToken(string(name)) {
			return nil, httpArgumentError("headers", "valid HTTP token string header names", key, "header names must be valid HTTP token strings")
		}
		switch canonicalName := http.CanonicalHeaderKey(string(name)); canonicalName {
		case "Content-Length", "Transfer-Encoding", "Trailer":
			return nil, httpArgumentError("headers", "headers excluding Content-Length, Transfer-Encoding, and Trailer", name, "Content-Length, Transfer-Encoding, and Trailer are managed by the HTTP transport")
		case "Host":
			sawHost = true
		case "User-Agent":
			sawUserAgent = true
		}

		rawValues := headerMap.Get(key)
		switch values := rawValues.(type) {
		case atom.String:
			if !httpValidHeaderValue(string(values)) {
				return nil, httpArgumentError("headers", "valid UTF-8 string or vector of valid UTF-8 strings", values, "header values must be valid UTF-8 strings without control characters")
			}
			headers.Add(string(name), string(values))
		case *coll.Vector:
			if values == nil {
				return nil, httpArgumentError("headers", "string or vector of strings", rawValues, "header values must be strings or vectors of strings")
			}
			for _, item := range values.Items {
				headerValue, ok := item.(atom.String)
				if !ok || !httpValidHeaderValue(string(headerValue)) {
					return nil, httpArgumentError("headers", "valid UTF-8 string or vector of valid UTF-8 strings", item, "header values must be valid UTF-8 strings without control characters")
				}
				headers.Add(string(name), string(headerValue))
			}
		default:
			return nil, httpArgumentError("headers", "string or vector of strings", rawValues, "header values must be strings or vectors of strings")
		}
	}
	if sawHost && len(headers.Values("Host")) != 1 {
		return nil, httpArgumentError("headers", "Host header with exactly one value", value, "Host header must have exactly one value")
	}
	if sawHost && !httpValidHost(headers.Get("Host")) {
		return nil, httpArgumentError("headers", "Host header containing a valid host and optional port", atom.NewString(headers.Get("Host")), "Host header must contain a valid host and optional port")
	}
	if sawUserAgent && len(headers.Values("User-Agent")) != 1 {
		return nil, httpArgumentError("headers", "User-Agent header with exactly one value", value, "User-Agent header must have exactly one value")
	}
	return headers, nil
}

func httpApplyRequestHeaders(request *http.Request, headers http.Header) {
	for name, values := range headers {
		switch name {
		case "Host":
			request.Host = values[0]
		case "User-Agent":
			request.Header.Set(name, values[0])
		default:
			for _, value := range values {
				request.Header.Add(name, value)
			}
		}
	}
}

func httpValidHost(value string) bool {
	parsed, err := url.Parse("http://" + value)
	return err == nil &&
		parsed.User == nil &&
		parsed.Opaque == "" &&
		parsed.Path == "" &&
		parsed.RawPath == "" &&
		!parsed.ForceQuery &&
		parsed.RawQuery == "" &&
		parsed.Fragment == "" &&
		parsed.RawFragment == "" &&
		!strings.ContainsRune(value, '#') &&
		parsed.Hostname() != ""
}

func httpValidToken(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !('0' <= c && c <= '9') &&
			!('a' <= c && c <= 'z') &&
			!('A' <= c && c <= 'Z') &&
			!strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			return false
		}
	}
	return true
}

func httpValidHeaderValue(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < ' ' && value[i] != '\t' || value[i] == 0x7f {
			return false
		}
	}
	return true
}

func httpReadResponseBody(body io.Reader, contentLength, maxBodyBytes int64) ([]byte, error) {
	if contentLength > maxBodyBytes {
		return nil, httpRuntimeError("response body exceeds %d bytes", maxBodyBytes)
	}

	data, err := io.ReadAll(io.LimitReader(body, maxBodyBytes+1))
	if err != nil {
		return nil, httpRuntimeError("reading response body: %v", err)
	}
	if int64(len(data)) > maxBodyBytes {
		return nil, httpRuntimeError("response body exceeds %d bytes", maxBodyBytes)
	}
	if !utf8.Valid(data) {
		return nil, httpRuntimeError("response body is not valid UTF-8")
	}
	return data, nil
}

func httpResponseValue(response *http.Response, body []byte) (types.Value, error) {
	headerPairs := make([]types.Value, 0, len(response.Header)*2)
	for name, values := range response.Header {
		headerName, err := httpUTF8ResponseString("response header name", name)
		if err != nil {
			return nil, err
		}
		headerValues := make([]types.Value, len(values))
		for i, value := range values {
			headerValue, err := httpUTF8ResponseString("response header value", value)
			if err != nil {
				return nil, err
			}
			headerValues[i] = headerValue
		}
		headerPairs = append(headerPairs, headerName, coll.NewVector(headerValues...))
	}

	if response.Request == nil || response.Request.URL == nil {
		return nil, httpRuntimeError("response is missing its final URL")
	}
	finalURL, err := httpUTF8ResponseString("final URL", response.Request.URL.String())
	if err != nil {
		return nil, err
	}

	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("status"), atom.NewNumberInt(int64(response.StatusCode)),
		atom.NewKeyword("headers"), coll.NewMapFromPairs(headerPairs),
		atom.NewKeyword("body"), atom.NewString(string(body)),
		atom.NewKeyword("url"), finalURL,
	}), nil
}

func httpUTF8ResponseString(label, value string) (atom.String, error) {
	if !utf8.ValidString(value) {
		return "", httpRuntimeError("%s is not valid UTF-8", label)
	}
	return atom.NewString(value), nil
}

func httpArgumentError(argument, expected string, actual types.Value, format string, args ...any) error {
	data := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("kind"), atom.NewKeyword("invalid-argument"),
		atom.NewKeyword("function"), types.Symbol(httpPublicRequestName),
		atom.NewKeyword("argument"), atom.NewKeyword(argument),
		atom.NewKeyword("expected"), atom.NewString(expected),
		atom.NewKeyword("actual"), httpUTF8Actual(actual),
	})
	return &VMError{Value: newExceptionInfo(httpPublicRequestName+": "+fmt.Sprintf(format, args...), data, nil)}
}

func httpUTF8Actual(value types.Value) types.Value {
	if value == nil {
		return atom.NilValue
	}
	if !utf8.ValidString(value.String()) {
		return atom.NewString("<invalid UTF-8>")
	}
	return value
}

func httpRuntimeError(format string, args ...any) error {
	return runtimeVMError(httpPublicRequestName, fmt.Sprintf(format, args...))
}
