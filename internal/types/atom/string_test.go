package atom

import "testing"

func TestString(t *testing.T) {
	tests := []struct {
		name     string
		str      String
		expected string
	}{
		{"simple string", String("foo"), `"foo"`},
		{"empty string", String(""), `""`},
		{"string with spaces", String("foo bar"), `"foo bar"`},
		{"string with quotes", String(`foo"bar`), `"foo\"bar"`},
		{"string with backslash", String(`foo\bar`), `"foo\\bar"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.str.String() != tt.expected {
				t.Errorf("String.String() = %v, want %v", tt.str.String(), tt.expected)
			}
			if tt.str.Type() != "string" {
				t.Errorf("String.Type() = %v, want string", tt.str.Type())
			}
		})
	}
}

func TestString_Display(t *testing.T) {
	tests := []struct {
		name     string
		str      String
		expected string
	}{
		{"simple string", String("foo"), "foo"},
		{"empty string", String(""), ""},
		{"string with spaces", String("foo bar"), "foo bar"},
		{"string with quotes", String(`foo"bar`), `foo"bar`},
		{"string with escapes", String("a\\b\n\t\x00日本語🙂"), "a\\b\n\t\x00日本語🙂"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.str.Display(); got != tt.expected {
				t.Errorf("String.Display() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCharacter_Display(t *testing.T) {
	tests := []struct {
		name        string
		char        Character
		wantDisplay string
		wantString  string
	}{
		{"simple char", Character('a'), "a", `\a`},
		{"space char", Character(' '), " ", `\space`},
		{"newline char", Character('\n'), "\n", `\newline`},
		{"tab char", Character('\t'), "\t", `\tab`},
		{"return char", Character('\r'), "\r", `\return`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.char.Display(); got != tt.wantDisplay {
				t.Errorf("Character.Display() = %q, want %q", got, tt.wantDisplay)
			}
			if got := tt.char.String(); got != tt.wantString {
				t.Errorf("Character.String() = %v, want %v", got, tt.wantString)
			}
		})
	}
}
