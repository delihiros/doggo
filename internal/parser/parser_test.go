package parser

import (
	"fmt"
	"strings"
	"testing"

	"doggo/internal/lexer"
	"doggo/internal/syntax/lower"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// parseString は文字列をパースして結果を返します。
func parseString(t *testing.T, input string) (types.Value, error) {
	t.Helper()
	l := lexer.New(input)
	tokens, err := l.Tokenize()
	if err != nil {
		return nil, err
	}

	p := New(tokens, input)
	node, err := p.Parse()
	if err != nil {
		return nil, err
	}
	return lower.ToValue(node)
}

// assertNoError はエラーがないことをアサートします。
func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// assertError はエラーがあることをアサートします。
func assertError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Error("expected error but got none")
	}
}

func TestNew(t *testing.T) {
	tokens := []lexer.Token{
		{Type: lexer.IDENT, Literal: "foo"},
		{Type: lexer.EOF, Literal: ""},
	}
	p := New(tokens)
	if p == nil {
		t.Fatal("New() returned nil")
	}
	if p.position != 0 {
		t.Errorf("New().position = %v, want 0", p.position)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty input", "", true},
		{"symbol", "foo", false},
		{"number int", "42", false},
		{"number float", "3.14", false},
		{"string", `"hello"`, false},
		{"keyword", ":foo", false},
		{"empty list", "()", false},
		{"list", "(+ 1 2)", false},
		{"nested list", "(+ (* 2 3) 4)", false},
		{"empty vector", "[]", false},
		{"vector", "[1 2 3]", false},
		{"nested vector", "[1 [2 3]]", false},
		{"empty map", "{}", false},
		{"map", "{:a 1 :b 2}", false},
		{"empty set", "#{}", false},
		{"set", "#{:a :b :c}", false},
		{"set with numbers", "#{1 2 3}", false},
		{"quote", "'foo", false},
		{"quasiquote", "`foo", false},
		{"unquote", "~foo", false},
		{"splice", "~@foo", false},
		{"unclosed list", "(+ 1 2", true},
		{"unclosed vector", "[1 2", true},
		{"unclosed map", "{:a 1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)

			if tt.wantErr {
				assertError(t, err)
				return
			}

			assertNoError(t, err)
			if result == nil {
				t.Error("Parse() returned nil")
			}
		})
	}
}

func TestParseSymbol(t *testing.T) {
	result, err := parseString(t, "foo")
	assertNoError(t, err)
	sym, ok := result.(types.Symbol)
	if !ok {
		t.Fatalf("Parse() = %T, want Symbol", result)
	}
	if sym != types.Symbol("foo") {
		t.Errorf("Parse() = %v, want foo", sym)
	}
}

func TestParseLiterals(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected types.Value
	}{
		{"true literal", "true", atom.True},
		{"false literal", "false", atom.False},
		{"nil literal", "nil", atom.NilValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			assertNoError(t, err)
			if result != tt.expected {
				t.Errorf("Parse(%q) = %v (%T), want %v (%T)", tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}

func TestParseLiteralsAreNotSymbols(t *testing.T) {
	// true, false, nil はシンボルではなくリテラル値としてパースされることを確認
	tests := []struct {
		name  string
		input string
	}{
		{"true is not symbol", "true"},
		{"false is not symbol", "false"},
		{"nil is not symbol", "nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			assertNoError(t, err)
			if _, ok := result.(types.Symbol); ok {
				t.Errorf("Parse(%q) returned Symbol, but should be a literal value", tt.input)
			}
		})
	}
}

func TestParseLiteralsInCollections(t *testing.T) {
	// コレクション内でもリテラルが正しくパースされることを確認
	t.Run("literals in list", func(t *testing.T) {
		result, err := parseString(t, "(list true false nil)")
		assertNoError(t, err)
		list, ok := result.(*coll.List)
		if !ok {
			t.Fatalf("Parse() = %T, want *List", result)
		}
		if len(list.Items) != 4 {
			t.Fatalf("Parse() list length = %v, want 4", len(list.Items))
		}
		// 2番目の要素がtrue
		if list.Items[1] != atom.True {
			t.Errorf("list[1] = %v (%T), want true", list.Items[1], list.Items[1])
		}
		// 3番目の要素がfalse
		if list.Items[2] != atom.False {
			t.Errorf("list[2] = %v (%T), want false", list.Items[2], list.Items[2])
		}
		// 4番目の要素がnil
		if list.Items[3] != atom.NilValue {
			t.Errorf("list[3] = %v (%T), want nil", list.Items[3], list.Items[3])
		}
	})

	t.Run("literals in vector", func(t *testing.T) {
		result, err := parseString(t, "[true false nil]")
		assertNoError(t, err)
		vec, ok := result.(*coll.Vector)
		if !ok {
			t.Fatalf("Parse() = %T, want *Vector", result)
		}
		if len(vec.Items) != 3 {
			t.Fatalf("Parse() vector length = %v, want 3", len(vec.Items))
		}
		if vec.Items[0] != atom.True {
			t.Errorf("vec[0] = %v (%T), want true", vec.Items[0], vec.Items[0])
		}
		if vec.Items[1] != atom.False {
			t.Errorf("vec[1] = %v (%T), want false", vec.Items[1], vec.Items[1])
		}
		if vec.Items[2] != atom.NilValue {
			t.Errorf("vec[2] = %v (%T), want nil", vec.Items[2], vec.Items[2])
		}
	})

	t.Run("literals as map values", func(t *testing.T) {
		result, err := parseString(t, "{:a true :b false :c nil}")
		assertNoError(t, err)
		m, ok := result.(*coll.Map)
		if !ok {
			t.Fatalf("Parse() = %T, want *Map", result)
		}
		if m.Count() != 3 {
			t.Errorf("Parse() map entries = %v, want 3", m.Count())
		}
		// マップの値を確認
		if val := m.Get(atom.Keyword("a")); val != atom.True {
			t.Errorf("map[:a] = %v, want true", val)
		}
		if val := m.Get(atom.Keyword("b")); val != atom.False {
			t.Errorf("map[:b] = %v, want false", val)
		}
		if val := m.Get(atom.Keyword("c")); val != atom.NilValue {
			t.Errorf("map[:c] = %v, want nil", val)
		}
	})
}

func TestParseSimilarSymbols(t *testing.T) {
	// true, false, nil に似ているが異なるシンボルは通常のシンボルとしてパースされることを確認
	tests := []struct {
		name  string
		input string
	}{
		{"true-ish", "true?"},
		{"truely", "truely"},
		{"false-ish", "false?"},
		{"falsely", "falsely"},
		{"nil-ish", "nil?"},
		{"nilly", "nilly"},
		{"my-true", "my-true"},
		{"not-nil", "not-nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			assertNoError(t, err)
			sym, ok := result.(types.Symbol)
			if !ok {
				t.Errorf("Parse(%q) = %T, want Symbol", tt.input, result)
				return
			}
			if string(sym) != tt.input {
				t.Errorf("Parse(%q) = %v, want %v", tt.input, sym, tt.input)
			}
		})
	}
}

func TestParseNumber(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"int", "42"},
		{"float", "3.14"},
		{"zero", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			assertNoError(t, err)
			_, ok := result.(*atom.Number)
			if !ok {
				t.Errorf("Parse() = %T, want *Number", result)
			}
		})
	}
}

func TestParseString(t *testing.T) {
	result, err := parseString(t, `"hello world"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	str, ok := result.(atom.String)
	if !ok {
		t.Fatalf("Parse() = %T, want String", result)
	}
	if str != atom.String("hello world") {
		t.Errorf("Parse() = %v, want hello world", str)
	}
}

func TestParseKeyword(t *testing.T) {
	result, err := parseString(t, ":foo")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	kw, ok := result.(atom.Keyword)
	if !ok {
		t.Fatalf("Parse() = %T, want Keyword", result)
	}
	if kw != atom.Keyword("foo") {
		t.Errorf("Parse() = %v, want foo", kw)
	}
}

func TestParseNamespacedKeywordWithDottedNamespace(t *testing.T) {
	result, err := parseString(t, ":doggo.schema/invalid")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	kw, ok := result.(atom.Keyword)
	if !ok {
		t.Fatalf("Parse() = %T, want Keyword", result)
	}
	if kw != atom.Keyword("doggo.schema/invalid") {
		t.Errorf("Parse() = %v, want doggo.schema/invalid", kw)
	}
}

func TestParseList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "()", 0},
		{"single", "(foo)", 1},
		{"multiple", "(+ 1 2)", 3},
		{"nested", "(+ (* 2 3) 4)", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			list, ok := result.(*coll.List)
			if !ok {
				t.Fatalf("Parse() = %T, want *List", result)
			}
			if len(list.Items) != tt.want {
				t.Errorf("Parse() list length = %v, want %v", len(list.Items), tt.want)
			}
		})
	}
}

func TestParseVector(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "[]", 0},
		{"single", "[1]", 1},
		{"multiple", "[1 2 3]", 3},
		{"nested", "[1 [2 3]]", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			vec, ok := result.(*coll.Vector)
			if !ok {
				t.Fatalf("Parse() = %T, want *Vector", result)
			}
			if len(vec.Items) != tt.want {
				t.Errorf("Parse() vector length = %v, want %v", len(vec.Items), tt.want)
			}
		})
	}
}

func TestParseMap(t *testing.T) {
	result, err := parseString(t, "{:a 1 :b 2}")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	m, ok := result.(*coll.Map)
	if !ok {
		t.Fatalf("Parse() = %T, want *Map", result)
	}
	if m.Count() != 2 {
		t.Errorf("Parse() map entries = %v, want 2", m.Count())
	}
}

func TestParseSet(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedLen int
	}{
		{"empty set", "#{}", 0},
		{"set with keywords", "#{:a :b :c}", 3},
		{"set with numbers", "#{1 2 3}", 3},
		{"set with mixed", "#{:a 1 :b 2}", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			assertNoError(t, err)
			s, ok := result.(*coll.Set)
			if !ok {
				t.Fatalf("Parse() = %T, want *Set", result)
			}
			if s.Count() != tt.expectedLen {
				t.Errorf("Parse() set elements = %v, want %v", s.Count(), tt.expectedLen)
			}
		})
	}
}

func TestParseQuote(t *testing.T) {
	result, err := parseString(t, "'foo")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	list, ok := result.(*coll.List)
	if !ok {
		t.Fatalf("Parse() = %T, want *List", result)
	}
	if len(list.Items) != 2 {
		t.Fatalf("Parse() list length = %v, want 2", len(list.Items))
	}
	sym, ok := list.Items[0].(types.Symbol)
	if !ok || sym != types.Symbol("quote") {
		t.Errorf("Parse() first item = %v, want quote", list.Items[0])
	}
}

func TestParseQuasiquote(t *testing.T) {
	result, err := parseString(t, "`foo")
	assertNoError(t, err)
	list, ok := result.(*coll.List)
	if !ok {
		t.Fatalf("Parse() = %T, want *List", result)
	}
	if len(list.Items) != 2 {
		t.Fatalf("Parse() list length = %v, want 2", len(list.Items))
	}
	sym, ok := list.Items[0].(types.Symbol)
	if !ok || sym != types.Symbol("quasiquote") {
		t.Errorf("Parse() first item = %v, want quasiquote", list.Items[0])
	}
}

func TestParseUnquote(t *testing.T) {
	result, err := parseString(t, "~foo")
	assertNoError(t, err)
	list, ok := result.(*coll.List)
	if !ok {
		t.Fatalf("Parse() = %T, want *List", result)
	}
	if len(list.Items) != 2 {
		t.Fatalf("Parse() list length = %v, want 2", len(list.Items))
	}
	sym, ok := list.Items[0].(types.Symbol)
	if !ok || sym != types.Symbol("unquote") {
		t.Errorf("Parse() first item = %v, want unquote", list.Items[0])
	}
}

func TestParseSplice(t *testing.T) {
	result, err := parseString(t, "~@foo")
	assertNoError(t, err)
	list, ok := result.(*coll.List)
	if !ok {
		t.Fatalf("Parse() = %T, want *List", result)
	}
	if len(list.Items) != 2 {
		t.Fatalf("Parse() list length = %v, want 2", len(list.Items))
	}
	sym, ok := list.Items[0].(types.Symbol)
	if !ok || sym != types.Symbol("splice-unquote") {
		t.Errorf("Parse() first item = %v, want splice-unquote", list.Items[0])
	}
}

func TestParseDiscard(t *testing.T) {
	// #_ は次のフォームを読み飛ばす
	tests := []struct {
		name     string
		input    string
		expected string // 期待される結果の文字列表現
	}{
		{
			"discard number in list",
			"(+ 1 #_ 2 3)",
			"(+ 1 3)", // 2 が読み飛ばされる
		},
		{
			"discard first element",
			"(#_ ignored result)",
			"(result)",
		},
		{
			"discard list",
			"(#_ (expensive-call) :done)",
			"(:done)",
		},
		{
			"discard vector",
			"(#_ [1 2 3] :ok)",
			"(:ok)",
		},
		{
			"multiple discards",
			"(#_ a #_ b c)",
			"(c)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.String() != tt.expected {
				t.Errorf("Parse(%q) = %v, want %v", tt.input, result.String(), tt.expected)
			}
		})
	}
}

func TestParseDiscardAtTopLevel(t *testing.T) {
	// トップレベルで #_ を使うと次のフォームを読み飛ばす
	// ParseAll で複数のフォームをパースする場合の挙動を確認
	l := lexer.New("#_ (skip-this) (keep-this)")
	tokens, err := l.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	p := New(tokens, "#_ (skip-this) (keep-this)")
	nodes, err := p.ParseAll()
	if err != nil {
		t.Fatalf("ParseAll error: %v", err)
	}
	forms, err := lower.ToValues(nodes)
	if err != nil {
		t.Fatalf("lower error: %v", err)
	}

	// (skip-this) は読み飛ばされ、(keep-this) のみが返される
	if len(forms) != 1 {
		t.Fatalf("expected 1 form, got %d: %v", len(forms), forms)
	}

	if forms[0].String() != "(keep-this)" {
		t.Errorf("expected (keep-this), got %v", forms[0].String())
	}
}

func TestParseSkipsTopLevelDiscardForSingleRead(t *testing.T) {
	result, err := parseString(t, "#_ 1 2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := result.String(); got != "2" {
		t.Fatalf("Parse() = %s, want 2", got)
	}
}

func TestParseNextSkipsTopLevelDiscard(t *testing.T) {
	tokens, err := lexer.New("#_ 1 2").Tokenize()
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	p := New(tokens, "#_ 1 2")
	node, err := p.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext error: %v", err)
	}
	form, err := lower.ToValue(node)
	if err != nil {
		t.Fatalf("lower error: %v", err)
	}
	if form == nil || form.String() != "2" {
		t.Fatalf("ParseNext = %v, want 2", form)
	}

	eof, err := p.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext eof error: %v", err)
	}
	if eof != nil {
		t.Fatalf("expected nil at eof, got %v", eof)
	}
}

func TestParseNestedTopLevelDiscardMatchesClojure(t *testing.T) {
	_, err := parseString(t, "#_ #_ 1 2")
	if err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("expected unexpected EOF for nested top-level discard, got %v", err)
	}
}

func TestParseNestedDiscardInsideCollectionMatchesClojure(t *testing.T) {
	result, err := parseString(t, "(#_ #_ 1 2)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := result.String(); got != "()" {
		t.Fatalf("Parse() = %s, want ()", got)
	}
}

func TestParseMapOddElements(t *testing.T) {
	// マップは偶数個の要素が必要
	_, err := parseString(t, "{:a 1 :b}")
	assertError(t, err)
}

func TestParseNestedCollections(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"deeply nested list", "((((((foo))))))", false},
		{"deeply nested vector", "[[[[[[foo]]]]]]", false},
		{"mixed nesting", "([{([{foo}])}])", true}, // マップは偶数個の要素が必要
		// ネストしたマップのパースは複雑なため、より単純なケースに変更
		{"valid mixed nesting", "([{:a 1}])", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			if tt.wantErr {
				assertError(t, err)
			} else {
				assertNoError(t, err)
				if result == nil {
					t.Error("Parse() returned nil")
				}
			}
		})
	}
}

func TestParseUnexpectedEOF(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"unclosed list", "(+ 1 2"},
		{"unclosed vector", "[1 2"},
		{"unclosed map", "{:a 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseString(t, tt.input)
			assertError(t, err)
		})
	}
}

func TestParseMethodCall(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, result types.Value)
	}{
		{
			name:  "method call with object and method",
			input: "(. obj method)",
			check: func(t *testing.T, result types.Value) {
				list, ok := result.(*coll.List)
				if !ok {
					t.Fatalf("expected *List, got %T", result)
				}
				if len(list.Items) != 3 {
					t.Fatalf("expected 3 items, got %d", len(list.Items))
				}
				// 最初の要素は.シンボル
				if sym, ok := list.Items[0].(types.Symbol); !ok || string(sym) != "." {
					t.Errorf("expected '.' symbol, got %v", list.Items[0])
				}
				// 2番目の要素はobjシンボル
				if sym, ok := list.Items[1].(types.Symbol); !ok || string(sym) != "obj" {
					t.Errorf("expected 'obj' symbol, got %v", list.Items[1])
				}
				// 3番目の要素はmethodシンボル
				if sym, ok := list.Items[2].(types.Symbol); !ok || string(sym) != "method" {
					t.Errorf("expected 'method' symbol, got %v", list.Items[2])
				}
			},
		},
		{
			name:  "method call with arguments",
			input: "(. obj method arg1 arg2)",
			check: func(t *testing.T, result types.Value) {
				list, ok := result.(*coll.List)
				if !ok {
					t.Fatalf("expected *List, got %T", result)
				}
				if len(list.Items) != 5 {
					t.Fatalf("expected 5 items, got %d", len(list.Items))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			assertNoError(t, err)
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestParseCharacter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantRune rune
		wantStr  string
	}{
		{"single char a", `\a`, 'a', `\a`},
		{"single char z", `\z`, 'z', `\z`},
		{"newline", `\newline`, '\n', `\newline`},
		{"space", `\space`, ' ', `\space`},
		{"tab", `\tab`, '\t', `\tab`},
		{"return", `\return`, '\r', `\return`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			assertNoError(t, err)

			ch, ok := result.(atom.Character)
			if !ok {
				t.Fatalf("Parse() = %T, want Character", result)
			}
			if rune(ch) != tt.wantRune {
				t.Errorf("Parse() rune = %v, want %v", rune(ch), tt.wantRune)
			}
			if ch.String() != tt.wantStr {
				t.Errorf("Parse() String() = %v, want %v", ch.String(), tt.wantStr)
			}
		})
	}
}

func TestParseCharacterInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"multi char", `\ab`},
		{"unknown special", `\foo`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseString(t, tt.input)
			assertError(t, err)
		})
	}
}

// TestParseMetadataOnSymbol tests that ^ applies metadata to symbols
func TestParseMetadataOnSymbol(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedName string
		metaKey      atom.Keyword
		metaValue    types.Value
	}{
		{
			name:         "map metadata on symbol",
			input:        "^{:a 1} foo",
			expectedName: "foo",
			metaKey:      atom.NewKeyword("a"),
			metaValue:    atom.NewNumberInt(1),
		},
		{
			name:         "keyword shorthand metadata",
			input:        "^:private foo",
			expectedName: "foo",
			metaKey:      atom.NewKeyword("private"),
			metaValue:    atom.True,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			assertNoError(t, err)

			// メタ付きシンボルとして取得
			symWithMeta, ok := result.(*types.SymbolWithMeta)
			if !ok {
				t.Fatalf("expected *types.SymbolWithMeta, got %T", result)
			}

			// シンボル名を確認
			if symWithMeta.Name != tt.expectedName {
				t.Errorf("expected name %q, got %q", tt.expectedName, symWithMeta.Name)
			}

			// メタデータを確認
			if symWithMeta.Meta == nil {
				t.Fatal("expected metadata, got nil")
			}
			metaVal := symWithMeta.Meta.Get(tt.metaKey)
			if metaVal == nil {
				t.Fatalf("expected metadata key %v, got nil", tt.metaKey)
			}
			if !metaVal.Equals(tt.metaValue) {
				t.Errorf("expected metadata value %v, got %v", tt.metaValue, metaVal)
			}
		})
	}
}

// TestParseMetadataOnList tests that ^ applies metadata to lists
func TestParseMetadataOnList(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		listLen   int
		metaKey   atom.Keyword
		metaValue types.Value
	}{
		{
			name:      "map metadata on list",
			input:     "^{:doc \"hello\"} (x y)",
			listLen:   2,
			metaKey:   atom.NewKeyword("doc"),
			metaValue: atom.NewString("hello"),
		},
		{
			name:      "keyword shorthand metadata on list",
			input:     "^:macro (defn foo [])",
			listLen:   3,
			metaKey:   atom.NewKeyword("macro"),
			metaValue: atom.True,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseString(t, tt.input)
			assertNoError(t, err)

			// リストとして取得
			list, ok := result.(*coll.List)
			if !ok {
				t.Fatalf("expected *coll.List, got %T", result)
			}

			// リストの長さを確認
			if len(list.Items) != tt.listLen {
				t.Errorf("expected list length %d, got %d", tt.listLen, len(list.Items))
			}

			// メタデータを確認
			if list.Metadata == nil {
				t.Fatal("expected metadata, got nil")
			}
			metaVal := list.Metadata.Get(tt.metaKey)
			if metaVal == nil {
				t.Fatalf("expected metadata key %v, got nil", tt.metaKey)
			}
			if !metaVal.Equals(tt.metaValue) {
				t.Errorf("expected metadata value %v, got %v", tt.metaValue, metaVal)
			}
		})
	}
}

// TestMetadataDoesNotAffectEquality tests that metadata does not affect equality
func TestMetadataDoesNotAffectEquality(t *testing.T) {
	// 同じ名前のシンボルはメタが異なっても等しい
	t.Run("symbols with different meta are equal", func(t *testing.T) {
		sym1, _ := parseString(t, "^{:a 1} foo")
		sym2, _ := parseString(t, "^{:b 2} foo")
		plainSym := types.NewSymbol("foo")

		if !sym1.Equals(sym2) {
			t.Error("symbols with different metadata should be equal")
		}
		if !sym1.Equals(plainSym) {
			t.Error("symbol with meta should equal plain symbol")
		}
	})

	// 同じ要素のリストはメタが異なっても等しい
	t.Run("lists with different meta are equal", func(t *testing.T) {
		list1, _ := parseString(t, "^{:a 1} (x y)")
		list2, _ := parseString(t, "^{:b 2} (x y)")
		plainList := coll.NewList(types.NewSymbol("x"), types.NewSymbol("y"))

		if !list1.Equals(list2) {
			t.Error("lists with different metadata should be equal")
		}
		if !list1.Equals(plainList) {
			t.Error("list with meta should equal plain list")
		}
	})
}

// TestParserErrorMessages はparserのエラーメッセージに行番号とカラム番号が含まれることを確認します。
func TestParserErrorMessages(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedLine   int
		expectedColumn int
		errorContains  string
	}{
		{
			name:           "unexpected EOF in list",
			input:          "(+ 1 2",
			expectedLine:   1,
			expectedColumn: 8, // EOFに達したときの位置
			errorContains:  "unexpected EOF",
		},
		{
			name:           "unexpected EOF in vector",
			input:          "[1 2 3",
			expectedLine:   1,
			expectedColumn: 8, // EOFに達したときの位置
			errorContains:  "unexpected EOF",
		},
		{
			name:           "unexpected EOF multi line",
			input:          "(+ 1\n  2",
			expectedLine:   2,
			expectedColumn: 5, // EOFに達したときの位置
			errorContains:  "unexpected EOF",
		},
		{
			name:           "unexpected token",
			input:          "(+ 1 2}",
			expectedLine:   1,
			expectedColumn: 8, // '}'の位置
			errorContains:  "unexpected token",
		},
		{
			name:           "map odd elements",
			input:          "{:a 1 :b}",
			expectedLine:   1,
			expectedColumn: 10, // '}'トークンの次の位置
			errorContains:  "even number of elements",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			tokens, err := l.Tokenize()
			if err != nil {
				// lexerエラーの場合はスキップ（lexerのテストでカバー）
				t.Skipf("lexer error: %v", err)
			}

			p := New(tokens)
			_, err = p.Parse()
			if err == nil {
				t.Fatal("expected error but got nil")
			}

			errMsg := err.Error()
			// エラーメッセージに行番号が含まれることを確認
			if !strings.Contains(errMsg, fmt.Sprintf("line %d", tt.expectedLine)) {
				t.Errorf("error message should contain 'line %d', got: %s", tt.expectedLine, errMsg)
			}
			// エラーメッセージにカラム番号が含まれることを確認
			if !strings.Contains(errMsg, fmt.Sprintf("column %d", tt.expectedColumn)) {
				t.Errorf("error message should contain 'column %d', got: %s", tt.expectedColumn, errMsg)
			}
			// エラーメッセージに期待する文字列が含まれることを確認
			if !strings.Contains(errMsg, tt.errorContains) {
				t.Errorf("error message should contain '%s', got: %s", tt.errorContains, errMsg)
			}
		})
	}
}

func FuzzParserDoesNotPanic(f *testing.F) {
	for _, seed := range []string{
		"",
		`(+ 1 2)`,
		`(let [x {:a 1}] (get x :a))`,
		`^{:doc "value"} [1 2 3]`,
		`#(+ %1 %2)`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		tokens, err := lexer.New(source).Tokenize()
		if err != nil {
			return
		}
		_, _ = New(tokens, source).ParseAll()
	})
}
