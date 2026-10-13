package parser

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	doggoerr "doggo/internal/errors"
	"doggo/internal/lexer"
	"doggo/internal/types"
	"doggo/internal/types/coll"
)

func TestParseHashFnRejectsInvalidPlaceholders(t *testing.T) {
	for _, placeholder := range []string{
		"%257",
		"%9223372036854775807",
		"%9223372036854775808",
		"%99999999999999999999999999999999999999",
		"%0",
		"%-1",
		"%+1",
		"%foo",
		"%1x",
	} {
		t.Run(placeholder, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("parsing placeholder panicked: %v", recovered)
				}
			}()
			_, err := parseString(t, "#(+ 1\n  "+placeholder+")")
			var parseErr *doggoerr.DoggoError
			if !errors.As(err, &parseErr) || parseErr.Kind != doggoerr.KindParser {
				t.Fatalf("expected parser error, got %v", err)
			}
			if !strings.Contains(parseErr.Message, "anonymous function") || !strings.Contains(parseErr.Message, placeholder) {
				t.Errorf("error must identify the invalid placeholder: %v", err)
			}
			if parseErr.Position.Line != 2 || parseErr.Position.Column != 3 {
				t.Errorf("error location = %+v, want line 2, column 3", parseErr.Position)
			}
		})
	}
}

func TestParseHashFnRejectsTooManyParametersWithRest(t *testing.T) {
	_, err := parseString(t, "#(vector %256\n  %&)")
	var parseErr *doggoerr.DoggoError
	if !errors.As(err, &parseErr) || parseErr.Kind != doggoerr.KindParser {
		t.Fatalf("expected parser error, got %v", err)
	}
	if !strings.Contains(parseErr.Message, "256") || !strings.Contains(parseErr.Message, "anonymous function") {
		t.Errorf("error must describe the parameter limit: %v", err)
	}
	if parseErr.Position.Line != 2 || parseErr.Position.Column != 3 {
		t.Errorf("error location = %+v, want line 2, column 3", parseErr.Position)
	}
}

func TestParseHashFnRejectsNestedInvalidPlaceholders(t *testing.T) {
	for _, source := range []string{
		"#(vector [%257])",
		"#(vector #{%257})",
		"#(vector {%257 true})",
		"#(vector {true %257})",
		"#(vector %& %256)",
	} {
		t.Run(source, func(t *testing.T) {
			_, err := parseString(t, source)
			var parseErr *doggoerr.DoggoError
			if !errors.As(err, &parseErr) || parseErr.Kind != doggoerr.KindParser {
				t.Fatalf("expected parser error, got %v", err)
			}
			if parseErr.Position.Line != 1 || parseErr.Position.Column != strings.LastIndex(source, "%")+1 {
				t.Errorf("error must locate the invalid placeholder, got %+v", parseErr.Position)
			}
		})
	}
}

func TestParseHashFnErrorWithoutSourceUsesTokenLocation(t *testing.T) {
	tokens, err := lexer.New("#(vector\n  %257)").Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}
	_, err = New(tokens).Parse()
	var parseErr *doggoerr.DoggoError
	if !errors.As(err, &parseErr) || parseErr.Kind != doggoerr.KindParser {
		t.Fatalf("expected parser error, got %v", err)
	}
	for _, tok := range tokens {
		if tok.Literal == "%257" {
			if parseErr.Position.Line != tok.Line || parseErr.Position.Column != tok.Column {
				t.Errorf("error location = %+v, want token line %d, column %d", parseErr.Position, tok.Line, tok.Column)
			}
			return
		}
	}
	t.Fatal("placeholder token not found")
}

func TestParseHashFnParameterBoundaries(t *testing.T) {
	tests := []struct {
		source string
		count  int
		rest   bool
	}{
		{source: "#(%256)", count: 256},
		{source: "#(vector %255 %&)", count: 255, rest: true},
		{source: "#(+ % %2)", count: 2},
		{source: "#(vector %&)", rest: true},
		{source: "#(vector)", count: 0},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			value, err := parseString(t, tt.source)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			fn, ok := value.(*coll.List)
			if !ok || len(fn.Items) != 3 {
				t.Fatalf("expected expanded fn form, got %v", value)
			}
			params, ok := fn.Items[1].(*coll.Vector)
			if !ok {
				t.Fatalf("expected parameter vector, got %v", fn.Items[1])
			}
			wantLen := tt.count
			if tt.rest {
				wantLen += 2
			}
			if len(params.Items) != wantLen {
				t.Fatalf("parameter vector length = %d, want %d", len(params.Items), wantLen)
			}
			if tt.count > 0 && params.Items[0] != types.Symbol("%1") {
				t.Errorf("first parameter = %v, want %%1", params.Items[0])
			}
			if tt.count > 0 && params.Items[tt.count-1] != types.Symbol("%"+strconv.Itoa(tt.count)) {
				t.Errorf("last required parameter = %v, want %%%d", params.Items[tt.count-1], tt.count)
			}
			if tt.rest && (params.Items[wantLen-2] != types.Symbol("&") || params.Items[wantLen-1] != types.Symbol("%&")) {
				t.Errorf("rest parameters = %v, want & %%&", params.Items[wantLen-2:])
			}
			if tt.source == "#(+ % %2)" && fn.Items[2].String() != "(+ %1 %2)" {
				t.Errorf("percent argument expansion = %v, want (+ %%1 %%2)", fn.Items[2])
			}
		})
	}
}
