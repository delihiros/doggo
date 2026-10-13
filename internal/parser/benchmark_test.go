package parser

import (
	"strings"
	"testing"

	"doggo/internal/lexer"
)

// tokenize はコードをトークン化するヘルパー関数
func tokenize(b *testing.B, code string) []lexer.Token {
	b.Helper()
	l := lexer.New(code)
	tokens, err := l.Tokenize()
	if err != nil {
		b.Fatalf("tokenize error: %v", err)
	}
	return tokens
}

// BenchmarkParseSimpleExpression は単純な式のパースベンチマーク
func BenchmarkParseSimpleExpression(b *testing.B) {
	tokens := tokenize(b, "(+ 1 2)")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseNestedExpression はネストした式のパースベンチマーク
func BenchmarkParseNestedExpression(b *testing.B) {
	tokens := tokenize(b, "(+ (* 2 3) (- 10 (/ 20 4)))")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseVector はベクターのパースベンチマーク
func BenchmarkParseVector(b *testing.B) {
	tokens := tokenize(b, "[1 2 3 4 5 6 7 8 9 10]")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseMap はマップのパースベンチマーク
func BenchmarkParseMap(b *testing.B) {
	tokens := tokenize(b, "{:a 1 :b 2 :c 3 :d 4 :e 5}")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseSet はセットのパースベンチマーク
func BenchmarkParseSet(b *testing.B) {
	tokens := tokenize(b, "#{:a :b :c :d :e}")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseFunctionDefinition は関数定義のパースベンチマーク
func BenchmarkParseFunctionDefinition(b *testing.B) {
	tokens := tokenize(b, "(defn add [x y] (+ x y))")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseQuotedExpression はクォート式のパースベンチマーク
func BenchmarkParseQuotedExpression(b *testing.B) {
	tokens := tokenize(b, "'(1 2 3 4 5)")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseComplexCode は複雑なコードのパースベンチマーク
func BenchmarkParseComplexCode(b *testing.B) {
	code := `
		(defn process-data [data]
			(let [filtered (filter even? data)
				  doubled (map #(* % 2) filtered)
				  result (reduce + 0 doubled)]
				{:filtered filtered
				 :doubled doubled
				 :result result}))
	`
	tokens := tokenize(b, code)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseDeepNesting は深くネストした構造のパースベンチマーク
func BenchmarkParseDeepNesting(b *testing.B) {
	// 深さ10のネストしたリスト
	code := "((((((((((x))))))))))"
	tokens := tokenize(b, code)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseLargeVector は大きなベクターのパースベンチマーク
func BenchmarkParseLargeVector(b *testing.B) {
	// 100要素のベクター
	var parts []string
	for i := 0; i < 100; i++ {
		parts = append(parts, "1")
	}
	code := "[" + strings.Join(parts, " ") + "]"
	tokens := tokenize(b, code)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkParseLargeMap は大きなマップのパースベンチマーク
func BenchmarkParseLargeMap(b *testing.B) {
	// 50エントリのマップ
	var parts []string
	for i := 0; i < 50; i++ {
		parts = append(parts, ":key"+string(rune('a'+i%26)))
		parts = append(parts, "1")
	}
	code := "{" + strings.Join(parts, " ") + "}"
	tokens := tokenize(b, code)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(tokens)
		_, err := p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}

// BenchmarkTokenize はトークナイズのベンチマーク
func BenchmarkTokenize(b *testing.B) {
	code := "(defn add [x y] (+ x y))"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := lexer.New(code)
		_, err := l.Tokenize()
		if err != nil {
			b.Fatalf("tokenize error: %v", err)
		}
	}
}

// BenchmarkTokenizeComplex は複雑なコードのトークナイズベンチマーク
func BenchmarkTokenizeComplex(b *testing.B) {
	code := `
		(defn process-data [data]
			(let [filtered (filter even? data)
				  doubled (map #(* % 2) filtered)
				  result (reduce + 0 doubled)]
				{:filtered filtered
				 :doubled doubled
				 :result result}))
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := lexer.New(code)
		_, err := l.Tokenize()
		if err != nil {
			b.Fatalf("tokenize error: %v", err)
		}
	}
}

// BenchmarkFullParse はトークナイズとパースを合わせたベンチマーク
func BenchmarkFullParse(b *testing.B) {
	code := "(defn add [x y] (+ x y))"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := lexer.New(code)
		tokens, err := l.Tokenize()
		if err != nil {
			b.Fatalf("tokenize error: %v", err)
		}
		p := New(tokens)
		_, err = p.Parse()
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
	}
}
