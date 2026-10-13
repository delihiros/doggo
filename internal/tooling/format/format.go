package format

import "strings"

// Diagnostic は format の診断情報です。
type Diagnostic struct {
	Line    int
	Column  int
	Message string
}

// Options は整形オプションです。
type Options struct {
	TabSize      int
	InsertSpaces bool
}

type lineState struct {
	inString bool
	escape   bool
}

type sourceLine struct {
	text   string
	ending string
}

// Format は構造ベースで source を整形します。
func Format(source string, opts Options) (string, []Diagnostic, error) {
	if source == "" {
		return "", nil, nil
	}
	tabSize := opts.TabSize
	if tabSize <= 0 {
		tabSize = 2
	}
	insertSpaces := opts.InsertSpaces

	lines := splitSourceLines(source)
	lineEnding := detectLineEnding(lines)

	indentUnit := "\t"
	if insertSpaces {
		indentUnit = strings.Repeat(" ", tabSize)
	}

	var out strings.Builder
	out.Grow(len(source))
	depth := 0
	state := lineState{}

	for _, line := range lines {
		lineStartsInString := state.inString
		leadingClosers, newDepth, newState := analyzeLine(line.text, depth, state)

		text := line.text
		// 行末が文字列内なら、開始行も含めて末尾空白を保存する。
		if !newState.inString {
			text = strings.TrimRight(text, " \t\r")
		}
		if !lineStartsInString {
			text = strings.TrimLeft(text, " \t")
			if text != "" {
				indentDepth := depth - leadingClosers
				if indentDepth < 0 {
					indentDepth = 0
				}
				out.WriteString(strings.Repeat(indentUnit, indentDepth))
			}
		}
		out.WriteString(text)

		// 文字列内の改行は元のバイト列を保存し、外側だけを正規化する。
		ending := line.ending
		if !newState.inString && (ending != "" || text != "") {
			ending = lineEnding
		}
		out.WriteString(ending)
		depth = newDepth
		state = newState
		if line.ending != "" {
			state.escape = false
		}
	}

	return out.String(), nil, nil
}

func splitSourceLines(source string) []sourceLine {
	var lines []sourceLine
	start := 0
	state := lineState{}
	inComment := false
	for i := 0; i < len(source); i++ {
		if source[i] != '\r' && source[i] != '\n' {
			if inComment {
				continue
			}
			if state.inString {
				if state.escape {
					state.escape = false
				} else if source[i] == '\\' {
					state.escape = true
				} else if source[i] == '"' {
					state.inString = false
				}
			} else if source[i] == '"' {
				state.inString = true
			} else if source[i] == ';' {
				inComment = true
			}
			continue
		}
		end := i + 1
		// Lexer comments end only at LF. A standalone CR inside a comment
		// must remain comment text, including any following quotes/delimiters.
		if source[i] == '\r' && inComment && (end == len(source) || source[end] != '\n') {
			continue
		}
		if source[i] == '\r' && end < len(source) && source[end] == '\n' {
			end++
		}
		lines = append(lines, sourceLine{text: source[start:i], ending: source[i:end]})
		start = end
		i = end - 1
		state.escape = false
		inComment = false
	}
	if start < len(source) {
		lines = append(lines, sourceLine{text: source[start:]})
	}
	return lines
}

func detectLineEnding(lines []sourceLine) string {
	state := lineState{}
	for _, line := range lines {
		_, _, state = analyzeLine(line.text, 0, state)
		if !state.inString {
			if line.ending == "\r\n" {
				return "\r\n"
			}
		}
		if line.ending != "" {
			state.escape = false
		}
	}
	// A bare CR cannot terminate a lexer comment; never introduce it as an
	// external line ending. Literal CR bytes are preserved independently.
	return "\n"
}

func analyzeLine(line string, currentDepth int, state lineState) (int, int, lineState) {
	depth := currentDepth
	leadingClosers := 0
	leadingPhase := true
	s := state

	for i := 0; i < len(line); i++ {
		ch := line[i]
		if s.inString {
			if s.escape {
				s.escape = false
				continue
			}
			if ch == '\\' {
				s.escape = true
				continue
			}
			if ch == '"' {
				s.inString = false
			}
			continue
		}

		if ch == ';' {
			break
		}
		if ch == '"' {
			leadingPhase = false
			s.inString = true
			continue
		}

		if leadingPhase {
			if ch == ' ' || ch == '\t' {
				continue
			}
			switch ch {
			case ')', ']', '}':
				leadingClosers++
			default:
				leadingPhase = false
			}
		}

		switch ch {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		}
	}
	if depth < 0 {
		depth = 0
	}
	return leadingClosers, depth, s
}
