// builtins_channel.go は Channel（非同期通信）関連の組み込み関数を定義します。
//
// Channel は Go の channel に相当する非同期通信プリミティブです。
// goroutine 間のメッセージパッシングを提供します。
//
// 提供する関数:
//   - 作成: chan
//   - 送受信: >!, <!, alts!
//   - 制御: close!
//   - 述語: chan?
package vm

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsChannel は Channel（非同期通信）関連の組み込み関数を定義します
var builtinsChannel = []BuiltinDefinition{
	{
		Name: "chan",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("chan", args, 0, 1); err != nil {
				return err
			}
			bufSize := 0
			if len(args) >= 1 {
				bufferSize, ok := integerForOperation(args[0])
				if !ok {
					return newArgumentError("chan: buffer size must be a number, got %s", args[0].Type())
				}
				bufSize = int(bufferSize)
				if bufSize < 0 {
					return newArgumentError("chan: buffer size must be non-negative, got %d", bufSize)
				}
			}
			channel, err := types.NewChannel(bufSize)
			if err != nil {
				return RuntimeErrf("chan", "%v", err)
			}
			return channel
		}},
	},
	{
		Name: "chan?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("chan?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*types.Channel)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "alts!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("alts!", args, 1, 2); err != nil {
				return nil, err
			}

			ports, errVal := parseAltsPorts(args[0])
			if errVal != nil {
				return errVal, nil
			}

			opts := altsOptions{}
			if len(args) == 2 {
				parsedOpts, optErr := parseAltsOptions(args[1])
				if optErr != nil {
					return optErr, nil
				}
				opts = parsedOpts
			}
			if ctx == nil {
				ctx = context.Background()
			}

			if opts.priority {
				result, selected, priorityErr := selectPriorityReadyPort(ctx, ports)
				if priorityErr != nil {
					return nil, priorityErr
				}
				if selected {
					return result, nil
				}
			}

			return selectWithOptions(ctx, ports, opts)
		}},
	},
	{
		Name: ">!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (result types.Value, err error) {
			if err := CheckArgCountCtx(">!", args, 2); err != nil {
				return nil, err
			}
			ch, ok := args[0].(*types.Channel)
			if !ok {
				return nil, newArgumentError(">!: first argument must be a channel, got %s", args[0].Type())
			}
			if ctx == nil {
				ctx = context.Background()
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					result = nil
					err = RuntimeErrf(">!", "%v", recovered)
				}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case ch.Value <- args[1]:
				return atom.NilValue, nil
			}
		}},
	},
	{
		Name: "<!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("<!", args, 1); err != nil {
				return nil, err
			}
			ch, ok := args[0].(*types.Channel)
			if !ok {
				return nil, newArgumentError("<!: argument must be a channel, got %s", args[0].Type())
			}
			if ctx == nil {
				ctx = context.Background()
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case val, ok := <-ch.Value:
				if !ok {
					return atom.NilValue, nil
				}
				return val, nil
			}
		}},
	},
	{
		Name: "close!",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("close!", args, 1); err != nil {
				return err
			}
			ch, ok := args[0].(*types.Channel)
			if !ok {
				return newArgumentError("close!: argument must be a channel, got %s", args[0].Type())
			}
			if err := ch.Close(); err != nil {
				return RuntimeErrf("close!", "%v", err)
			}
			return atom.NilValue
		}},
	},
}

type altsPortKind int

const (
	altsPortRecv altsPortKind = iota
	altsPortSend
)

type altsPort struct {
	kind    altsPortKind
	ch      *types.Channel
	sendVal types.Value
}

type altsOptions struct {
	hasDefault   bool
	defaultValue types.Value
	hasTimeout   bool
	timeoutMs    int64
	priority     bool
}

func parseAltsPorts(raw types.Value) ([]altsPort, types.Value) {
	var items []types.Value

	switch ports := raw.(type) {
	case *coll.Vector:
		items = ports.Items
	case *coll.List:
		items = ports.Items
	default:
		return nil, newArgumentError("alts!: first argument must be a vector or list of channels, got %s", valueTypeName(raw))
	}

	if len(items) == 0 {
		return nil, newArgumentError("alts!: ports must not be empty")
	}

	parsedPorts := make([]altsPort, 0, len(items))
	for i, item := range items {
		switch port := item.(type) {
		case *types.Channel:
			parsedPorts = append(parsedPorts, altsPort{
				kind: altsPortRecv,
				ch:   port,
			})
		case *coll.Vector:
			if len(port.Items) != 2 {
				return nil, newArgumentError("alts!: ports[%d] send port must be [channel value]", i)
			}

			ch, ok := port.Items[0].(*types.Channel)
			if !ok {
				return nil, newArgumentError("alts!: ports[%d] send port first element must be a channel, got %s", i, valueTypeName(port.Items[0]))
			}

			sendVal := port.Items[1]
			if sendVal == nil {
				sendVal = atom.NilValue
			}

			parsedPorts = append(parsedPorts, altsPort{
				kind:    altsPortSend,
				ch:      ch,
				sendVal: sendVal,
			})
		default:
			return nil, newArgumentError("alts!: ports[%d] must be a channel, got %s", i, valueTypeName(item))
		}
	}

	return parsedPorts, nil
}

func parseAltsOptions(raw types.Value) (altsOptions, types.Value) {
	optsMap, ok := raw.(types.Map)
	if !ok {
		return altsOptions{}, newArgumentError("alts!: opts must be a map, got %s", valueTypeName(raw))
	}

	opts := altsOptions{
		defaultValue: atom.NilValue,
	}

	unknownSet := map[string]struct{}{}
	known := map[string]struct{}{
		"default":  {},
		"timeout":  {},
		"priority": {},
	}

	keys := optsMap.Keys()
	if keys != nil {
		keyValue, ok := keys.(types.Value)
		if ok {
			items, err := toSlice(keyValue)
			if err != nil {
				return altsOptions{}, newArgumentError("%v", err)
			}
			for _, key := range items {
				switch k := key.(type) {
				case atom.Keyword:
					if _, exists := known[string(k)]; !exists {
						unknownSet[k.String()] = struct{}{}
					}
				default:
					unknownSet[key.String()] = struct{}{}
				}
			}
		}
	}

	defaultVal := optsMap.Get(atom.Keyword("default"))
	if defaultVal != nil {
		opts.hasDefault = true
		opts.defaultValue = defaultVal
	}

	timeoutVal := optsMap.Get(atom.Keyword("timeout"))
	if timeoutVal != nil {
		opts.hasTimeout = true
		num, ok := timeoutVal.(types.Number)
		if !ok {
			return altsOptions{}, newArgumentError("alts!: :timeout must be a number, got %s", valueTypeName(timeoutVal))
		}

		timeout := num.ToFloat64()
		if timeout < 0 {
			return altsOptions{}, newArgumentError("alts!: :timeout must be non-negative")
		}
		timeout = math.Ceil(timeout)
		maxTimeoutMs := float64((time.Duration(1<<63 - 1)) / time.Millisecond)
		if math.IsNaN(timeout) || timeout > maxTimeoutMs {
			return altsOptions{}, newArgumentError("alts!: :timeout is out of range")
		}
		opts.timeoutMs = int64(timeout)
	}

	priorityVal := optsMap.Get(atom.Keyword("priority"))
	if priorityVal != nil {
		priority, ok := priorityVal.(atom.Boolean)
		if !ok {
			return altsOptions{}, newArgumentError("alts!: :priority must be boolean, got %s", valueTypeName(priorityVal))
		}
		opts.priority = bool(priority)
	}

	if opts.hasDefault && opts.hasTimeout {
		return altsOptions{}, newArgumentError("alts!: :default and :timeout cannot be used together")
	}

	if len(unknownSet) > 0 {
		unknownOptions := make([]string, 0, len(unknownSet))
		for key := range unknownSet {
			unknownOptions = append(unknownOptions, key)
		}
		sort.Strings(unknownOptions)
		return altsOptions{}, newArgumentError("alts!: unsupported option %s", unknownOptions[0])
	}

	return opts, nil
}

func selectPriorityReadyPort(ctx context.Context, ports []altsPort) (types.Value, bool, error) {
	for _, port := range ports {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		cases := []reflect.SelectCase{
			buildAltsSelectCase(port),
			{Dir: reflect.SelectDefault},
		}
		selectedIdx, recv, recvOK, errVal := safeReflectSelect(cases)
		if errVal != nil {
			return nil, false, errVal
		}
		if selectedIdx == 0 {
			result, resultErr := buildAltsPortResult(port, recv, recvOK)
			if resultErr != nil {
				return nil, false, resultErr
			}
			return result, true, nil
		}
	}
	return nil, false, nil
}

func selectWithOptions(ctx context.Context, ports []altsPort, opts altsOptions) (types.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cases := make([]reflect.SelectCase, 0, len(ports)+2)
	for _, port := range ports {
		cases = append(cases, buildAltsSelectCase(port))
	}
	cancelIndex := len(cases)
	cases = append(cases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	})

	if opts.hasDefault {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectDefault})
	} else if opts.hasTimeout {
		timer := time.NewTimer(time.Duration(opts.timeoutMs) * time.Millisecond)
		defer timer.Stop()
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(timer.C),
		})
	}

	selectedIdx, recv, recvOK, errVal := safeReflectSelect(cases)
	if errVal != nil {
		return nil, errVal
	}

	if selectedIdx < len(ports) {
		return buildAltsPortResult(ports[selectedIdx], recv, recvOK)
	}
	if selectedIdx == cancelIndex {
		return nil, ctx.Err()
	}

	if opts.hasDefault {
		return makeAltsResult(opts.defaultValue, atom.Keyword("default")), nil
	}
	if opts.hasTimeout {
		return makeAltsResult(atom.NilValue, atom.Keyword("timeout")), nil
	}
	return nil, RuntimeErr("alts!", "internal error: unexpected select result")
}

func buildAltsSelectCase(port altsPort) reflect.SelectCase {
	switch port.kind {
	case altsPortRecv:
		return reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(port.ch.Value),
		}
	case altsPortSend:
		sendVal := port.sendVal
		if sendVal == nil {
			sendVal = atom.NilValue
		}
		return reflect.SelectCase{
			Dir:  reflect.SelectSend,
			Chan: reflect.ValueOf(port.ch.Value),
			Send: reflect.ValueOf(sendVal),
		}
	default:
		return reflect.SelectCase{Dir: reflect.SelectDefault}
	}
}

func buildAltsPortResult(port altsPort, recv reflect.Value, recvOK bool) (types.Value, error) {
	switch port.kind {
	case altsPortSend:
		return makeAltsResult(atom.True, port.ch), nil
	case altsPortRecv:
		var val types.Value = atom.NilValue
		if recvOK {
			recvAny := recv.Interface()
			if recvAny != nil {
				recvVal, ok := recvAny.(types.Value)
				if !ok {
					return nil, RuntimeErrf("alts!", "internal error: received non-doggo value %T", recvAny)
				}
				val = recvVal
			}
		}
		return makeAltsResult(val, port.ch), nil
	default:
		return nil, RuntimeErr("alts!", "internal error: unknown port kind")
	}
}

func safeReflectSelect(cases []reflect.SelectCase) (selectedIdx int, recv reflect.Value, recvOK bool, errVal error) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("%v", r)
			if strings.Contains(msg, "send on closed channel") {
				errVal = RuntimeErr("alts!", "send on closed channel")
				return
			}
			errVal = RuntimeErr("alts!", msg)
		}
	}()
	selectedIdx, recv, recvOK = reflect.Select(cases)
	return selectedIdx, recv, recvOK, nil
}

func makeAltsResult(value, selected types.Value) types.Value {
	if value == nil {
		value = atom.NilValue
	}
	return coll.NewVector(value, selected)
}

func valueTypeName(v types.Value) string {
	if v == nil {
		return "nil"
	}
	return v.Type()
}
