package vm

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestHTTPNamespaceDefaultGET(t *testing.T) {
	method := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method <- r.Method
		w.Header().Add("X-Test", "a")
		w.Header().Add("X-Test", "b")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	runner := MustNewRunner()
	value, err := runner.Run(`(require '[doggo.http :as http]) (http/request ` + strconv.Quote(server.URL) + `)`)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if got := <-method; got != http.MethodGet {
		t.Fatalf("unexpected method: %s", got)
	}

	response := mustMap(t, value)
	status, ok := response.Get(atom.NewKeyword("status")).(types.Number)
	if !ok {
		t.Fatalf("expected numeric status, got %T", response.Get(atom.NewKeyword("status")))
	}
	statusCode, ok := types.Int64Value(status)
	if !ok || statusCode != http.StatusCreated {
		t.Fatalf("unexpected status: %v", status)
	}

	headers := mustMap(t, response.Get(atom.NewKeyword("headers")))
	xTest := mustVector(t, headers.Get(atom.NewString("X-Test")))
	if len(xTest.Items) != 2 || mustString(t, xTest.Items[0]) != "a" || mustString(t, xTest.Items[1]) != "b" {
		t.Fatalf("unexpected X-Test headers: %v", xTest)
	}
	if got := mustString(t, response.Get(atom.NewKeyword("body"))); got != "hello" {
		t.Fatalf("unexpected body: %q", got)
	}
	if got := mustString(t, response.Get(atom.NewKeyword("url"))); got != server.URL {
		t.Fatalf("unexpected URL: %q", got)
	}
}

func TestHTTPNamespaceReturnsErrorStatuses(t *testing.T) {
	tests := []struct {
		status int
		body   string
	}{
		{status: http.StatusNotFound, body: "missing"},
		{status: http.StatusInternalServerError, body: "broken"},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			value, err := MustNewRunner().Run(`(require '[doggo.http :as http]) (http/request ` + strconv.Quote(server.URL) + `)`)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}

			response := mustMap(t, value)
			status, ok := response.Get(atom.NewKeyword("status")).(types.Number)
			if !ok {
				t.Fatalf("expected numeric status, got %T", response.Get(atom.NewKeyword("status")))
			}
			statusCode, exact := types.Int64Value(status)
			if !exact || statusCode != int64(tt.status) {
				t.Fatalf("unexpected status: %v", status)
			}
			if got := mustString(t, response.Get(atom.NewKeyword("body"))); got != tt.body {
				t.Fatalf("unexpected body: %q", got)
			}
		})
	}
}

func TestHTTPNamespaceRejectsInvalidArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("invalid request reached the network")
	}))
	defer server.Close()

	builtin, ok := DefaultBuiltinCatalog().lookup("http-request*")
	if !ok {
		t.Fatal("missing private HTTP builtin")
	}
	emptyOptions := coll.NewMap(nil)
	option := func(key string, value types.Value) types.Value {
		return coll.NewMapFromPairs([]types.Value{atom.NewKeyword(key), value})
	}
	headerOption := func(name, value types.Value) types.Value {
		return option("headers", coll.NewMapFromPairs([]types.Value{name, value}))
	}
	request := func(options string) string {
		return `(http/request ` + strconv.Quote(server.URL) + ` ` + options + `)`
	}
	malformedURL := "http://[::1"
	if _, err := url.Parse(malformedURL); err == nil {
		t.Fatal("malformed URL fixture unexpectedly parsed")
	}
	invalidControlURL := "http://example.test/\x7f"
	if !utf8.ValidString(invalidControlURL) {
		t.Fatal("control URL fixture is not valid UTF-8")
	}
	if _, err := http.NewRequestWithContext(context.Background(), http.MethodGet, invalidControlURL, nil); err == nil {
		t.Fatal("control URL fixture was unexpectedly accepted")
	}

	tests := []struct {
		name       string
		expression string
		directArgs []types.Value
		argument   string
	}{
		{name: "non-string URL", expression: `(http/request 42)`, argument: "url"},
		{name: "malformed URL rejected by url.Parse", directArgs: []types.Value{atom.NewString(malformedURL), emptyOptions}, argument: "url"},
		{
			name:       "URL rejected by http.NewRequestWithContext",
			directArgs: []types.Value{atom.NewString(invalidControlURL), emptyOptions},
			argument:   "url",
		},
		{name: "non-HTTP scheme", expression: `(http/request "ftp://example.test")`, argument: "url"},
		{name: "missing host", expression: `(http/request "http:///path")`, argument: "url"},
		{name: "port without hostname", expression: `(http/request "http://:80")`, argument: "url"},
		{name: "nil options", expression: request(`nil`), argument: "options"},
		{name: "non-map options", expression: request(`42`), argument: "options"},
		{name: "unknown option", expression: request(`{:unknown true}`), argument: "options"},
		{name: "string method", expression: request(`{:method "get"}`), argument: "method"},
		{name: "namespaced keyword method", expression: request(`{:method :doggo.http/get}`), argument: "method"},
		{
			name:       "invalid method token",
			directArgs: []types.Value{atom.NewString(server.URL), option("method", atom.NewKeyword("bad method"))},
			argument:   "method",
		},
		{name: "non-map headers", expression: request(`{:headers 42}`), argument: "headers"},
		{name: "non-string header key", expression: request(`{:headers {:x "value"}}`), argument: "headers"},
		{name: "non-string-or-vector header value", expression: request(`{:headers {"X-Test" 42}}`), argument: "headers"},
		{name: "non-string vector header element", expression: request(`{:headers {"X-Test" ["ok" 42]}}`), argument: "headers"},
		{name: "invalid header name", expression: request(`{:headers {"Bad Header" "value"}}`), argument: "headers"},
		{
			name:       "CR header value",
			directArgs: []types.Value{atom.NewString(server.URL), headerOption(atom.NewString("X-Test"), atom.NewString("bad\rvalue"))},
			argument:   "headers",
		},
		{
			name:       "LF header value",
			directArgs: []types.Value{atom.NewString(server.URL), headerOption(atom.NewString("X-Test"), atom.NewString("bad\nvalue"))},
			argument:   "headers",
		},
		{name: "non-string body", expression: request(`{:body 42}`), argument: "body"},
		{name: "invalid timeout type", expression: request(`{:timeout "soon"}`), argument: "timeout"},
		{name: "negative timeout", expression: request(`{:timeout -1}`), argument: "timeout"},
		{
			name:       "non-finite timeout",
			directArgs: []types.Value{atom.NewString(server.URL), option("timeout", atom.NewNumberFloat(math.Inf(1)))},
			argument:   "timeout",
		},
		{name: "timeout above duration maximum", expression: request(`{:timeout 9223372036855}`), argument: "timeout"},
		{name: "zero max body", expression: request(`{:max-body-bytes 0}`), argument: "max-body-bytes"},
		{name: "fractional max body", expression: request(`{:max-body-bytes 1.5}`), argument: "max-body-bytes"},
		{name: "max int64 max body", expression: request(`{:max-body-bytes 9223372036854775807}`), argument: "max-body-bytes"},
	}

	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.http :as http])`); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data *coll.Map
			if tt.expression != "" {
				value, err := runner.Run(`(try ` + tt.expression + ` (catch [e] (ex-data e)))`)
				if err != nil {
					t.Fatalf("request validation failed outside catch: %v", err)
				}
				data = mustMap(t, value)
			} else {
				_, err := builtin.CtxFn(context.Background(), tt.directArgs...)
				data = httpExceptionData(t, err)
			}

			if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "invalid-argument" {
				t.Fatalf("kind = %s", got)
			}
			if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != httpPublicRequestName {
				t.Fatalf("function = %v", data.Get(atom.NewKeyword("function")))
			}
			if got := mustKeywordString(t, data.Get(atom.NewKeyword("argument"))); got != tt.argument {
				t.Fatalf("argument = %s, want %s", got, tt.argument)
			}
			if got := mustString(t, data.Get(atom.NewKeyword("expected"))); got == "" {
				t.Fatal("expected is empty")
			}
			if data.Get(atom.NewKeyword("actual")) == nil {
				t.Fatal("actual is missing")
			}
		})
	}
}

func TestHTTPNamespaceAcceptsMaximumBodyLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	value, err := MustNewRunner().Run(`
		(require '[doggo.http :as http])
		(http/request ` + strconv.Quote(server.URL) + ` {:max-body-bytes 9223372036854775806})
	`)
	if err != nil {
		t.Fatalf("maximum body limit was rejected: %v", err)
	}
	if got := mustString(t, mustMap(t, value).Get(atom.NewKeyword("body"))); got != "ok" {
		t.Fatalf("body = %q", got)
	}
}

func TestHTTPBuiltinRejectsMalformedUTF8RequestValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("malformed request value reached the network")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	builtin, ok := DefaultBuiltinCatalog().lookup("http-request*")
	if !ok {
		t.Fatal("missing private HTTP builtin")
	}
	option := func(key string, value types.Value) types.Value {
		return coll.NewMapFromPairs([]types.Value{atom.NewKeyword(key), value})
	}
	headerOption := func(name, value types.Value) types.Value {
		return option("headers", coll.NewMapFromPairs([]types.Value{name, value}))
	}
	tests := []struct {
		name     string
		url      types.Value
		options  types.Value
		argument string
	}{
		{name: "URL", url: atom.NewString(server.URL + "/\xff"), options: coll.NewMap(nil), argument: "url"},
		{name: "body", url: atom.NewString(server.URL), options: option("body", atom.NewString("\xff")), argument: "body"},
		{
			name:     "header name",
			url:      atom.NewString(server.URL),
			options:  headerOption(atom.NewString("X-\xff"), atom.NewString("value")),
			argument: "headers",
		},
		{
			name:     "header value",
			url:      atom.NewString(server.URL),
			options:  headerOption(atom.NewString("X-Test"), atom.NewString("\xff")),
			argument: "headers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := builtin.CtxFn(context.Background(), tt.url, tt.options)
			if err == nil {
				t.Fatal("expected malformed UTF-8 to fail")
			}
			if !utf8.ValidString(err.Error()) {
				t.Fatalf("error rendering is not valid UTF-8: %q", err.Error())
			}
			data := httpExceptionData(t, err)
			if !utf8.ValidString(data.String()) {
				t.Fatalf("exception data is not valid UTF-8: %q", data.String())
			}
			if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "invalid-argument" {
				t.Fatalf("kind = %s", got)
			}
			if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != httpPublicRequestName {
				t.Fatalf("function = %v", data.Get(atom.NewKeyword("function")))
			}
			if got := mustKeywordString(t, data.Get(atom.NewKeyword("argument"))); got != tt.argument {
				t.Fatalf("argument = %s, want %s", got, tt.argument)
			}
			if got := mustString(t, data.Get(atom.NewKeyword("actual"))); got != "<invalid UTF-8>" {
				t.Fatalf("raw safe actual = %q, want <invalid UTF-8>", got)
			}
		})
	}
}

func TestHTTPNamespaceTimesOut(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
		_, _ = w.Write([]byte("late"))
	}))
	defer server.Close()
	defer close(block)

	type runResult struct {
		value types.Value
		err   error
	}
	result := make(chan runResult, 1)
	runner := MustNewRunner()
	go func() {
		value, err := runner.Run(`
			(require '[doggo.http :as http])
			(try
			  (http/request ` + strconv.Quote(server.URL) + ` {:timeout 10})
			  (catch [e] (ex-data e)))
		`)
		result <- runResult{value: value, err: err}
	}()

	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("request failed: %v", got.err)
		}
		assertHTTPRuntimeFailure(t, mustMap(t, got.value))
	case <-time.After(time.Second):
		t.Fatal("request did not honor its timeout")
	}
}

func TestHTTPBuiltinHonorsParentCancellation(t *testing.T) {
	builtin, ok := DefaultBuiltinCatalog().lookup("http-request*")
	if !ok {
		t.Fatal("missing private HTTP builtin")
	}
	if builtin.ErrorFunction != httpPublicRequestName {
		t.Fatalf("unexpected private builtin owner: %q", builtin.ErrorFunction)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := builtin.CtxFn(
		ctx,
		atom.NewString("http://example.test"),
		coll.NewMap(nil),
	)
	assertHTTPRuntimeFailure(t, httpExceptionData(t, err))
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("cancellation error does not include cause: %v", err)
	}
}

func TestHTTPNamespaceRejectsOversizedBodies(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "declared Content-Length",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "4")
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "streamed body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.(http.Flusher).Flush()
				_, _ = w.Write([]byte("four"))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			value, err := MustNewRunner().Run(`
				(require '[doggo.http :as http])
				(try
				  (http/request ` + strconv.Quote(server.URL) + ` {:max-body-bytes 3})
				  (catch [e] {:data (ex-data e) :message (ex-message e)}))
			`)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			failure := mustMap(t, value)
			assertHTTPRuntimeFailure(t, mustMap(t, failure.Get(atom.NewKeyword("data"))))
			if got := mustString(t, failure.Get(atom.NewKeyword("message"))); got != "response body exceeds 3 bytes" {
				t.Fatalf("unexpected body limit error: %q", got)
			}
		})
	}
}

func TestHTTPNamespaceRejectsMalformedUTF8Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte{0xff})
	}))
	defer server.Close()

	value, err := MustNewRunner().Run(`
		(require '[doggo.http :as http])
		(try
		  (http/request ` + strconv.Quote(server.URL) + `)
		  (catch [e] (ex-data e)))
	`)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	assertHTTPRuntimeFailure(t, mustMap(t, value))
}

func TestHTTPReadBodyReportsReadFailure(t *testing.T) {
	sentinel := errors.New("sentinel read failure")
	_, err := httpReadResponseBody(iotest.ErrReader(sentinel), -1, 1024)
	assertHTTPRuntimeFailure(t, httpExceptionData(t, err))
	if !strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("read error does not include cause: %v", err)
	}
}

func TestHTTPResponseConversionRejectsMalformedUTF8(t *testing.T) {
	t.Run("header value", func(t *testing.T) {
		finalURL, err := url.Parse("http://example.test/final")
		if err != nil {
			t.Fatal(err)
		}
		response := &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"X-Test": []string{"\xff"},
			},
			Request: &http.Request{URL: finalURL},
		}

		_, err = httpResponseValue(response, []byte("ok"))
		assertHTTPRuntimeFailure(t, httpExceptionData(t, err))
	})

	t.Run("final URL", func(t *testing.T) {
		_, err := httpUTF8ResponseString("final URL", "\xff")
		assertHTTPRuntimeFailure(t, httpExceptionData(t, err))
	})
}

func TestHTTPNamespaceReportsTransportFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverURL := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	value, err := MustNewRunner().Run(`
		(require '[doggo.http :as http])
		(try
		  (http/request ` + strconv.Quote(serverURL) + `)
		  (catch [e] (ex-data e)))
	`)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	assertHTTPRuntimeFailure(t, mustMap(t, value))
}

func TestHTTPNamespaceSendsMethodHeadersAndBodyAndReportsFinalURL(t *testing.T) {
	type requestDetails struct {
		method string
		body   string
		one    string
		many   []string
	}
	finalRequest := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
		case "/final":
			body, _ := io.ReadAll(r.Body)
			finalRequest <- requestDetails{
				method: r.Method,
				body:   string(body),
				one:    r.Header.Get("X-One"),
				many:   r.Header.Values("X-Many"),
			}
			w.Header().Add("Set-Cookie", "a=1")
			w.Header().Add("Set-Cookie", "b=2")
			_, _ = w.Write([]byte("done"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runner := MustNewRunner()
	value, err := runner.Run(`(require '[doggo.http :as http]) (http/request ` + strconv.Quote(server.URL+"/start") + ` {:method :post :headers {"X-One" "value" "X-Many" ["a" "b"]} :body "payload"})`)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	request := <-finalRequest
	if request.method != http.MethodPost {
		t.Fatalf("unexpected method: %s", request.method)
	}
	if request.body != "payload" {
		t.Fatalf("unexpected body: %q", request.body)
	}
	if request.one != "value" {
		t.Fatalf("unexpected X-One header: %q", request.one)
	}
	if len(request.many) != 2 || request.many[0] != "a" || request.many[1] != "b" {
		t.Fatalf("unexpected X-Many headers: %v", request.many)
	}

	response := mustMap(t, value)
	statusValue, ok := response.Get(atom.NewKeyword("status")).(types.Number)
	if !ok {
		t.Fatalf("expected numeric status, got %T", response.Get(atom.NewKeyword("status")))
	}
	status, ok := types.Int64Value(statusValue)
	if !ok || status != http.StatusOK {
		t.Fatalf("unexpected status: %v", statusValue)
	}
	headers := mustMap(t, response.Get(atom.NewKeyword("headers")))
	cookies := mustVector(t, headers.Get(atom.NewString("Set-Cookie")))
	if len(cookies.Items) != 2 || mustString(t, cookies.Items[0]) != "a=1" || mustString(t, cookies.Items[1]) != "b=2" {
		t.Fatalf("unexpected Set-Cookie headers: %v", cookies)
	}
	if got := mustString(t, response.Get(atom.NewKeyword("body"))); got != "done" {
		t.Fatalf("unexpected body: %q", got)
	}
	if got := mustString(t, response.Get(atom.NewKeyword("url"))); got != server.URL+"/final" {
		t.Fatalf("unexpected URL: %q", got)
	}
}

func TestHTTPRequestMalformedOptionKeywordKeepsExceptionUTF8Valid(t *testing.T) {
	options := coll.NewMapFromPairs([]types.Value{
		atom.Keyword("bad\xff"), atom.NilValue,
	})
	_, err := builtinsHTTP[0].Builtin.CtxFn(
		context.Background(),
		atom.NewString("http://example.test"),
		options,
	)
	if err == nil {
		t.Fatal("expected malformed option keyword to fail")
	}
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T: %v", err, err)
	}

	message := mustString(t, mustExceptionBuiltin(t, "ex-message").Fn(vmErr.Value))
	if !utf8.ValidString(message) {
		t.Fatalf("exception message is not valid UTF-8: %q", message)
	}
	data := mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
	if !utf8.ValidString(data.String()) {
		t.Fatalf("exception data is not valid UTF-8: %q", data.String())
	}
	if got := mustString(t, data.Get(atom.NewKeyword("actual"))); got != "<invalid UTF-8>" {
		t.Fatalf("unexpected safe actual value: %q", got)
	}
}

func TestHTTPNamespaceSendsSingleHostHeader(t *testing.T) {
	host := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host <- r.Host
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	runner := MustNewRunner()
	_, err := runner.Run(`(require '[doggo.http :as http]) (http/request ` + strconv.Quote(server.URL) + ` {:headers {"Host" "doggo.test"}})`)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if got := <-host; got != "doggo.test" {
		t.Fatalf("unexpected Host header: %q", got)
	}
}

func TestHTTPNamespaceSendsSingleUserAgentHeader(t *testing.T) {
	userAgent := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent <- r.Header.Values("User-Agent")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	runner := MustNewRunner()
	_, err := runner.Run(`(require '[doggo.http :as http]) (http/request ` + strconv.Quote(server.URL) + ` {:headers {"User-Agent" ["doggo-test"]}})`)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	values := <-userAgent
	if len(values) != 1 || values[0] != "doggo-test" {
		t.Fatalf("unexpected User-Agent header: %v", values)
	}
}

func TestHTTPNamespaceRejectsSpecialHeadersThatCannotBeAppliedExactly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	tests := []struct {
		name    string
		headers string
	}{
		{name: "multiple Host values", headers: `{"Host" ["one.test" "two.test"]}`},
		{name: "empty Host", headers: `{"Host" ""}`},
		{name: "invalid Host", headers: `{"Host" "bad host"}`},
		{name: "empty User-Agent", headers: `{"User-Agent" []}`},
		{name: "multiple User-Agent values", headers: `{"User-Agent" ["one" "two"]}`},
		{name: "Content-Length", headers: `{"Content-Length" "1"}`},
		{name: "Transfer-Encoding", headers: `{"Transfer-Encoding" "chunked"}`},
		{name: "Trailer", headers: `{"Trailer" "X-Trailer"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			value, err := runner.Run(`
				(require '[doggo.http :as http])
				(try
				  (http/request ` + strconv.Quote(server.URL) + ` {:headers ` + tt.headers + `})
				  (catch [e] (ex-data e)))
			`)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}

			data := mustMap(t, value)
			if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "invalid-argument" {
				t.Fatalf("unexpected error kind: %s", got)
			}
			if got := mustKeywordString(t, data.Get(atom.NewKeyword("argument"))); got != "headers" {
				t.Fatalf("unexpected error argument: %s", got)
			}
			if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != "doggo.http/request" {
				t.Fatalf("unexpected error function: %v", data.Get(atom.NewKeyword("function")))
			}
		})
	}
}

func TestHTTPNamespaceRejectsEmptyHostname(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.http :as http])
		(try
		  (http/request "http://:0")
		  (catch [e] (ex-data e)))
	`)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	data := mustMap(t, value)
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "invalid-argument" {
		t.Fatalf("unexpected error kind: %s", got)
	}
	if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != "doggo.http/request" {
		t.Fatalf("unexpected error function: %v", data.Get(atom.NewKeyword("function")))
	}
}

func httpExceptionData(t *testing.T, err error) *coll.Map {
	t.Helper()
	if err == nil {
		t.Fatal("expected request to fail")
	}
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T: %v", err, err)
	}
	return mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
}

func assertHTTPRuntimeFailure(t *testing.T, data *coll.Map) {
	t.Helper()
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "runtime-failure" {
		t.Fatalf("unexpected error kind: %s", got)
	}
	if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != httpPublicRequestName {
		t.Fatalf("unexpected error function: %v", data.Get(atom.NewKeyword("function")))
	}
}
