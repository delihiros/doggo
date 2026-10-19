package lexer

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	doggoerr "doggo/internal/errors"
)

// TokenType はトークンの種類を表します。
type TokenType int

const (
	ILLEGAL TokenType = iota
	EOF
	IDENT      // 識別子（シンボル）
	NUMBER     // 数値
	STRING     // 文字列
	LPAREN     // (
	RPAREN     // )
	LBRACKET   // [
	RBRACKET   // ]
	LBRACE     // {
	RBRACE     // }
	KEYWORD    // :keyword
	QUOTE      // '
	QUASIQUOTE // `
	UNQUOTE    // ~
	SPLICE     // ~@
	DEREF      // @ (derefリーダーマクロ)
	DOT        // . (メソッド呼び出し用)
	HASHSET    // #{} (セットリテラル)
	HASHFN     // #() (無名関数リテラル)
	VAR_QUOTE  // #' (varリーダーマクロ)
	METADATA   // ^ (メタデータリーダーマクロ)
	REGEX      // #"..." (正規表現リテラル)
	CHAR       // \a, \newline などの文字リテラル
	DISCARD    // #_ (次のフォームを読み飛ばす)
)

// Token はトークンを表します。
type Token struct {
	Type      TokenType
	Literal   string
	Line      int
	Column    int
	Offset    int
	EndOffset int
}

// Lexer は字句解析器です。
type Lexer struct {
	input        string
	position     int  // 現在の位置
	readPosition int  // 次の読み取り位置
	ch           byte // 現在の文字
	line         int
	column       int
	err          error // エラー情報（未閉じ文字列など）
	errLine      int   // エラーが発生した行番号
	errColumn    int   // エラーが発生したカラム番号
}

// New は新しいLexerを作成します。
func New(input string) *Lexer {
	l := &Lexer{
		input:  input,
		line:   1,
		column: 1,
	}
	l.readChar()
	return l
}

// readChar は次の文字を読み込みます。
func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition++
	if l.ch == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}
}

// peekChar は次の文字を先読みします（位置は進めない）。
func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

// NextToken は次のトークンを返します。
func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	// 位置情報を保存（読み込み前に位置を記録）
	line := l.line
	column := l.column
	startOffset := l.position

	switch l.ch {
	case '(':
		return l.readSingleCharToken(LPAREN, line, column, startOffset)
	case ')':
		return l.readSingleCharToken(RPAREN, line, column, startOffset)
	case '[':
		return l.readSingleCharToken(LBRACKET, line, column, startOffset)
	case ']':
		return l.readSingleCharToken(RBRACKET, line, column, startOffset)
	case '{':
		return l.readSingleCharToken(LBRACE, line, column, startOffset)
	case '}':
		return l.readSingleCharToken(RBRACE, line, column, startOffset)
	case '\'':
		return l.readSingleCharToken(QUOTE, line, column, startOffset)
	case '`':
		return l.readSingleCharToken(QUASIQUOTE, line, column, startOffset)
	case '~':
		if l.peekChar() == '@' {
			ch := l.ch
			spliceColumn := column
			l.readChar() // '@'を読み込む
			tok := Token{Type: SPLICE, Literal: string(ch) + string(l.ch), Line: line, Column: spliceColumn, Offset: startOffset}
			l.readChar() // 次の文字に進む
			tok.EndOffset = l.position
			return tok
		}
		return l.readSingleCharToken(UNQUOTE, line, column, startOffset)
	case '.':
		// 次の文字も.の場合は識別子として読み込む（..マクロ用）
		// 次の文字が英字の場合は識別子として読み込む（メソッド呼び出し用）
		if l.peekChar() == '.' || isLetter(l.peekChar()) {
			return l.newTokenWithPosition(IDENT, l.readIdentifier(), line, column, startOffset)
		}
		return l.readSingleCharToken(DOT, line, column, startOffset)
	case '^':
		return l.readSingleCharToken(METADATA, line, column, startOffset)
	case '"':
		str, strLine, strCol, err := l.readString()
		if err != nil {
			// エラー情報をLexerに保存
			l.err = err
			l.errLine = strLine
			l.errColumn = strCol
			// ILLEGALトークンを返す（エラー情報はTokenizeで処理）
			return Token{Type: ILLEGAL, Literal: "", Line: strLine, Column: strCol, Offset: startOffset, EndOffset: l.position}
		}
		return l.newTokenWithPosition(STRING, str, line, column, startOffset)
	case ':':
		return l.newTokenWithPosition(KEYWORD, l.readKeyword(), line, column, startOffset)
	case '#':
		// #' 構文のチェック
		if l.peekChar() == '\'' {
			ch := l.ch
			varQuoteColumn := column
			l.readChar() // '\''を読み込む
			tok := Token{Type: VAR_QUOTE, Literal: string(ch) + string(l.ch), Line: line, Column: varQuoteColumn, Offset: startOffset}
			l.readChar() // 次の文字に進む
			tok.EndOffset = l.position
			return tok
		}
		// #{} 構文のチェック
		if l.peekChar() == '{' {
			ch := l.ch
			hashsetColumn := column
			l.readChar() // '{'を読み込む
			tok := Token{Type: HASHSET, Literal: string(ch) + string(l.ch), Line: line, Column: hashsetColumn, Offset: startOffset}
			l.readChar() // 次の文字に進む
			tok.EndOffset = l.position
			return tok
		}
		// #() 無名関数リテラルのチェック
		if l.peekChar() == '(' {
			hashfnColumn := column
			l.readChar() // '('を読み込む
			tok := Token{Type: HASHFN, Literal: "#(", Line: line, Column: hashfnColumn, Offset: startOffset}
			l.readChar() // 次の文字に進む（'(' の後の文字）
			tok.EndOffset = l.position
			return tok
		}
		// #"..." 正規表現リテラルのチェック
		if l.peekChar() == '"' {
			l.readChar() // '#'を読み込み、次の'"'に進む
			str, strLine, strCol, err := l.readRegex()
			if err != nil {
				l.err = err
				l.errLine = strLine
				l.errColumn = strCol
				return Token{Type: ILLEGAL, Literal: "", Line: strLine, Column: strCol}
			}
			return l.newTokenWithPosition(REGEX, str, line, column, startOffset)
		}
		// #_ discard リーダーマクロ（次のフォームを読み飛ばす）
		if l.peekChar() == '_' {
			discardColumn := column
			l.readChar() // '_'を読み込む
			tok := Token{Type: DISCARD, Literal: "#_", Line: line, Column: discardColumn, Offset: startOffset}
			l.readChar() // 次の文字に進む
			tok.EndOffset = l.position
			return tok
		}
		// #が単独で来た場合は識別子として処理
		return l.newTokenWithPosition(IDENT, l.readIdentifier(), line, column, startOffset)
	case '@':
		return l.readSingleCharToken(DEREF, line, column, startOffset)
	case '\\':
		// 文字リテラル: \a, \newline, \space, \tab, \return
		return l.newTokenWithPosition(CHAR, l.readCharLiteral(), line, column, startOffset)
	case '-':
		// 負の数値のチェック: 次の文字が数字の場合は負の数値として処理
		if isDigit(l.peekChar()) {
			return l.newTokenWithPosition(NUMBER, l.readNegativeNumber(), line, column, startOffset)
		}
		// 数字でない場合は識別子として処理（-はisSpecialCharに含まれる）
		return l.newTokenWithPosition(IDENT, l.readIdentifier(), line, column, startOffset)
	case 0:
		if l.position < len(l.input) {
			// A source NUL is an invalid byte, not physical EOF.
			tok := newToken(ILLEGAL, l.ch, line, column, startOffset, startOffset+1)
			l.readChar()
			return tok
		}
		return Token{Type: EOF, Literal: "", Line: line, Column: column, Offset: startOffset, EndOffset: startOffset}
	default:
		if isDigit(l.ch) {
			return l.newTokenWithPosition(NUMBER, l.readNumber(), line, column, startOffset)
		} else if isLetter(l.ch) || isSpecialChar(l.ch) {
			return l.newTokenWithPosition(IDENT, l.readIdentifier(), line, column, startOffset)
		}
		tok := newToken(ILLEGAL, l.ch, line, column, startOffset, startOffset+1)
		l.readChar()
		return tok
	}
}

// newToken は新しいトークンを作成します。
func newToken(tokenType TokenType, ch byte, line, column, startOffset, endOffset int) Token {
	return Token{
		Type:      tokenType,
		Literal:   string(ch),
		Line:      line,
		Column:    column,
		Offset:    startOffset,
		EndOffset: endOffset,
	}
}

// newTokenWithPosition は指定された位置情報を使用して新しいトークンを作成します。
func (l *Lexer) newTokenWithPosition(tokenType TokenType, literal string, line, column, startOffset int) Token {
	return Token{
		Type:      tokenType,
		Literal:   literal,
		Line:      line,
		Column:    column,
		Offset:    startOffset,
		EndOffset: l.position,
	}
}

// readSingleCharToken は単一文字トークンを読み込みます。
func (l *Lexer) readSingleCharToken(tokenType TokenType, line, column, startOffset int) Token {
	tok := newToken(tokenType, l.ch, line, column, startOffset, startOffset+1)
	l.readChar()
	return tok
}

// skipComment はコメント（;で始まる行末まで）をスキップします。
func (l *Lexer) skipComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

// skipWhitespace は空白文字をスキップします。
func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' || l.ch == ';' || l.ch == ',' {
		if l.ch == ';' {
			l.skipComment()
			continue
		}
		l.readChar()
	}
}

// readIdentifier は識別子を読み込みます。
func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) || isSpecialChar(l.ch) || l.ch == '.' {
		// ドットの場合は、次の文字が数字でないことを確認（数値リテラルと区別）
		if l.ch == '.' {
			// 次の文字が数字の場合は数値リテラルの一部なので、ここで停止
			if isDigit(l.peekChar()) {
				break
			}
		}
		l.readChar()
	}
	return l.input[position:l.position]
}

// readNumberDigits は数字部分、浮動小数点部分、指数部分を読み込みます。
func (l *Lexer) readNumberDigits() {
	// 整数部分を読み込む
	for isDigit(l.ch) {
		l.readChar()
	}
	// 浮動小数点数のサポート
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	// 指数表記のサポート (e/E に続いて任意の +/- と少なくとも1桁の数字)
	if l.ch == 'e' || l.ch == 'E' {
		next := l.peekChar()
		// 次の文字が数字、または +/- で、その次が数字の場合のみ指数部として読み取る
		if isDigit(next) || ((next == '+' || next == '-') && l.isDigitAt(l.readPosition+1)) {
			l.readChar() // 'e' または 'E' を読み込む
			if l.ch == '+' || l.ch == '-' {
				l.readChar() // 符号を読み込む
			}
			for isDigit(l.ch) {
				l.readChar()
			}
		}
	}
}

// isDigitAt は指定位置の文字が数字かどうかを判定します。
func (l *Lexer) isDigitAt(pos int) bool {
	if pos >= len(l.input) {
		return false
	}
	return isDigit(l.input[pos])
}

// readNumber は数値を読み込みます。
func (l *Lexer) readNumber() string {
	position := l.position
	l.readNumberDigits()
	return l.input[position:l.position]
}

// readNegativeNumber は負の数値を読み込みます。
func (l *Lexer) readNegativeNumber() string {
	position := l.position // '-'の位置
	l.readChar()           // '-'を読み込む
	l.readNumberDigits()   // 数字部分を読み込む
	return l.input[position:l.position]
}

// readString は文字列を読み込みます。
// エラーが発生した場合（未閉じの文字列など）、エラー情報を含む構造体を返します。
func (l *Lexer) readString() (string, int, int, error) {
	startLine := l.line
	startColumn := l.column
	var result strings.Builder
	for {
		l.readChar()
		if l.ch == '"' {
			l.readChar()
			return result.String(), startLine, startColumn, nil
		}
		if l.ch == 0 {
			// 文字列が閉じられずにEOFに達した
			return "", startLine, startColumn, doggoerr.LexerError("unclosed string", startLine, startColumn)
		}
		if l.ch == '\\' {
			escaped, err := l.readStringEscape()
			if err != nil {
				return "", startLine, startColumn, err
			}
			result.WriteString(escaped)
		} else {
			result.WriteByte(l.ch)
		}
	}
}

func (l *Lexer) readStringEscape() (string, error) {
	l.readChar()
	switch l.ch {
	case 'n':
		return "\n", nil
	case 't':
		return "\t", nil
	case 'r':
		return "\r", nil
	case 'b':
		return "\b", nil
	case 'f':
		return "\f", nil
	case '\\':
		return "\\", nil
	case '"':
		return `"`, nil
	case 'u':
		return l.readUnicodeEscape()
	case 0:
		return "", doggoerr.LexerError("incomplete escape sequence", l.line, l.column)
	default:
		if isOctalDigit(l.ch) {
			return l.readOctalEscape()
		}
		return "", doggoerr.LexerError("unknown escape sequence", l.line, l.column)
	}
}

func (l *Lexer) readUnicodeEscape() (string, error) {
	codeUnit, err := l.readUnicodeCodeUnit()
	if err != nil {
		return "", err
	}

	if isHighSurrogate(codeUnit) {
		if low, ok := l.peekUnicodeEscapeCodeUnit(); ok && isLowSurrogate(low) {
			l.readChar() // consume '\'
			l.readChar() // consume 'u'

			low, err := l.readUnicodeCodeUnit()
			if err != nil {
				return "", err
			}
			return string(utf16.DecodeRune(rune(codeUnit), rune(low))), nil
		}
		return "", doggoerr.LexerError("high surrogate must be followed by a low surrogate", l.line, l.column)
	}
	if isLowSurrogate(codeUnit) {
		return "", doggoerr.LexerError("low surrogate must follow a high surrogate", l.line, l.column)
	}
	return string(rune(codeUnit)), nil
}

func (l *Lexer) readUnicodeCodeUnit() (uint16, error) {
	var digits [4]byte
	for i := range digits {
		l.readChar()
		if !isHexDigit(l.ch) {
			return 0, doggoerr.LexerError("invalid unicode escape", l.line, l.column)
		}
		digits[i] = l.ch
	}

	value, err := strconv.ParseUint(string(digits[:]), 16, 16)
	if err != nil {
		return 0, doggoerr.LexerError("invalid unicode escape", l.line, l.column)
	}
	return uint16(value), nil
}

func (l *Lexer) readOctalEscape() (string, error) {
	digits := []byte{l.ch}
	maxDigits := 2
	if l.ch <= '3' {
		maxDigits = 3
	}

	for len(digits) < maxDigits && isOctalDigit(l.peekChar()) {
		l.readChar()
		digits = append(digits, l.ch)
	}

	value, err := strconv.ParseUint(string(digits), 8, 8)
	if err != nil {
		return "", doggoerr.LexerError("invalid octal escape", l.line, l.column)
	}
	return string(rune(value)), nil
}

func (l *Lexer) peekUnicodeEscapeCodeUnit() (uint16, bool) {
	if l.peekChar() != '\\' || l.peekCharAt(2) != 'u' {
		return 0, false
	}

	var digits [4]byte
	for i := range digits {
		ch := l.peekCharAt(3 + i)
		if !isHexDigit(ch) {
			return 0, false
		}
		digits[i] = ch
	}

	value, err := strconv.ParseUint(string(digits[:]), 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(value), true
}

func (l *Lexer) peekCharAt(offset int) byte {
	index := l.readPosition + offset - 1
	if index < 0 || index >= len(l.input) {
		return 0
	}
	return l.input[index]
}

// readRegex は正規表現リテラルを読み込みます（#"..."形式）。
// 文字列リテラルと同様のエスケープ規則を使用します。
func (l *Lexer) readRegex() (string, int, int, error) {
	startLine := l.line
	startColumn := l.column
	var result []byte
	for {
		l.readChar()
		if l.ch == '"' {
			l.readChar()
			return string(result), startLine, startColumn, nil
		}
		if l.ch == 0 {
			return "", startLine, startColumn, doggoerr.LexerError("unclosed regex", startLine, startColumn)
		}
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				result = append(result, '\\', 'n')
			case 't':
				result = append(result, '\\', 't')
			case 'r':
				result = append(result, '\\', 'r')
			case '\\':
				result = append(result, '\\', '\\')
			case '"':
				result = append(result, '"')
			case 'd', 'D', 'w', 'W', 's', 'S', 'b', 'B', 'A', 'z', 'Z':
				// 正規表現のメタ文字はそのまま保持
				result = append(result, '\\', l.ch)
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
				// バックリファレンスなどの数字はそのまま保持
				result = append(result, '\\', l.ch)
			case 0:
				return "", startLine, startColumn, doggoerr.LexerError("incomplete escape sequence in regex", l.line, l.column)
			default:
				// その他のエスケープシーケンスはそのまま保持
				result = append(result, '\\', l.ch)
			}
		} else {
			result = append(result, l.ch)
		}
	}
}

// readKeyword はキーワードを読み込みます。
func (l *Lexer) readKeyword() string {
	l.readChar() // ':' をスキップ
	start := l.position
	for isLetter(l.ch) || isDigit(l.ch) || isSpecialChar(l.ch) || l.ch == '.' {
		l.readChar()
	}
	if l.position > start {
		return l.input[start:l.position]
	}
	return ""
}

// readCharLiteral は文字リテラルを読み込みます（\a, \newline など）。
// 区切りは空白、括弧類、ダブルクォートなどです。
func (l *Lexer) readCharLiteral() string {
	l.readChar() // '\' をスキップ
	start := l.position
	// The first escaped delimiter is the character itself, not the next token.
	if l.ch != 0 && isCharTerminator(l.ch) {
		l.readChar()
		return l.input[start:l.position]
	}
	for l.ch != 0 && !isCharTerminator(l.ch) {
		l.readChar()
	}
	if l.position > start {
		return l.input[start:l.position]
	}
	return ""
}

// isCharTerminator は文字リテラルの終端文字かどうかを判定します。
func isCharTerminator(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' ||
		ch == '(' || ch == ')' || ch == '[' || ch == ']' ||
		ch == '{' || ch == '}' || ch == '"' || ch == ';' || ch == ','
}

// isLetter は文字が英字かどうかを判定します。
func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z'
}

// isDigit は文字が数字かどうかを判定します。
func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

func isHexDigit(ch byte) bool {
	return ('0' <= ch && ch <= '9') ||
		('a' <= ch && ch <= 'f') ||
		('A' <= ch && ch <= 'F')
}

func isOctalDigit(ch byte) bool {
	return '0' <= ch && ch <= '7'
}

func isHighSurrogate(value uint16) bool {
	return 0xD800 <= value && value <= 0xDBFF
}

func isLowSurrogate(value uint16) bool {
	return 0xDC00 <= value && value <= 0xDFFF
}

// isSpecialChar はLispで使用される特殊文字かどうかを判定します。
func isSpecialChar(ch byte) bool {
	return ch == '-' || ch == '_' || ch == '+' || ch == '*' || ch == '/' ||
		ch == '=' || ch == '<' || ch == '>' || ch == '!' || ch == '?' ||
		ch == '&' || ch == '|' || ch == '%' || ch == '$' || ch == '#'
}

// Tokenize は入力文字列をトークン列に変換します。
func (l *Lexer) Tokenize() ([]Token, error) {
	if !utf8.ValidString(l.input) {
		return nil, doggoerr.LexerError("input is not valid UTF-8", 1, 1)
	}
	// Validate the whole source, including comments and literal bodies, so a
	// sentinel byte cannot hide the remaining forms. Escaped NUL values are
	// decoded later and are unaffected by this source-byte check.
	if offset := strings.IndexByte(l.input, 0); offset >= 0 {
		prefix := l.input[:offset]
		line := strings.Count(prefix, "\n") + 1
		lineStart := strings.LastIndexByte(prefix, '\n') + 1
		column := utf8.RuneCountInString(prefix[lineStart:]) + 1
		return nil, doggoerr.LexerError("NUL byte in source", line, column)
	}
	var tokens []Token
	for {
		tok := l.NextToken()

		// Lexerにエラーが設定されている場合（未閉じ文字列など）
		// l.err は既に DoggoError なのでそのまま返す
		if l.err != nil {
			return nil, l.err
		}

		// ILLEGALトークンを検出した場合、エラーを返す
		if tok.Type == ILLEGAL {
			return nil, doggoerr.LexerErrorf(tok.Line, tok.Column, "illegal character '%s'", tok.Literal)
		}

		tokens = append(tokens, tok)
		if tok.Type == EOF {
			break
		}
	}
	return tokens, nil
}
