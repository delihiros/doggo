package cli

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	toolingformat "doggo/internal/tooling/format"
	toolinglint "doggo/internal/tooling/lint"
)

type fileDiagnostic struct {
	File    string
	Line    int
	Column  int
	Message string
}

func (d fileDiagnostic) String() string {
	line := d.Line
	if line <= 0 {
		line = 1
	}
	column := d.Column
	if column <= 0 {
		column = 1
	}
	return fmt.Sprintf("%s:%d:%d: %s", d.File, line, column, d.Message)
}

func (app *App) runFmtCommand() error {
	opts, err := parseFmtCommandArgs(app.args[1:])
	if err != nil {
		return err
	}
	files, err := collectDogFiles(opts.Paths, dogFileSearchOptions{
		ExcludeDirs: map[string]struct{}{
			".doggo": {},
			".git":   {},
		},
	})
	if err != nil {
		return err
	}

	base := "."
	issues := make([]fileDiagnostic, 0)
	formatted := make([]string, 0, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		displayPath := toDisplayPath(base, file)
		original := string(content)
		fixed := formatSource(original)
		fileIssues := checkFormatSource(displayPath, original)
		issues = append(issues, fileIssues...)

		if opts.Check {
			continue
		}
		if fixed == original {
			continue
		}
		if err := os.WriteFile(file, []byte(fixed), 0644); err != nil {
			return err
		}
		formatted = append(formatted, displayPath)
	}

	if opts.Check {
		for _, issue := range issues {
			fmt.Println(issue.String())
		}
		if len(issues) > 0 {
			return fmt.Errorf("format check failed (%d issue(s))", len(issues))
		}
		return nil
	}

	for _, file := range formatted {
		fmt.Printf("formatted %s\n", file)
	}
	return nil
}

func (app *App) runLintCommand() error {
	paths, err := parsePathOnlyCommandArgs(app.args[1:], "lint")
	if err != nil {
		return err
	}
	files, err := collectDogFiles(paths, dogFileSearchOptions{
		ExcludeDirs: map[string]struct{}{
			".doggo": {},
			".git":   {},
		},
	})
	if err != nil {
		return err
	}

	base := "."
	issues := make([]fileDiagnostic, 0)
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		displayPath := toDisplayPath(base, file)
		fileIssues := lintSource(displayPath, string(content))
		issues = append(issues, fileIssues...)
	}

	for _, issue := range issues {
		fmt.Println(issue.String())
	}
	if len(issues) > 0 {
		return fmt.Errorf("lint failed (%d issue(s))", len(issues))
	}
	return nil
}

func parsePathOnlyCommandArgs(args []string, command string) ([]string, error) {
	paths := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return nil, fmt.Errorf("unknown %s option: %s", command, arg)
		}
		paths = append(paths, arg)
	}
	return normalizeStringList(paths), nil
}

type fmtCommandOptions struct {
	Check bool
	Paths []string
}

func parseFmtCommandArgs(args []string) (fmtCommandOptions, error) {
	opts := fmtCommandOptions{
		Check: false,
		Paths: []string{},
	}
	for _, arg := range args {
		switch arg {
		case "--check":
			opts.Check = true
		default:
			if strings.HasPrefix(arg, "-") {
				return fmtCommandOptions{}, fmt.Errorf("unknown fmt option: %s", arg)
			}
			opts.Paths = append(opts.Paths, arg)
		}
	}
	opts.Paths = normalizeStringList(opts.Paths)
	return opts, nil
}

func checkFormatSource(filePath, content string) []fileDiagnostic {
	formatted := formatSource(content)
	if formatted == content {
		return nil
	}
	line, col := firstDiffLineCol(content, formatted)
	return []fileDiagnostic{{
		File:    filePath,
		Line:    line,
		Column:  col,
		Message: "source is not formatted",
	}}
}

func formatSource(content string) string {
	formatted, _, err := toolingformat.Format(content, toolingformat.Options{
		TabSize:      2,
		InsertSpaces: true,
	})
	if err != nil {
		return content
	}
	return formatted
}

func lintSource(filePath, content string) []fileDiagnostic {
	issues := toolinglint.AnalyzeWithOptions(filePath, content, toolinglint.AnalyzeOptions{})
	out := make([]fileDiagnostic, 0, len(issues))
	for _, issue := range issues {
		out = append(out, fileDiagnostic{
			File:    issue.File,
			Line:    issue.Line,
			Column:  issue.Column,
			Message: issue.Message,
		})
	}
	return out
}

func firstDiffLineCol(a, b string) (int, int) {
	line, col := 1, 1
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		ra, sa := utf8.DecodeRuneInString(a[ia:])
		rb, sb := utf8.DecodeRuneInString(b[ib:])
		if ra != rb {
			return line, col
		}
		if ra == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		ia += sa
		ib += sb
	}
	if ia < len(a) || ib < len(b) {
		return line, col
	}
	return 1, 1
}
