package lexer

import "testing"

func TestCharacterDelimiterLeavesFollowingTokens(t *testing.T) {
	for _, ch := range `()[]{}";,` {
		t.Run(string(ch), func(t *testing.T) {
			lexer := New(`\` + string(ch) + "] 7")
			for i, want := range []struct {
				typeName TokenType
				literal  string
			}{{CHAR, string(ch)}, {RBRACKET, "]"}, {NUMBER, "7"}, {EOF, ""}} {
				got := lexer.NextToken()
				if got.Type != want.typeName || got.Literal != want.literal {
					t.Fatalf("token %d = %#v, want %v %q", i, got, want.typeName, want.literal)
				}
			}
		})
	}
}

func TestCharacterDelimiterAtEOF(t *testing.T) {
	for _, ch := range `()[]{}";,` {
		lexer := New(`\` + string(ch))
		got := lexer.NextToken()
		if got.Type != CHAR || got.Literal != string(ch) {
			t.Errorf("%q at EOF = %#v, want a character token", ch, got)
		}
		if next := lexer.NextToken(); next.Type != EOF {
			t.Errorf("%q left a trailing token %#v", ch, next)
		}
	}
}
