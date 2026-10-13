package vm

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	doggoerr "doggo/internal/errors"
	"doggo/internal/syntax/frontend"

	"github.com/chzyer/readline"
)

var (
	replGetenv      = os.Getenv
	replUserHomeDir = os.UserHomeDir
	replTempDir     = os.TempDir
	replMkdirAll    = os.MkdirAll
	replNewReadline = readline.NewEx
)

// RunREPLWithRunner は指定されたRunnerで REPL を起動します。
func RunREPLWithRunner(runner *Runner) error {
	if runner == nil {
		var err error
		runner, err = NewRunner()
		if err != nil {
			return fmt.Errorf("initialize REPL: %w", err)
		}
	}

	fmt.Println("Doggo REPL - Bytecode Virtual Machine")
	fmt.Println("Type 'exit' or press Ctrl+C to quit")
	fmt.Println()

	historyPath := resolveREPLHistoryPath(os.Stderr)

	rl, err := replNewReadline(&readline.Config{
		Prompt:       "doggo> ",
		HistoryFile:  historyPath,
		AutoComplete: &replAutoCompleter{runner: runner},
	})
	if err != nil {
		return fmt.Errorf("initialize readline: %w", err)
	}
	defer rl.Close()

	RunREPLWithReadlineRunner(rl, os.Stdout, os.Stderr, runner)
	return nil
}

// RunREPLWithReadlineRunner は指定されたreadlineとRunnerでREPLループを実行します。
func RunREPLWithReadlineRunner(rl *readline.Instance, out io.Writer, errOut io.Writer, runner *Runner) {
	if runner == nil {
		var err error
		runner, err = NewRunner()
		if err != nil {
			fmt.Fprintf(errOut, "Error initializing REPL: %v\n", err)
			return
		}
	}

	if err := loadREPLNamespace(runner); err != nil {
		fmt.Fprintf(errOut, "Warning: failed to preload doggo.repl helpers: %v\n", err)
	}

	for {
		input, shouldContinue := readInput(rl, out, errOut)
		if !shouldContinue {
			break
		}
		if input == "" {
			continue
		}

		result, err := runner.Run(input)
		if err != nil {
			fmt.Fprintf(errOut, "Error: %v\n", err)
			continue
		}

		if result != nil {
			fmt.Fprintln(out, result.String())
		}
	}
}

// readInput は入力が完全な式になるまで複数行入力を読み込みます。
// Lexer/Parser ベースの判定で、コメントや文字列内の括弧も正しく扱います。
func readInput(rl *readline.Instance, out io.Writer, errOut io.Writer) (string, bool) {
	var inputBuffer strings.Builder

	rl.SetPrompt("doggo> ")
	line, err := rl.Readline()
	if err != nil {
		handleEOF(err, out, errOut)
		return "", false
	}

	trimmedLine := strings.TrimSpace(line)
	if trimmedLine == "" {
		return "", true
	}

	if trimmedLine == "exit" {
		fmt.Fprintln(out, "Goodbye!")
		return "", false
	}

	inputBuffer.WriteString(line)

	// Lexer/Parser ベースで継続判定
	for {
		needsMore, parseErr := replNeedsMoreInput(inputBuffer.String())
		if !needsMore {
			// 完全な式か、確定エラー
			// エラーがあっても一旦入力を返し、Runner.Run でエラーを出す
			break
		}

		// まだ入力が必要
		rl.SetPrompt("doggo...> ")
		line, err := rl.Readline()
		if err != nil {
			handleEOF(err, out, errOut)
			return "", false
		}

		inputBuffer.WriteString("\n")
		inputBuffer.WriteString(line)

		// 空入力が続いた場合のタイムアウト的な処理は行わない
		// ユーザーは Ctrl+C で抜けられる
		_ = parseErr // parseErr は継続判定にのみ使用
	}

	return inputBuffer.String(), true
}

func handleEOF(err error, out io.Writer, errOut io.Writer) {
	if err == readline.ErrInterrupt {
		return
	} else if err == io.EOF {
		fmt.Fprintln(out, "\nGoodbye!")
		return
	}
	fmt.Fprintf(errOut, "Error reading input: %v\n", err)
}

// replNeedsMoreInput は入力がまだ完全でないかを判定します。
// Lexer/Parser を使用して、コメントや文字列内の括弧も正しく扱います。
//
// 戻り値:
//   - needsMore: true なら継続入力が必要、false なら入力確定（正常完了またはエラー）
//   - err: 確定エラー（needsMore=false の場合のみ意味を持つ）
func replNeedsMoreInput(input string) (needsMore bool, err error) {
	// 空入力は完了扱い
	if strings.TrimSpace(input) == "" {
		return false, nil
	}

	if _, err := frontend.ReadForms("<repl>", input); err != nil {
		if isIncompleteInputError(err) {
			return true, nil
		}
		return false, err
	}

	return false, nil
}

// isIncompleteInputError はエラーが「入力がまだ完全でない」ことを示すかを判定します。
// 未閉じの文字列/正規表現/括弧などを検出します。
func isIncompleteInputError(err error) bool {
	if err == nil {
		return false
	}

	doggoErr, ok := doggoerr.AsDoggoError(err)
	if !ok {
		return false
	}

	msg := doggoErr.Message

	// Lexer の未閉じエラー
	if strings.Contains(msg, "unclosed string") ||
		strings.Contains(msg, "unclosed regex") {
		return true
	}

	// Parser の未閉じエラー（unexpected EOF）
	if strings.HasPrefix(msg, "unexpected EOF") {
		return true
	}

	return false
}

func resolveREPLHistoryPath(errOut io.Writer) string {
	const historyEnv = "DOGGO_REPL_HISTORY"

	candidates := make([]string, 0, 3)
	if envPath := strings.TrimSpace(replGetenv(historyEnv)); envPath != "" {
		candidates = append(candidates, envPath)
	}

	homeDir, homeErr := replUserHomeDir()
	if homeErr == nil && strings.TrimSpace(homeDir) != "" {
		candidates = append(candidates, filepath.Join(homeDir, ".doggo", "repl_history"))
	} else if errOut != nil {
		fmt.Fprintf(errOut, "Warning: failed to resolve home directory for REPL history: %v\n", homeErr)
	}

	tempPath := filepath.Join(replTempDir(), "doggo-repl-history")
	candidates = append(candidates, tempPath)

	for _, candidate := range candidates {
		if err := createHistoryParentDirectory(candidate); err == nil {
			return candidate
		} else if errOut != nil {
			fmt.Fprintf(errOut, "Warning: failed to initialize REPL history path %s: %v\n", candidate, err)
		}
	}

	return tempPath
}

func createHistoryParentDirectory(historyPath string) error {
	dir := filepath.Dir(historyPath)
	if dir == "" || dir == "." {
		return nil
	}
	return replMkdirAll(dir, 0o755)
}

func loadREPLNamespace(runner *Runner) error {
	if runner == nil {
		return fmt.Errorf("runner is nil")
	}
	_, err := runner.Run(`(require '[doggo.repl :refer [doc source]])`)
	return err
}

type replAutoCompleter struct {
	runner *Runner
}

func (c *replAutoCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if c == nil || c.runner == nil {
		return nil, 0
	}

	prefix := replCompletionPrefix(line, pos)
	if prefix == "" {
		return nil, 0
	}

	candidates := c.runner.CompletionCandidates(prefix)
	if len(candidates) == 0 {
		return nil, 0
	}

	prefixRunes := []rune(prefix)
	prefixLen := len(prefixRunes)
	suggestions := make([][]rune, 0, len(candidates))
	for _, candidate := range candidates {
		candidateRunes := []rune(candidate)
		if len(candidateRunes) < prefixLen || !strings.HasPrefix(candidate, prefix) {
			continue
		}
		suffix := string(candidateRunes[prefixLen:])
		suggestions = append(suggestions, []rune(suffix))
	}

	return suggestions, prefixLen
}

func replCompletionPrefix(line []rune, pos int) string {
	if pos < 0 {
		return ""
	}
	if pos > len(line) {
		pos = len(line)
	}

	start := pos
	for start > 0 {
		if isCompletionDelimiter(line[start-1]) {
			break
		}
		start--
	}

	return string(line[start:pos])
}

func isCompletionDelimiter(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '(', ')', '[', ']', '{', '}', '"', '\'', '`', ',', ';':
		return true
	default:
		return false
	}
}
