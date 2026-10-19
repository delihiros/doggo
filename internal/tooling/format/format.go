package format

import (
	"strings"

	"doggo/internal/lexer"
)

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
	// The last character token's end is a floor for trimming this line.
	characterEnd int
}

type sourceLine struct {
	text   string
	ending string
	// A raw CR/LF character owns the first byte of this physical ending.
	literalEnding bool
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
			trimmed := strings.TrimRight(text, " \t\r")
			end := max(len(trimmed), newState.characterEnd)
			text = text[:end]
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

		// Keep literal newline bytes, including raw character payloads.
		// Normalize only endings that are outside literals.
		ending := line.ending
		if !newState.inString && !line.literalEnding && (ending != "" || text != "") {
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
	characterEnd := 0
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
			} else if source[i] == '\\' {
				characterEnd = characterLiteralEnd(source, i)
				i = characterEnd - 1
				// A raw CR/LF character still needs the line-ending path below.
				if source[i] == '\r' || source[i] == '\n' {
					i--
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
		lines = append(lines, sourceLine{text: source[start:i], ending: source[i:end], literalEnding: i < characterEnd})
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

// Use the reader's character-token grammar so escaped delimiters are data.
func characterLiteralEnd(source string, start int) int {
	return start + lexer.New(source[start:]).NextToken().EndOffset
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
	s.characterEnd = 0

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

		if ch == '\\' {
			leadingPhase = false
			s.characterEnd = characterLiteralEnd(line, i)
			i = s.characterEnd - 1
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
