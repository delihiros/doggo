package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"doggo/internal/project"
	"golang.org/x/mod/modfile"
)

// packageLoadDir keeps Go's lexical workspace module paths consistent with its
// working directory. Only the verified Darwin system alias may change spelling;
// retained filesystem operations continue to use the canonical module root.
func packageLoadDir(moduleRoot string, env []string) (string, error) {
	if runtime.GOOS != "darwin" || (!strings.HasPrefix(moduleRoot, "/var/") && !strings.HasPrefix(moduleRoot, "/private/var/")) {
		return moduleRoot, nil
	}
	workFile, err := goWorkspaceFile(moduleRoot, env)
	if err != nil || workFile == "" {
		return moduleRoot, err
	}
	contents, err := os.ReadFile(workFile)
	if err != nil {
		return "", err
	}
	return selectWorkspaceModuleDir(moduleRoot, workFile, contents, project.NormalizeSystemPath)
}

func selectWorkspaceModuleDir(moduleRoot, workFile string, contents []byte, normalize func(string) (string, error)) (string, error) {
	canonicalRoot, err := normalize(moduleRoot)
	if err != nil {
		return "", err
	}
	workspace, err := modfile.ParseWork(workFile, contents, nil)
	if err != nil {
		return "", err
	}
	for _, use := range workspace.Use {
		dir := use.Path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(workFile), dir)
		}
		canonical, err := normalize(dir)
		if err != nil {
			return "", err
		}
		if canonical == canonicalRoot {
			return dir, nil
		}
	}
	return moduleRoot, nil
}

func goWorkspaceFile(moduleRoot string, env []string) (string, error) {
	// Ask Go so process settings, GOENV defaults, auto discovery and off retain
	// their normal precedence. Neither GOWORK nor the user's file is rewritten.
	cmd := exec.Command("go", "env", "GOWORK")
	cmd.Dir = moduleRoot
	cmd.Env = append(append([]string(nil), env...), "PWD="+moduleRoot)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("resolve Go workspace: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("resolve Go workspace: %w", err)
	}
	workFile := strings.TrimSpace(string(output))
	if workFile == "off" {
		return "", nil
	}
	return workFile, nil
}
