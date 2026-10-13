package vm

import (
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

type ednReadOptions struct {
	readers   map[string]types.Value
	defaultFn types.Value
	eofValue  types.Value
	hasEOF    bool
	location  bool
}

type ednWriteOptions struct {
	pretty   bool
	indent   int
	sortKeys bool
	newline  bool
}

var (
	ednKeywordEOF      = atom.NewKeyword("eof")
	ednKeywordReaders  = atom.NewKeyword("readers")
	ednKeywordDefault  = atom.NewKeyword("default")
	ednKeywordLocation = atom.NewKeyword("location?")
	ednKeywordPretty   = atom.NewKeyword("pretty?")
	ednKeywordIndent   = atom.NewKeyword("indent")
	ednKeywordSortKeys = atom.NewKeyword("sort-keys?")
	ednKeywordNewline  = atom.NewKeyword("newline?")
)

func ednParseReadOptions(label string, arg types.Value, allowEOF bool) (ednReadOptions, error) {
	options := ednReadOptions{
		readers: make(map[string]types.Value),
	}

	value, ok := arg.(types.Map)
	if !ok {
		return options, ednInvalidArg(label, "expected option map, got %s", arg.Type())
	}

	for _, key := range sequableToSlice(value.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok {
			return options, ednInvalidArg(label, "option keys must be keywords, got %s", key.Type())
		}
		switch keyword {
		case ednKeywordEOF, ednKeywordReaders, ednKeywordDefault, ednKeywordLocation:
		default:
			return options, ednInvalidArg(label, "unsupported option %s", keyword.String())
		}
		if keyword == ednKeywordEOF && !allowEOF {
			return options, ednInvalidArg(label, "unsupported option %s", keyword.String())
		}
	}

	if readersValue := value.Get(ednKeywordReaders); readersValue != nil && readersValue != atom.NilValue {
		readerMap, ok := readersValue.(types.Map)
		if !ok {
			return options, ednInvalidArg(label, "option :readers must be map, got %s", readersValue.Type())
		}

		for _, key := range sequableToSlice(readerMap.Keys()) {
			tagName, err := ednReaderTagName(key)
			if err != nil {
				return options, ednInvalidArg(label, "option :readers %v", err)
			}

			readerFn := readerMap.Get(key)
			if !isCallable(readerFn) {
				return options, ednInvalidArg(label, "option :readers[%s] must be function, got %s", tagName, readerFn.Type())
			}
			options.readers[tagName] = readerFn
		}
	}

	if defaultValue := value.Get(ednKeywordDefault); defaultValue != nil && defaultValue != atom.NilValue {
		if !isCallable(defaultValue) {
			return options, ednInvalidArg(label, "option :default must be function, got %s", defaultValue.Type())
		}
		options.defaultFn = defaultValue
	}

	if eofValue := value.Get(ednKeywordEOF); eofValue != nil {
		options.eofValue = eofValue
		options.hasEOF = true
	}

	if locationValue := value.Get(ednKeywordLocation); locationValue != nil {
		location, ok := locationValue.(atom.Boolean)
		if !ok {
			return options, ednInvalidArg(label, "option :location? must be boolean, got %s", locationValue.Type())
		}
		options.location = bool(location)
	}

	return options, nil
}

func ednParseWriteOptions(label string, arg types.Value) (ednWriteOptions, error) {
	options := ednWriteOptions{
		indent: 2,
	}

	value, ok := arg.(types.Map)
	if !ok {
		return options, ednInvalidArg(label, "expected option map, got %s", arg.Type())
	}

	for _, key := range sequableToSlice(value.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok {
			return options, ednInvalidArg(label, "option keys must be keywords, got %s", key.Type())
		}
		switch keyword {
		case ednKeywordPretty, ednKeywordIndent, ednKeywordSortKeys, ednKeywordNewline:
		default:
			return options, ednInvalidArg(label, "unsupported option %s", keyword.String())
		}
	}

	if prettyValue := value.Get(ednKeywordPretty); prettyValue != nil {
		pretty, err := ednOptionBoolean(label, ednKeywordPretty, prettyValue)
		if err != nil {
			return options, err
		}
		options.pretty = pretty
	}

	if indentValue := value.Get(ednKeywordIndent); indentValue != nil {
		indent, err := ednOptionNonNegativeInt(label, ednKeywordIndent, indentValue)
		if err != nil {
			return options, err
		}
		options.indent = indent
	}

	if sortKeysValue := value.Get(ednKeywordSortKeys); sortKeysValue != nil {
		sortKeys, err := ednOptionBoolean(label, ednKeywordSortKeys, sortKeysValue)
		if err != nil {
			return options, err
		}
		options.sortKeys = sortKeys
	}

	if newlineValue := value.Get(ednKeywordNewline); newlineValue != nil {
		newline, err := ednOptionBoolean(label, ednKeywordNewline, newlineValue)
		if err != nil {
			return options, err
		}
		options.newline = newline
	}

	return options, nil
}

func ednOptionBoolean(label string, keyword atom.Keyword, value types.Value) (bool, error) {
	boolean, ok := value.(atom.Boolean)
	if !ok {
		return false, ednInvalidArg(label, "option %s must be boolean, got %s", keyword.String(), value.Type())
	}
	return bool(boolean), nil
}

func ednOptionNonNegativeInt(label string, keyword atom.Keyword, value types.Value) (int, error) {
	number, ok := value.(types.Number)
	if !ok {
		return 0, ednInvalidArg(label, "option %s must be a non-negative integer, got %s", keyword.String(), value.Type())
	}
	intValue, isInteger := types.Int64Value(number)
	if !isInteger {
		return 0, ednInvalidArg(label, "option %s must be a non-negative integer, got %s", keyword.String(), value.Type())
	}
	if intValue < 0 {
		return 0, ednInvalidArg(label, "option %s must be a non-negative integer, got %s", keyword.String(), value.String())
	}
	return int(intValue), nil
}

func ednReaderTagName(value types.Value) (string, error) {
	switch tag := value.(type) {
	case types.Symbol:
		return tag.String(), nil
	case atom.String:
		return string(tag), nil
	default:
		return "", fmt.Errorf("keys must be symbols or strings, got %s", value.Type())
	}
}

func ednIsSortableScalarKey(value types.Value) bool {
	switch value.(type) {
	case types.Nil, atom.Boolean, types.Number, atom.String, atom.Keyword, atom.Character, types.Symbol, *atom.Instant, *atom.UUID:
		return true
	default:
		return false
	}
}
