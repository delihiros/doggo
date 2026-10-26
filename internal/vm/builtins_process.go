package vm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

const maxProcessTimeoutMs = int64((1<<63 - 1) / int64(time.Millisecond))

type blockingDeref interface {
	Deref() types.Value
}

type contextDeref interface {
	DerefContext(context.Context) (types.Value, error)
}

type processExitRef struct {
	done  chan struct{}
	once  sync.Once
	value types.Value
}

func newProcessExitRef() *processExitRef {
	return &processExitRef{done: make(chan struct{})}
}

func (r *processExitRef) fulfill(value types.Value) {
	if value == nil {
		value = atom.NilValue
	}
	r.once.Do(func() {
		r.value = value
		close(r.done)
	})
}

func (r *processExitRef) Deref() types.Value {
	<-r.done
	return r.valueOrNil()
}

func (r *processExitRef) DerefContext(ctx context.Context) (types.Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return r.valueOrNil(), nil
	}
}

func (r *processExitRef) valueOrNil() types.Value {
	if r.value == nil {
		return atom.NilValue
	}
	return r.value
}

func (r *processExitRef) valueIfRealized() (types.Value, bool) {
	select {
	case <-r.done:
		return r.valueOrNil(), true
	default:
		return atom.NilValue, false
	}
}

func (r *processExitRef) waitFor(timeout time.Duration) (types.Value, bool) {
	if timeout <= 0 {
		return r.valueIfRealized()
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-r.done:
		return r.valueOrNil(), true
	case <-timer.C:
		return atom.NilValue, false
	}
}

func (r *processExitRef) realized() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

type processCapturedOutput struct {
	done     chan struct{}
	mu       sync.Mutex
	data     []byte
	err      error
	consumed bool
}

func newProcessCapturedOutput(reader io.ReadCloser) *processCapturedOutput {
	capture := &processCapturedOutput{done: make(chan struct{})}
	go func() {
		data, err := io.ReadAll(reader)
		if closeErr := reader.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		capture.data = data
		capture.err = err
		close(capture.done)
	}()
	return capture
}

func (c *processCapturedOutput) readAll() ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("stream is not piped")
	}
	<-c.done
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consumed {
		return nil, fmt.Errorf("stream closed")
	}
	c.consumed = true
	if c.err != nil {
		return nil, c.err
	}
	data := c.data
	c.data = nil
	return data, nil
}

type processHandle struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdinMu        sync.Mutex
	stdout         *processCapturedOutput
	stderr         *processCapturedOutput
	exitRef        *processExitRef
	closeAfterWait []io.Closer
	runCtx         context.Context
	commandCancel  context.CancelFunc
	cancel         context.CancelFunc
	processTree    processTreeController
	killTreeErr    error
}

func (h *processHandle) alive() bool {
	return h != nil && h.exitRef != nil && !h.exitRef.realized()
}

func (h *processHandle) killTree() (types.Value, error) {
	if h == nil || h.exitRef == nil {
		return atom.NilValue, nil
	}
	if !h.alive() {
		return h.exitRef.Deref(), nil
	}
	if h.killTreeErr != nil {
		return nil, h.killTreeErr
	}

	var err error
	switch {
	case h.cmd != nil && h.cmd.Cancel != nil:
		err = h.cmd.Cancel()
	case h.commandCancel != nil:
		h.commandCancel()
	case h.cmd != nil && h.cmd.Process != nil:
		err = h.cmd.Process.Kill()
	}
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return nil, err
	}
	return h.exitRef.Deref(), nil
}

func (h *processHandle) wait() {
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- h.cmd.Wait()
	}()

	var (
		waitErr    error
		contextErr error
	)
	if h.runCtx != nil && h.runCtx.Done() != nil {
		select {
		case waitErr = <-waitResult:
		case <-h.runCtx.Done():
			waitCompleted := false
			select {
			case waitErr = <-waitResult:
				waitCompleted = true
			default:
			}
			if waitCompleted {
				break
			}
			if processExitObserved(h.cmd) {
				waitErr = <-waitResult
				break
			}
			contextErr = h.runCtx.Err()
			if h.commandCancel != nil {
				h.commandCancel()
			} else if h.cmd.Cancel != nil {
				_ = h.cmd.Cancel()
			} else if h.cmd.Process != nil {
				_ = h.cmd.Process.Kill()
			}
			waitErr = <-waitResult
		}
	} else {
		waitErr = <-waitResult
	}

	_ = h.closeStdin()
	closeProcessClosers(h.closeAfterWait)
	if h.processTree != nil {
		_ = h.processTree.close()
	}

	exitCode := int64(0)
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = int64(exitErr.ExitCode())
		} else {
			exitCode = -1
		}
	}
	if errors.Is(contextErr, context.DeadlineExceeded) {
		exitCode = 124
	} else if errors.Is(contextErr, context.Canceled) {
		exitCode = -1
	}
	if h.cancel != nil {
		h.cancel()
	}
	if h.commandCancel != nil {
		h.commandCancel()
	}
	h.exitRef.fulfill(atom.NewNumberInt(exitCode))
}

func (h *processHandle) writeStdin(data []byte) error {
	h.stdinMu.Lock()
	defer h.stdinMu.Unlock()
	if h.stdin == nil {
		return fmt.Errorf("stdin is not piped")
	}
	_, err := h.stdin.Write(data)
	return err
}

func (h *processHandle) writeAndCloseStdin(data []byte) error {
	h.stdinMu.Lock()
	defer h.stdinMu.Unlock()
	if h.stdin == nil {
		return fmt.Errorf("stdin is not piped")
	}
	writeErr := error(nil)
	if len(data) > 0 {
		_, writeErr = h.stdin.Write(data)
	}
	closeErr := h.stdin.Close()
	h.stdin = nil
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (h *processHandle) closeStdin() error {
	h.stdinMu.Lock()
	defer h.stdinMu.Unlock()
	if h.stdin == nil {
		return nil
	}
	err := h.stdin.Close()
	h.stdin = nil
	return err
}

type processRedirectMode int

const (
	processRedirectPipe processRedirectMode = iota
	processRedirectInherit
	processRedirectDiscard
	processRedirectStdout
	processRedirectFile
	processRedirectContent
	processRedirectClosed
)

type processRedirect struct {
	mode   processRedirectMode
	path   string
	append bool
	data   []byte
}

type processStartOptions struct {
	dir       string
	clearEnv  bool
	env       map[string]string
	timeoutMs int64
	in        processRedirect
	out       processRedirect
	err       processRedirect
}

type processIOSetup struct {
	stdin                io.WriteCloser
	stdout               io.ReadCloser
	stderr               io.ReadCloser
	initialInput         []byte
	closeStdinAfterStart bool
	closeAfterStart      []io.Closer
	closeAfterWait       []io.Closer
}

func processNamedEnvKey(name string) string {
	if idx := strings.LastIndexByte(name, '/'); idx > 0 {
		return name[idx+1:]
	}
	return name
}

func processEnvValueString(value types.Value) string {
	switch v := value.(type) {
	case atom.String:
		return string(v)
	case types.Nil:
		return ""
	case types.Displayable:
		return v.Display()
	default:
		return value.String()
	}
}

func processEnvKeyStringWithContext(fnName string, value types.Value) (string, types.Value) {
	switch v := value.(type) {
	case atom.String:
		return string(v), nil
	case atom.Keyword:
		return processNamedEnvKey(string(v)), nil
	default:
		if name, ok := types.SymbolName(value); ok {
			return processNamedEnvKey(name), nil
		}
		return "", newArgumentError("%s: env keys must be strings, symbols, or keywords, got %s", fnName, value.Type())
	}
}

func processStringArg(fnName string, arg types.Value) (string, types.Value) {
	str, ok := arg.(atom.String)
	if !ok {
		return "", newArgumentError("%s: expected string argument, got %s", fnName, arg.Type())
	}
	return string(str), nil
}

func processOptionString(options types.Map, key string) (string, types.Value) {
	return processOptionStringWithContext("process-run*", options, key)
}

func processOptionStringWithContext(fnName string, options types.Map, key string) (string, types.Value) {
	value := options.Get(atom.NewKeyword(key))
	if value == nil || value == atom.NilValue {
		return "", nil
	}
	str, ok := value.(atom.String)
	if !ok {
		return "", newArgumentError("%s: option %s must be a string, got %s", fnName, key, value.Type())
	}
	return string(str), nil
}

func processOptionEnv(options types.Map) (map[string]string, types.Value) {
	return processOptionEnvWithContext("process-run*", options)
}

func processOptionEnvWithContext(fnName string, options types.Map) (map[string]string, types.Value) {
	value := options.Get(atom.NewKeyword("env"))
	if value == nil || value == atom.NilValue {
		return nil, nil
	}

	envMap, ok := value.(types.Map)
	if !ok {
		return nil, newArgumentError("%s: option env must be a map, got %s", fnName, value.Type())
	}

	result := make(map[string]string)
	for _, key := range sequableToSlice(envMap.Keys()) {
		keyString, err := processEnvKeyStringWithContext(fnName, key)
		if err != nil {
			return nil, err
		}
		rawValue := envMap.Get(key)
		result[keyString] = processEnvValueString(rawValue)
	}
	return result, nil
}

func processOptionBoolWithContext(fnName string, options types.Map, key string) (bool, types.Value) {
	value := options.Get(atom.NewKeyword(key))
	if value == nil || value == atom.NilValue {
		return false, nil
	}
	booleanValue, ok := value.(atom.Boolean)
	if !ok {
		return false, newArgumentError("%s: option %s must be boolean, got %s", fnName, key, value.Type())
	}
	return bool(booleanValue), nil
}

func processOptionTimeoutMs(options types.Map) (int64, types.Value) {
	return processOptionTimeoutMsWithContext("process-run*", options)
}

func processOptionTimeoutMsWithContext(fnName string, options types.Map) (int64, types.Value) {
	value := options.Get(atom.NewKeyword("timeout"))
	if value == nil || value == atom.NilValue {
		return 0, nil
	}
	return processTimeoutMsValue(fnName, value)
}

func processTimeoutMsValue(fnName string, value types.Value) (int64, types.Value) {
	if number, ok := value.(types.Number); ok {
		return processTimeoutMsNumber(fnName, number)
	}

	durationMap, ok := value.(types.Map)
	if !ok {
		return 0, newArgumentError("%s: option timeout must be a number or duration map, got %s", fnName, value.Type())
	}

	millis := durationMap.Get(atom.NewKeyword("millis"))
	number, ok := millis.(types.Number)
	if !ok {
		return 0, newArgumentError("%s: option timeout must be a number or duration map, got %s", fnName, value.Type())
	}

	return processTimeoutMsNumber(fnName, number)
}

func processTimeoutMsNumber(fnName string, number types.Number) (int64, types.Value) {
	raw := number.ToFloat64()
	if raw < 0 {
		return 0, newArgumentError("%s: option timeout must be non-negative", fnName)
	}
	if math.IsNaN(raw) || math.IsInf(raw, 1) {
		return 0, newArgumentError("%s: option timeout must be finite", fnName)
	}
	if raw > float64(maxProcessTimeoutMs) {
		return 0, newArgumentError("%s: option timeout must be no greater than %d ms", fnName, maxProcessTimeoutMs)
	}
	return int64(math.Ceil(raw)), nil
}

func mergeProcessEnv(overrides map[string]string) []string {
	base := make(map[string]string)
	for _, entry := range os.Environ() {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				base[entry[:i]] = entry[i+1:]
				break
			}
		}
	}
	for key, value := range overrides {
		base[key] = value
	}

	result := make([]string, 0, len(base))
	for key, value := range base {
		result = append(result, key+"="+value)
	}
	return result
}

func buildProcessEnv(clearEnv bool, overrides map[string]string) []string {
	if !clearEnv {
		if overrides == nil {
			return nil
		}
		return mergeProcessEnv(overrides)
	}

	result := make([]string, 0, len(overrides))
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func closeProcessClosers(closers []io.Closer) {
	for _, closer := range closers {
		if closer != nil {
			_ = closer.Close()
		}
	}
}

func processCleanupIOSetup(setup *processIOSetup) {
	if setup == nil {
		return
	}
	if setup.stdin != nil {
		_ = setup.stdin.Close()
		setup.stdin = nil
	}
	if setup.stdout != nil {
		_ = setup.stdout.Close()
		setup.stdout = nil
	}
	if setup.stderr != nil {
		_ = setup.stderr.Close()
		setup.stderr = nil
	}
	closeProcessClosers(setup.closeAfterStart)
	setup.closeAfterStart = nil
	closeProcessClosers(setup.closeAfterWait)
	setup.closeAfterWait = nil
}

func parseProcessStartArgs(args []types.Value) ([]string, types.Map, types.Value) {
	if len(args) == 0 {
		return nil, nil, newArgumentError("process-start*: wrong number of arguments. got=0, want at least 1")
	}

	startIndex := 0
	var options types.Map
	if optionMap, ok := args[0].(types.Map); ok {
		options = optionMap
		startIndex = 1
	}

	if startIndex >= len(args) {
		return nil, nil, newArgumentError("process-start*: expected at least one command string")
	}

	command := make([]string, len(args)-startIndex)
	for i := startIndex; i < len(args); i++ {
		str, errValue := processStringArg("process-start*", args[i])
		if errValue != nil {
			return nil, nil, errValue
		}
		command[i-startIndex] = str
	}

	return command, options, nil
}

func processParseFileRedirect(fnName, key string, value types.Value, allowedTypes ...string) (processRedirect, types.Value) {
	switch v := value.(type) {
	case atom.String:
		return processRedirect{mode: processRedirectFile, path: string(v)}, nil
	case types.Map:
		pathValue := v.Get(atom.NewKeyword("path"))
		pathString, ok := pathValue.(atom.String)
		if !ok {
			return processRedirect{}, newArgumentError("%s: option %s redirect must include string :path", fnName, key)
		}

		typeValue := v.Get(atom.NewKeyword("type"))
		if typeValue != nil && typeValue != atom.NilValue {
			typeKeyword, ok := typeValue.(atom.Keyword)
			if !ok {
				return processRedirect{}, newArgumentError("%s: option %s redirect :type must be keyword, got %s", fnName, key, typeValue.Type())
			}
			typeName := string(typeKeyword)
			allowed := false
			for _, candidate := range allowedTypes {
				if typeName == candidate {
					allowed = true
					break
				}
			}
			if !allowed {
				return processRedirect{}, newArgumentError("%s: option %s redirect must be one of %s, got :%s", fnName, key, strings.Join(allowedTypes, ", "), typeName)
			}
		}

		appendMode := false
		appendValue := v.Get(atom.NewKeyword("append"))
		if appendValue != nil && appendValue != atom.NilValue {
			booleanValue, ok := appendValue.(atom.Boolean)
			if !ok {
				return processRedirect{}, newArgumentError("%s: option %s redirect :append must be boolean, got %s", fnName, key, appendValue.Type())
			}
			appendMode = bool(booleanValue)
		}

		return processRedirect{mode: processRedirectFile, path: string(pathString), append: appendMode}, nil
	default:
		return processRedirect{}, newArgumentError("%s: option %s must be a redirect keyword, path string, or doggo.io descriptor, got %s", fnName, key, value.Type())
	}
}

func processOptionInputRedirect(fnName string, options types.Map, key string) (processRedirect, types.Value) {
	value := options.Get(atom.NewKeyword(key))
	if value == nil || value == atom.NilValue {
		return processRedirect{mode: processRedirectPipe}, nil
	}

	switch v := value.(type) {
	case atom.String:
		return processRedirect{mode: processRedirectContent, data: []byte(string(v))}, nil
	case *coll.Vector:
		data, err := vectorToBytes(v)
		if err != nil {
			return processRedirect{}, newArgumentError("%s: option %s binary input must be a vector of bytes: %v", fnName, key, err)
		}
		return processRedirect{mode: processRedirectContent, data: data}, nil
	}

	if keywordValue, ok := value.(atom.Keyword); ok {
		switch string(keywordValue) {
		case "pipe":
			return processRedirect{mode: processRedirectPipe}, nil
		case "inherit":
			return processRedirect{mode: processRedirectInherit}, nil
		case "closed":
			return processRedirect{mode: processRedirectClosed}, nil
		default:
			return processRedirect{}, newArgumentError("%s: option %s must be :pipe, :inherit, :closed, input content, or input redirect", fnName, key)
		}
	}

	return processParseFileRedirect(fnName, key, value, "reader-spec", "input-stream-spec")
}

func processOptionOutputRedirect(fnName string, options types.Map, key string, allowStdout bool) (processRedirect, types.Value) {
	value := options.Get(atom.NewKeyword(key))
	if value == nil || value == atom.NilValue {
		return processRedirect{mode: processRedirectPipe}, nil
	}

	if keywordValue, ok := value.(atom.Keyword); ok {
		switch string(keywordValue) {
		case "pipe":
			return processRedirect{mode: processRedirectPipe}, nil
		case "inherit":
			return processRedirect{mode: processRedirectInherit}, nil
		case "discard":
			return processRedirect{mode: processRedirectDiscard}, nil
		case "stdout":
			if allowStdout {
				return processRedirect{mode: processRedirectStdout}, nil
			}
		}
		if allowStdout {
			return processRedirect{}, newArgumentError("%s: option %s must be :pipe, :inherit, :discard, :stdout, or output redirect", fnName, key)
		}
		return processRedirect{}, newArgumentError("%s: option %s must be :pipe, :inherit, :discard, or output redirect", fnName, key)
	}

	return processParseFileRedirect(fnName, key, value, "writer-spec", "output-stream-spec")
}

func parseProcessStartOptions(options types.Map) (processStartOptions, types.Value) {
	startOptions := processStartOptions{
		in:  processRedirect{mode: processRedirectPipe},
		out: processRedirect{mode: processRedirectPipe},
		err: processRedirect{mode: processRedirectPipe},
	}

	if options == nil {
		return startOptions, nil
	}

	dir, errValue := processOptionStringWithContext("process-start*", options, "dir")
	if errValue != nil {
		return processStartOptions{}, errValue
	}
	startOptions.dir = dir

	env, errValue := processOptionEnvWithContext("process-start*", options)
	if errValue != nil {
		return processStartOptions{}, errValue
	}
	startOptions.env = env

	clearEnv, errValue := processOptionBoolWithContext("process-start*", options, "clear-env")
	if errValue != nil {
		return processStartOptions{}, errValue
	}
	startOptions.clearEnv = clearEnv

	timeoutMs, errValue := processOptionTimeoutMsWithContext("process-start*", options)
	if errValue != nil {
		return processStartOptions{}, errValue
	}
	startOptions.timeoutMs = timeoutMs

	inRedirect, errValue := processOptionInputRedirect("process-start*", options, "in")
	if errValue != nil {
		return processStartOptions{}, errValue
	}
	startOptions.in = inRedirect

	outRedirect, errValue := processOptionOutputRedirect("process-start*", options, "out", false)
	if errValue != nil {
		return processStartOptions{}, errValue
	}
	startOptions.out = outRedirect

	errRedirect, errValue := processOptionOutputRedirect("process-start*", options, "err", true)
	if errValue != nil {
		return processStartOptions{}, errValue
	}
	startOptions.err = errRedirect

	return startOptions, nil
}

func processOpenOutputFile(path string, appendMode bool) (*os.File, error) {
	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	return os.OpenFile(path, flags, 0644)
}

func processSetupOutput(cmd *exec.Cmd, redirect processRedirect, stdout bool, setup *processIOSetup) error {
	switch redirect.mode {
	case processRedirectPipe:
		reader, writer, err := os.Pipe()
		if err != nil {
			return err
		}
		setup.closeAfterStart = append(setup.closeAfterStart, writer)
		if stdout {
			setup.stdout = reader
			cmd.Stdout = writer
			return nil
		}
		setup.stderr = reader
		cmd.Stderr = writer
		return nil
	case processRedirectInherit:
		if stdout {
			cmd.Stdout = os.Stdout
		} else {
			cmd.Stderr = os.Stderr
		}
		return nil
	case processRedirectDiscard:
		if stdout {
			cmd.Stdout = io.Discard
		} else {
			cmd.Stderr = io.Discard
		}
		return nil
	case processRedirectFile:
		file, err := processOpenOutputFile(redirect.path, redirect.append)
		if err != nil {
			return err
		}
		setup.closeAfterWait = append(setup.closeAfterWait, file)
		if stdout {
			cmd.Stdout = file
		} else {
			cmd.Stderr = file
		}
		return nil
	default:
		return fmt.Errorf("unsupported process redirect mode %d", redirect.mode)
	}
}

func processSetupIO(cmd *exec.Cmd, options processStartOptions) (setup processIOSetup, retErr error) {
	defer func() {
		if retErr != nil {
			processCleanupIOSetup(&setup)
		}
	}()

	switch options.in.mode {
	case processRedirectPipe:
		reader, writer, err := os.Pipe()
		if err != nil {
			return setup, err
		}
		setup.stdin = writer
		setup.closeAfterStart = append(setup.closeAfterStart, reader)
		cmd.Stdin = reader
	case processRedirectContent:
		reader, writer, err := os.Pipe()
		if err != nil {
			return setup, err
		}
		setup.stdin = writer
		setup.closeAfterStart = append(setup.closeAfterStart, reader)
		cmd.Stdin = reader
		setup.initialInput = append([]byte{}, options.in.data...)
		setup.closeStdinAfterStart = true
	case processRedirectInherit:
		cmd.Stdin = os.Stdin
	case processRedirectClosed:
		// Leave stdin nil so the process sees EOF immediately, matching exec.Command defaults.
	case processRedirectFile:
		file, err := os.Open(options.in.path)
		if err != nil {
			return setup, err
		}
		setup.closeAfterWait = append(setup.closeAfterWait, file)
		cmd.Stdin = file
	default:
		return setup, fmt.Errorf("unsupported input redirect mode %d", options.in.mode)
	}

	if options.err.mode == processRedirectStdout {
		switch options.out.mode {
		case processRedirectPipe:
			reader, writer, err := os.Pipe()
			if err != nil {
				return setup, err
			}
			setup.stdout = reader
			setup.closeAfterStart = append(setup.closeAfterStart, writer)
			cmd.Stdout = writer
			cmd.Stderr = writer
		case processRedirectInherit:
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stdout
		case processRedirectDiscard:
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
		case processRedirectFile:
			file, err := processOpenOutputFile(options.out.path, options.out.append)
			if err != nil {
				return setup, err
			}
			setup.closeAfterWait = append(setup.closeAfterWait, file)
			cmd.Stdout = file
			cmd.Stderr = file
		default:
			return setup, fmt.Errorf("unsupported output redirect mode %d", options.out.mode)
		}
		return setup, nil
	}

	if err := processSetupOutput(cmd, options.out, true, &setup); err != nil {
		return setup, err
	}
	if err := processSetupOutput(cmd, options.err, false, &setup); err != nil {
		return setup, err
	}
	return setup, nil
}

func processCaptureOutputs(setup *processIOSetup) (*processCapturedOutput, *processCapturedOutput) {
	if setup == nil {
		return nil, nil
	}

	var stdout *processCapturedOutput
	if setup.stdout != nil {
		stdout = newProcessCapturedOutput(setup.stdout)
		setup.stdout = nil
	}

	var stderr *processCapturedOutput
	if setup.stderr != nil {
		stderr = newProcessCapturedOutput(setup.stderr)
		setup.stderr = nil
	}

	return stdout, stderr
}

func processStartHandle(ctx context.Context, command []string, options processStartOptions) (*processHandle, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	var cancel context.CancelFunc
	if err := runCtx.Err(); err != nil {
		return nil, err
	}

	control, err := processHandleController(runCtx, options.timeoutMs)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	processTree := control.controller
	commandCtx, commandCancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(commandCtx, command[0], command[1:]...)
	if options.dir != "" {
		cmd.Dir = options.dir
	}
	if env := buildProcessEnv(options.clearEnv, options.env); env != nil {
		cmd.Env = env
	}
	if err := processTree.configure(cmd); err != nil {
		commandCancel()
		_ = processTree.close()
		if cancel != nil {
			cancel()
		}
		return nil, err
	}

	setup, err := processSetupIO(cmd, options)
	if err != nil {
		commandCancel()
		_ = processTree.close()
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	if err := runCtx.Err(); err != nil {
		processCleanupIOSetup(&setup)
		commandCancel()
		_ = processTree.close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		processCleanupIOSetup(&setup)
		commandCancel()
		_ = processTree.close()
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	if err := processTree.afterStart(runCtx, cmd); err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		processCleanupIOSetup(&setup)
		commandCancel()
		_ = processTree.close()
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	// A start call must return a handle even when process creation or Windows
	// job attachment takes longer than its execution timeout. Parent context
	// cancellation still applies throughout setup and attachment.
	if options.timeoutMs > 0 {
		runCtx, cancel = context.WithTimeout(runCtx, time.Duration(options.timeoutMs)*time.Millisecond)
	}
	closeProcessClosers(setup.closeAfterStart)
	setup.closeAfterStart = nil

	stdout, stderr := processCaptureOutputs(&setup)

	stdin := setup.stdin
	if setup.closeStdinAfterStart {
		stdin = nil
		go func(writer io.WriteCloser, data []byte) {
			if len(data) > 0 {
				_, _ = writer.Write(data)
			}
			_ = writer.Close()
		}(setup.stdin, setup.initialInput)
	}

	handle := &processHandle{
		cmd:            cmd,
		stdin:          stdin,
		stdout:         stdout,
		stderr:         stderr,
		exitRef:        newProcessExitRef(),
		closeAfterWait: setup.closeAfterWait,
		runCtx:         runCtx,
		commandCancel:  commandCancel,
		cancel:         cancel,
		processTree:    processTree,
		killTreeErr:    control.killTreeUnsupportedErr,
	}
	go handle.wait()
	return handle, nil
}

func processHandleFromValue(fnName string, value types.Value) (*processHandle, types.Value) {
	goObject, ok := value.(*atom.GoObject)
	if !ok {
		return nil, newArgumentError("%s: expected process handle, got %s", fnName, value.Type())
	}
	handle, ok := goObject.Unwrap().(*processHandle)
	if !ok || handle == nil {
		return nil, newArgumentError("%s: expected process handle, got %s", fnName, value.Type())
	}
	return handle, nil
}

func processStreamDescriptor(kind atom.Keyword, handle *processHandle) types.Value {
	streamType := atom.NewKeyword("process-reader")
	if kind == atom.NewKeyword("stdin") {
		streamType = atom.NewKeyword("process-writer")
	}
	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("type"), streamType,
		atom.NewKeyword("stream"), kind,
		atom.NewKeyword("handle"), atom.NewGoObject(handle),
	})
}

func processDescriptorHandle(fnName string, descriptor types.Value, wantType string, allowedStreams ...string) (*processHandle, string, types.Value) {
	descriptorMap, ok := descriptor.(types.Map)
	if !ok {
		return nil, "", newArgumentError("%s: expected process stream descriptor, got %s", fnName, descriptor.Type())
	}

	typeValue := descriptorMap.Get(atom.NewKeyword("type"))
	typeKeyword, ok := typeValue.(atom.Keyword)
	if !ok || string(typeKeyword) != wantType {
		return nil, "", newArgumentError("%s: expected %s descriptor", fnName, wantType)
	}

	streamValue := descriptorMap.Get(atom.NewKeyword("stream"))
	streamKeyword, ok := streamValue.(atom.Keyword)
	if !ok {
		return nil, "", newArgumentError("%s: descriptor must include keyword :stream", fnName)
	}
	streamName := string(streamKeyword)
	allowed := false
	for _, candidate := range allowedStreams {
		if streamName == candidate {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, "", newArgumentError("%s: unsupported process stream :%s", fnName, streamName)
	}

	handle, errValue := processHandleFromValue(fnName, descriptorMap.Get(atom.NewKeyword("handle")))
	if errValue != nil {
		return nil, "", errValue
	}
	return handle, streamName, nil
}

func parseProcessArgs(args []types.Value) ([]string, types.Map, types.Value) {
	commandCount := 0
	for commandCount < len(args) {
		if _, ok := args[commandCount].(atom.String); !ok {
			break
		}
		commandCount++
	}

	if commandCount == 0 {
		_, errValue := processStringArg("process-run*", args[0])
		return nil, nil, errValue
	}

	command := make([]string, commandCount)
	for i := 0; i < commandCount; i++ {
		str, err := processStringArg("process-run*", args[i])
		if err != nil {
			return nil, nil, err
		}
		command[i] = str
	}

	if commandCount == len(args) {
		return command, nil, nil
	}

	remainder := args[commandCount:]
	if len(remainder) == 1 {
		if optionMap, ok := remainder[0].(types.Map); ok {
			return command, optionMap, nil
		}
	}

	if len(remainder)%2 != 0 {
		return nil, nil, newArgumentError("process-run*: options must be a map or keyword/value pairs")
	}

	optionPairs := make([]types.Value, 0, len(remainder))
	for i := 0; i < len(remainder); i += 2 {
		if _, ok := remainder[i].(atom.Keyword); !ok {
			return nil, nil, newArgumentError("process-run*: option keys must be keywords, got %s", remainder[i].Type())
		}
		optionPairs = append(optionPairs, remainder[i], remainder[i+1])
	}

	return command, coll.NewMapFromPairs(optionPairs), nil
}

func processValueError(function string, errValue types.Value) error {
	if errValue == nil {
		return nil
	}
	if err, ok := errValue.(*Error); ok {
		return invalidArgumentVMError(function, err.Message)
	}
	return invalidArgumentVMError(function, errValue.String())
}

func processRuntimeError(function, format string, args ...any) error {
	return runtimeVMError(function, fmt.Sprintf(format, args...))
}

var builtinsProcess = []BuiltinDefinition{
	{
		Name:       "process-start*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("process-start*", args, 1); err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, processRuntimeError("doggo.process/start", "process-start*: %v", err)
			}

			command, optionMap, errValue := parseProcessStartArgs(args)
			if errValue != nil {
				return nil, processValueError("doggo.process/start", errValue)
			}

			options, errValue := parseProcessStartOptions(optionMap)
			if errValue != nil {
				return nil, processValueError("doggo.process/start", errValue)
			}

			handle, err := processStartHandle(ctx, command, options)
			if err != nil {
				return nil, processRuntimeError("doggo.process/start", "process-start*: %v", err)
			}
			return atom.NewGoObject(handle), nil
		}},
	},
	{
		Name:       "process-stdout*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("process-stdout*", args, 1); err != nil {
				return err
			}
			handle, errValue := processHandleFromValue("process-stdout*", args[0])
			if errValue != nil {
				return errValue
			}
			return processStreamDescriptor(atom.NewKeyword("stdout"), handle)
		}},
	},
	{
		Name:       "process-stderr*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("process-stderr*", args, 1); err != nil {
				return err
			}
			handle, errValue := processHandleFromValue("process-stderr*", args[0])
			if errValue != nil {
				return errValue
			}
			return processStreamDescriptor(atom.NewKeyword("stderr"), handle)
		}},
	},
	{
		Name:       "process-stdin*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("process-stdin*", args, 1); err != nil {
				return err
			}
			handle, errValue := processHandleFromValue("process-stdin*", args[0])
			if errValue != nil {
				return errValue
			}
			return processStreamDescriptor(atom.NewKeyword("stdin"), handle)
		}},
	},
	{
		Name:       "process-stdin-close*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("process-stdin-close*", args, 1); err != nil {
				return nil, err
			}
			handle, errValue := processHandleFromValue("process-stdin-close*", args[0])
			if errValue != nil {
				return nil, processValueError("doggo.process/close-stdin", errValue)
			}
			if err := handle.closeStdin(); err != nil {
				return nil, processRuntimeError("doggo.process/close-stdin", "process-stdin-close*: %v", err)
			}
			return atom.NilValue, nil
		}},
	},
	{
		Name:       "process-stdin-write*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("process-stdin-write*", args, 2); err != nil {
				return nil, err
			}
			handle, errValue := processHandleFromValue("process-stdin-write*", args[0])
			if errValue != nil {
				return nil, processValueError("doggo.process/write-stdin", errValue)
			}

			var data []byte
			if vec, ok := args[1].(*coll.Vector); ok {
				bytesValue, err := vectorToBytes(vec)
				if err != nil {
					return nil, processRuntimeError("doggo.process/write-stdin", "process-stdin-write*: %v", err)
				}
				data = bytesValue
			} else {
				data = []byte(valueToString(args[1]))
			}

			if err := handle.writeStdin(data); err != nil {
				return nil, processRuntimeError("doggo.process/write-stdin", "process-stdin-write*: %v", err)
			}
			return atom.NilValue, nil
		}},
	},
	{
		Name:       "process-exit-ref*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("process-exit-ref*", args, 1); err != nil {
				return err
			}
			handle, errValue := processHandleFromValue("process-exit-ref*", args[0])
			if errValue != nil {
				return errValue
			}
			return atom.NewGoObject(handle.exitRef)
		}},
	},
	{
		Name:       "process-exited?*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("process-exited?*", args, 1); err != nil {
				return err
			}
			handle, errValue := processHandleFromValue("process-exited?*", args[0])
			if errValue != nil {
				return errValue
			}
			return atom.Boolean(handle.exitRef.realized())
		}},
	},
	{
		Name:       "process-exit-value*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("process-exit-value*", args, 1); err != nil {
				return err
			}
			handle, errValue := processHandleFromValue("process-exit-value*", args[0])
			if errValue != nil {
				return errValue
			}
			value, ok := handle.exitRef.valueIfRealized()
			if !ok {
				return atom.NilValue
			}
			return value
		}},
	},
	{
		Name:       "process-wait-for*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("process-wait-for*", args, 2); err != nil {
				return err
			}
			handle, errValue := processHandleFromValue("process-wait-for*", args[0])
			if errValue != nil {
				return errValue
			}
			timeoutMs, errValue := processTimeoutMsValue("process-wait-for*", args[1])
			if errValue != nil {
				return errValue
			}
			value, ok := handle.exitRef.waitFor(time.Duration(timeoutMs) * time.Millisecond)
			if !ok {
				return atom.NilValue
			}
			return value
		}},
	},
	{
		Name:       "process-tree-supported?*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("process-tree-supported?*", args, 0); err != nil {
				return nil, err
			}
			supported, err := processTreeSupported()
			if err != nil {
				return nil, processRuntimeError("doggo.process/process-tree-supported?", "process-tree-supported?*: %v", err)
			}
			return atom.Boolean(supported), nil
		}},
	},
	{
		Name:       "process-alive?*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("process-alive?*", args, 1); err != nil {
				return err
			}
			handle, errValue := processHandleFromValue("process-alive?*", args[0])
			if errValue != nil {
				return errValue
			}
			return atom.Boolean(handle.alive())
		}},
	},
	{
		Name:       "process-kill-tree*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("process-kill-tree*", args, 1); err != nil {
				return nil, err
			}
			handle, errValue := processHandleFromValue("process-kill-tree*", args[0])
			if errValue != nil {
				return nil, processValueError("doggo.process/kill-tree", errValue)
			}
			value, err := handle.killTree()
			if err != nil {
				return nil, processRuntimeError("doggo.process/kill-tree", "process-kill-tree*: %v", err)
			}
			return value, nil
		}},
	},
	{
		Name:       "process-stream-slurp*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("process-stream-slurp*", args, 2); err != nil {
				return nil, err
			}
			handle, streamName, errValue := processDescriptorHandle("process-stream-slurp*", args[0], "process-reader", "stdout", "stderr")
			if errValue != nil {
				return nil, processValueError("doggo.io/slurp", errValue)
			}
			binaryFlag, ok := args[1].(atom.Boolean)
			if !ok {
				return nil, processValueError("doggo.io/slurp", newArgumentError("process-stream-slurp*: binary flag must be boolean, got %s", args[1].Type()))
			}

			var capture *processCapturedOutput
			switch streamName {
			case "stdout":
				capture = handle.stdout
			case "stderr":
				capture = handle.stderr
			}
			if capture == nil {
				return nil, processRuntimeError("doggo.io/slurp", "process-stream-slurp*: %s is not piped", streamName)
			}

			data, err := capture.readAll()
			if err != nil {
				return nil, processRuntimeError("doggo.io/slurp", "process-stream-slurp*: %v", err)
			}
			if bool(binaryFlag) {
				return bytesToVector(data), nil
			}
			if !utf8.Valid(data) {
				return nil, processRuntimeError("doggo.io/slurp", "process-stream-slurp*: text output is not valid UTF-8")
			}
			return atom.NewString(string(data)), nil
		}},
	},
	{
		Name:       "process-stream-spit*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("process-stream-spit*", args, 3); err != nil {
				return nil, err
			}
			handle, _, errValue := processDescriptorHandle("process-stream-spit*", args[0], "process-writer", "stdin")
			if errValue != nil {
				return nil, processValueError("doggo.io/spit", errValue)
			}
			binaryFlag, ok := args[2].(atom.Boolean)
			if !ok {
				return nil, processValueError("doggo.io/spit", newArgumentError("process-stream-spit*: binary flag must be boolean, got %s", args[2].Type()))
			}

			var data []byte
			if bool(binaryFlag) {
				vec, ok := args[1].(*coll.Vector)
				if !ok {
					return nil, processValueError("doggo.io/spit", newArgumentError("process-stream-spit*: binary mode requires a vector, got %s", args[1].Type()))
				}
				bytesValue, err := vectorToBytes(vec)
				if err != nil {
					return nil, processRuntimeError("doggo.io/spit", "process-stream-spit*: %v", err)
				}
				data = bytesValue
			} else {
				data = []byte(valueToString(args[1]))
			}

			if err := handle.writeAndCloseStdin(data); err != nil {
				return nil, processRuntimeError("doggo.io/spit", "process-stream-spit*: %v", err)
			}
			return atom.NilValue, nil
		}},
	},
	{
		Name:       "process-run*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (result types.Value, err error) {
			if err := CheckArgMinCtx("process-run*", args, 1); err != nil {
				return nil, err
			}

			command, options, errValue := parseProcessArgs(args)
			if errValue != nil {
				return nil, processValueError("doggo.process/run", errValue)
			}

			runCtx := ctx
			timeoutMs := int64(0)
			if options != nil {
				timeoutValue, err := processOptionTimeoutMs(options)
				if err != nil {
					return nil, processValueError("doggo.process/run", err)
				}
				timeoutMs = timeoutValue
				if timeoutMs > 0 {
					var cancel context.CancelFunc
					runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
					defer cancel()
				}
			}

			if err := runCtx.Err(); err != nil {
				return nil, processRuntimeError("doggo.process/run", "process-run*: %v", err)
			}

			processTree := processTreeController(noopProcessTreeController{})
			if processNeedsTreeControl(runCtx, timeoutMs) {
				processTree, err = newProcessTreeController()
				if err != nil {
					return nil, processRuntimeError("doggo.process/run", "process-run*: %v", err)
				}
			}
			defer func() {
				closeErr := processTree.close()
				if err == nil && closeErr != nil {
					result = nil
					err = processRuntimeError("doggo.process/run", "process-run*: %v", closeErr)
				}
			}()

			cmd := exec.CommandContext(runCtx, command[0], command[1:]...)
			if options != nil {
				dir, err := processOptionString(options, "dir")
				if err != nil {
					return nil, processValueError("doggo.process/run", err)
				}
				if dir != "" {
					cmd.Dir = dir
				}

				stdin, err := processOptionString(options, "in")
				if err != nil {
					return nil, processValueError("doggo.process/run", err)
				}
				if stdin != "" {
					cmd.Stdin = bytes.NewBufferString(stdin)
				}

				env, err := processOptionEnv(options)
				if err != nil {
					return nil, processValueError("doggo.process/run", err)
				}
				if env != nil {
					cmd.Env = mergeProcessEnv(env)
				}
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			exitCode := 0
			if err := processStartAndWait(runCtx, cmd, processTree); err != nil {
				if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
					exitCode = 124
					if stderr.Len() > 0 {
						stderr.WriteByte('\n')
					}
					stderr.WriteString("process-run*: timed out after ")
					stderr.WriteString(strconv.FormatInt(timeoutMs, 10))
					stderr.WriteString(" ms")
				} else if errors.Is(runCtx.Err(), context.Canceled) {
					return nil, processRuntimeError("doggo.process/run", "process-run*: %v", runCtx.Err())
				} else {
					exitErr, ok := err.(*exec.ExitError)
					if !ok {
						return nil, processRuntimeError("doggo.process/run", "process-run*: %v", err)
					}
					exitCode = exitErr.ExitCode()
				}
			}

			if !utf8.Valid(stdout.Bytes()) || !utf8.Valid(stderr.Bytes()) {
				return nil, processRuntimeError("doggo.process/run", "process-run*: output is not valid UTF-8")
			}
			result = coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("exit"), atom.NewNumberInt(int64(exitCode)),
				atom.NewKeyword("out"), atom.NewString(stdout.String()),
				atom.NewKeyword("err"), atom.NewString(stderr.String()),
			})
			return result, nil
		}},
	},
}
