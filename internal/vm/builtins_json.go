package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type jsonKeyMode string
type jsonNumberMode string

const (
	jsonKeyModeString  jsonKeyMode = "string"
	jsonKeyModeKeyword jsonKeyMode = "keyword"

	jsonNumberModeIntOrFloat jsonNumberMode = "int-or-float"
	jsonNumberModeFloat      jsonNumberMode = "float"
	jsonNumberModeString     jsonNumberMode = "string"
)

type jsonReadOptions struct {
	keyMode    jsonKeyMode
	numberMode jsonNumberMode
}

type jsonWriteOptions struct {
	pretty bool
	prefix string
	indent string
}

var (
	jsonKeywordKeyFn      = atom.NewKeyword("key-fn")
	jsonKeywordNumberMode = atom.NewKeyword("number-mode")
	jsonKeywordPretty     = atom.NewKeyword("pretty")
	jsonKeywordPrefix     = atom.NewKeyword("prefix")
	jsonKeywordIndent     = atom.NewKeyword("indent")
)

var builtinsJSON = []BuiltinDefinition{
	{
		Name:       "json-read-string*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("doggo.json/read-string", args, 2); err != nil {
				return nil, err
			}

			input, ok := args[0].(atom.String)
			if !ok {
				return nil, jsonInvalidArg("doggo.json/read-string", "expected string input, got %s", args[0].Type())
			}
			if err := validateJSONUnicodeEscapes(string(input)); err != nil {
				return nil, jsonInvalidArg("doggo.json/read-string", "%v", err)
			}

			options, err := jsonParseReadOptions(args[1])
			if err != nil {
				return nil, err
			}

			decoder := json.NewDecoder(strings.NewReader(string(input)))
			decoder.UseNumber()

			var parsed any
			if err := decoder.Decode(&parsed); err != nil {
				return nil, jsonInvalidArg("doggo.json/read-string", "invalid JSON: %v", err)
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				if err == nil {
					return nil, jsonInvalidArg("doggo.json/read-string", "expected a single JSON value")
				}
				return nil, jsonInvalidArg("doggo.json/read-string", "invalid trailing JSON: %v", err)
			}

			return jsonToDoggoValue(parsed, options)
		}},
	},
	{
		Name:       "json-write-string*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("doggo.json/write-string", args, 2); err != nil {
				return nil, err
			}

			options, err := jsonParseWriteOptions(args[1])
			if err != nil {
				return nil, err
			}

			normalized, err := jsonFromDoggoValue(args[0])
			if err != nil {
				return nil, err
			}

			var data []byte
			if options.pretty {
				data, err = json.MarshalIndent(normalized, options.prefix, options.indent)
			} else {
				data, err = json.Marshal(normalized)
			}
			if err != nil {
				return nil, jsonInvalidArg("doggo.json/write-string", "cannot encode value: %v", err)
			}

			return atom.NewString(string(data)), nil
		}},
	},
}

func validateJSONUnicodeEscapes(input string) error {
	inString := false
	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || i+1 >= len(input) {
				continue
			}
			i++
			if input[i] != 'u' || i+4 >= len(input) {
				continue
			}
			unit, err := strconv.ParseUint(input[i+1:i+5], 16, 16)
			if err != nil {
				continue
			}
			i += 4
			if 0xD800 <= unit && unit <= 0xDBFF {
				if i+6 >= len(input) || input[i+1] != '\\' || input[i+2] != 'u' {
					return fmt.Errorf("high surrogate must be followed by a low surrogate")
				}
				low, err := strconv.ParseUint(input[i+3:i+7], 16, 16)
				if err != nil || low < 0xDC00 || low > 0xDFFF {
					return fmt.Errorf("high surrogate must be followed by a low surrogate")
				}
				i += 6
			} else if 0xDC00 <= unit && unit <= 0xDFFF {
				return fmt.Errorf("low surrogate must follow a high surrogate")
			}
		}
	}
	return nil
}

func jsonInvalidArg(label, format string, args ...any) error {
	return invalidArgumentVMError(label, fmt.Sprintf("%s: %s", label, fmt.Sprintf(format, args...)))
}

func jsonParseReadOptions(arg types.Value) (jsonReadOptions, error) {
	options := jsonReadOptions{
		keyMode:    jsonKeyModeString,
		numberMode: jsonNumberModeIntOrFloat,
	}

	value, ok := arg.(types.Map)
	if !ok {
		return options, argumentVMError(
			"doggo.json/read-string",
			"options",
			"map",
			arg,
			fmt.Sprintf("doggo.json/read-string: expected option map, got %s", arg.Type()),
		)
	}

	for _, key := range sequableToSlice(value.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok {
			return options, jsonInvalidArg("doggo.json/read-string", "option keys must be keywords, got %s", key.Type())
		}
		switch keyword {
		case jsonKeywordKeyFn, jsonKeywordNumberMode:
		default:
			return options, jsonInvalidArg("doggo.json/read-string", "unsupported option %s", keyword.String())
		}
	}

	if keyFn := value.Get(jsonKeywordKeyFn); keyFn != nil && keyFn != atom.NilValue {
		mode, err := jsonParseKeyMode(keyFn)
		if err != nil {
			return options, jsonInvalidArg("doggo.json/read-string", "option :key-fn %v", err)
		}
		options.keyMode = mode
	}

	if numberMode := value.Get(jsonKeywordNumberMode); numberMode != nil && numberMode != atom.NilValue {
		mode, err := jsonParseNumberMode(numberMode)
		if err != nil {
			return options, jsonInvalidArg("doggo.json/read-string", "option :number-mode %v", err)
		}
		options.numberMode = mode
	}

	return options, nil
}

func jsonParseWriteOptions(arg types.Value) (jsonWriteOptions, error) {
	options := jsonWriteOptions{indent: "  "}
	value, ok := arg.(types.Map)
	if !ok {
		return options, argumentVMError(
			"doggo.json/write-string",
			"options",
			"map",
			arg,
			fmt.Sprintf("doggo.json/write-string: expected option map, got %s", arg.Type()),
		)
	}
	for _, key := range sequableToSlice(value.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok {
			return options, jsonInvalidArg("doggo.json/write-string", "option keys must be keywords, got %s", key.Type())
		}
		if keyword != jsonKeywordPretty {
			return options, jsonInvalidArg("doggo.json/write-string", "unsupported option %s", keyword.String())
		}
	}

	if pretty := value.Get(jsonKeywordPretty); pretty != nil && pretty != atom.NilValue {
		switch prettyValue := pretty.(type) {
		case atom.Boolean:
			options.pretty = bool(prettyValue)
		case types.Map:
			options.pretty = true
			prettyOptions, err := jsonParsePrettyOptions(prettyValue)
			if err != nil {
				return options, err
			}
			options.prefix = prettyOptions.prefix
			options.indent = prettyOptions.indent
		default:
			return options, jsonInvalidArg("doggo.json/write-string", "option :pretty must be boolean or option map, got %s", pretty.Type())
		}
	}

	return options, nil
}

func jsonParsePrettyOptions(value types.Map) (jsonWriteOptions, error) {
	options := jsonWriteOptions{
		pretty: true,
		indent: "  ",
	}

	for _, key := range sequableToSlice(value.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok {
			return options, jsonInvalidArg("doggo.json/write-string", "option :pretty keys must be keywords, got %s", key.Type())
		}
		switch keyword {
		case jsonKeywordPrefix, jsonKeywordIndent:
		default:
			return options, jsonInvalidArg("doggo.json/write-string", "option :pretty does not support %s", keyword.String())
		}
	}

	if prefix := value.Get(jsonKeywordPrefix); prefix != nil && prefix != atom.NilValue {
		text, ok := prefix.(atom.String)
		if !ok {
			return options, jsonInvalidArg("doggo.json/write-string", "option :pretty.prefix must be string, got %s", prefix.Type())
		}
		options.prefix = string(text)
	}

	if indent := value.Get(jsonKeywordIndent); indent != nil && indent != atom.NilValue {
		text, ok := indent.(atom.String)
		if !ok {
			return options, jsonInvalidArg("doggo.json/write-string", "option :pretty.indent must be string, got %s", indent.Type())
		}
		options.indent = string(text)
	}

	return options, nil
}

func jsonParseKeyMode(value types.Value) (jsonKeyMode, error) {
	switch v := value.(type) {
	case atom.Keyword:
		switch string(v) {
		case "string":
			return jsonKeyModeString, nil
		case "keyword":
			return jsonKeyModeKeyword, nil
		}
	case atom.String:
		switch string(v) {
		case "string":
			return jsonKeyModeString, nil
		case "keyword":
			return jsonKeyModeKeyword, nil
		}
	}
	return "", fmt.Errorf("must be :string or :keyword, got %s", value.String())
}

func jsonParseNumberMode(value types.Value) (jsonNumberMode, error) {
	switch v := value.(type) {
	case atom.Keyword:
		switch string(v) {
		case "int-or-float":
			return jsonNumberModeIntOrFloat, nil
		case "float":
			return jsonNumberModeFloat, nil
		case "string":
			return jsonNumberModeString, nil
		}
	case atom.String:
		switch string(v) {
		case "int-or-float":
			return jsonNumberModeIntOrFloat, nil
		case "float":
			return jsonNumberModeFloat, nil
		case "string":
			return jsonNumberModeString, nil
		}
	}
	return "", fmt.Errorf("must be :int-or-float, :float, or :string, got %s", value.String())
}

func jsonToDoggoValue(value any, options jsonReadOptions) (types.Value, error) {
	switch v := value.(type) {
	case nil:
		return atom.NilValue, nil
	case bool:
		return atom.NewBoolean(v), nil
	case string:
		return atom.NewString(v), nil
	case json.Number:
		return jsonNumberToDoggoValue(v, options.numberMode)
	case []any:
		items := make([]types.Value, len(v))
		for i, item := range v {
			converted, err := jsonToDoggoValue(item, options)
			if err != nil {
				return nil, err
			}
			items[i] = converted
		}
		return coll.NewVector(items...), nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		pairs := make([]types.Value, 0, len(v)*2)
		for _, key := range keys {
			converted, err := jsonToDoggoValue(v[key], options)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, jsonObjectKeyValue(key, options.keyMode), converted)
		}
		return coll.NewMapFromPairs(pairs), nil
	default:
		return nil, jsonInvalidArg("doggo.json/read-string", "unsupported JSON value %T", value)
	}
}

func jsonObjectKeyValue(key string, mode jsonKeyMode) types.Value {
	if mode == jsonKeyModeKeyword {
		return atom.NewKeyword(key)
	}
	return atom.NewString(key)
}

func jsonNumberToDoggoValue(value json.Number, mode jsonNumberMode) (types.Value, error) {
	text := value.String()
	switch mode {
	case jsonNumberModeString:
		return atom.NewString(text), nil
	case jsonNumberModeFloat:
		parsed, err := value.Float64()
		if err != nil {
			return nil, jsonInvalidArg("doggo.json/read-string", "invalid JSON number %q", text)
		}
		return atom.NewNumberFloat(parsed), nil
	default:
		if !strings.ContainsAny(text, ".eE") {
			parsed, err := strconv.ParseInt(text, 10, 64)
			if err == nil {
				return atom.NewNumberInt(parsed), nil
			}
			if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
				return nil, jsonInvalidArg("doggo.json/read-string", "integer %q out of int64 range; use {:number-mode :string} to preserve it", text)
			}
			return nil, jsonInvalidArg("doggo.json/read-string", "invalid JSON number %q", text)
		}
		parsed, err := value.Float64()
		if err != nil {
			return nil, jsonInvalidArg("doggo.json/read-string", "invalid JSON number %q", text)
		}
		return atom.NewNumberFloat(parsed), nil
	}
}

func jsonFromDoggoValue(value types.Value) (any, error) {
	normalized := datafyValue(value)
	if normalized != value {
		return jsonFromDoggoValue(normalized)
	}

	if intValue, ok := value.(types.Int64Valuer); ok {
		if exact, ok := intValue.Int64Value(); ok {
			return exact, nil
		}
	}

	switch v := value.(type) {
	case nil:
		return nil, nil
	case types.Nil:
		return nil, nil
	case atom.Boolean:
		return bool(v), nil
	case atom.String:
		return string(v), nil
	case atom.Character:
		return string(v), nil
	case atom.Keyword:
		return string(v), nil
	case types.Number:
		return v.ToFloat64(), nil
	case types.Map:
		return jsonFromDoggoMap(v)
	case *coll.Vector:
		return jsonFromDoggoSlice(v.Items)
	case *coll.List:
		return jsonFromDoggoSlice(v.Items)
	case *coll.Set:
		return jsonFromDoggoSlice(v.AllElements())
	default:
		if name, ok := types.SymbolName(value); ok {
			return name, nil
		}
		if seq, ok := value.(types.Seq); ok {
			items, err := toSlice(seq)
			if err != nil {
				return nil, err
			}
			return jsonFromDoggoSlice(items)
		}
		if sequable, ok := value.(types.Sequable); ok {
			items, err := toSlice(sequable.Seq())
			if err != nil {
				return nil, err
			}
			return jsonFromDoggoSlice(items)
		}
		return nil, jsonInvalidArg("doggo.json/write-string", "value of type %s cannot be encoded as JSON", value.Type())
	}
}

func jsonFromDoggoMap(value types.Map) (map[string]any, error) {
	result := make(map[string]any)
	for _, key := range sequableToSlice(value.Keys()) {
		keyString, err := jsonMapKeyString(key)
		if err != nil {
			return nil, err
		}
		converted, err := jsonFromDoggoValue(value.Get(key))
		if err != nil {
			return nil, err
		}
		result[keyString] = converted
	}
	return result, nil
}

func jsonFromDoggoSlice(values []types.Value) ([]any, error) {
	result := make([]any, len(values))
	for i, value := range values {
		converted, err := jsonFromDoggoValue(value)
		if err != nil {
			return nil, err
		}
		result[i] = converted
	}
	return result, nil
}

func jsonMapKeyString(value types.Value) (string, error) {
	switch v := value.(type) {
	case atom.String:
		return string(v), nil
	case atom.Keyword:
		return string(v), nil
	default:
		if name, ok := types.SymbolName(value); ok {
			return name, nil
		}
		return "", jsonInvalidArg("doggo.json/write-string", "JSON object keys must be strings, symbols, or keywords, got %s", value.Type())
	}
}
