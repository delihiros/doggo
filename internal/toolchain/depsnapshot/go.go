package depsnapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"doggo/internal/depsource"
	"doggo/internal/project"
	"golang.org/x/mod/module"
)

type goModuleDownload struct {
	Path, Version, Dir, Error string
}

var runGoModuleDownload = func(query string) (_ []byte, _ string, retErr error) {
	root, err := project.NewOwnedTempRoot("doggo-go-download-")
	if err != nil {
		return nil, "", err
	}
	defer func() {
		retErr = errors.Join(retErr, root.CaptureSubprocessOutput(), root.Close())
	}()
	if err := root.Tree().WriteFile("go.mod", []byte("module doggo.invalid/dependency-resolution\n\ngo 1.25.0\n"), 0644); err != nil {
		return nil, "", err
	}

	cmd := exec.Command("go", "mod", "download", "-json", query)
	cmd.Dir = root.Path()
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	retErr = cmd.Run()
	return stdout.Bytes(), strings.TrimSpace(stderr.String()), retErr
}

func resolveGoDependency(parsed depsource.Source) (string, string, error) {
	stdout, diagnostic, commandErr := runGoModuleDownload(parsed.ModulePath + "@" + parsed.GoQuery())
	fail := func(err error) (string, string, error) {
		if diagnostic != "" {
			err = errors.Join(err, errors.New(diagnostic))
		}
		return "", "", fmt.Errorf("resolve Go dependency %q: %w", parsed.Declaration, err)
	}
	if len(bytes.TrimSpace(stdout)) == 0 && commandErr != nil {
		return fail(commandErr)
	}

	var download goModuleDownload
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	if err := decoder.Decode(&download); err != nil {
		decodeErr := fmt.Errorf("decode JSON: %w", err)
		if commandErr != nil {
			decodeErr = errors.Join(decodeErr, commandErr)
		}
		return fail(decodeErr)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		decodeErr := errors.New("download output must contain exactly one JSON object")
		if commandErr != nil {
			decodeErr = errors.Join(decodeErr, commandErr)
		}
		return fail(decodeErr)
	}
	if download.Error != "" {
		var reported error = errors.New(download.Error)
		if commandErr != nil {
			reported = errors.Join(reported, commandErr)
		}
		return fail(reported)
	}
	if commandErr != nil {
		return fail(commandErr)
	}
	if download.Path != parsed.ModulePath {
		return fail(fmt.Errorf("returned module path %q", download.Path))
	}
	if download.Version == "" {
		return fail(errors.New("returned empty version"))
	}
	if module.CanonicalVersion(download.Version) != download.Version || module.Check(download.Path, download.Version) != nil {
		return fail(fmt.Errorf("returned noncanonical version %q", download.Version))
	}
	if parsed.Query != "" && parsed.Query != "latest" && download.Version != parsed.Query {
		return fail(fmt.Errorf("requested version %q but Go returned %q", parsed.Query, download.Version))
	}
	if download.Dir == "" {
		return fail(errors.New("returned empty source directory"))
	}
	info, err := os.Stat(download.Dir)
	if err != nil {
		return fail(fmt.Errorf("source directory %q is unavailable: %w", download.Dir, err))
	}
	if !info.IsDir() {
		return fail(fmt.Errorf("source %q is not a directory", download.Dir))
	}
	return download.Dir, download.Version, nil
}
