package vm

import (
	"bytes"
	"context"
	"encoding/csv"
	"strconv"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

const (
	csvReadStringFunction  = "doggo.csv/read-string"
	csvReadFunction        = "doggo.csv/read"
	csvWriteStringFunction = "doggo.csv/write-string"
	csvWriteFunction       = "doggo.csv/write"
)

var builtinsCSV = []BuiltinDefinition{
	{
		Name:       "csv-read-string*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgRangeCtx(csvReadStringFunction, args, 1, 2); err != nil {
				return nil, err
			}
			source, ok := args[0].(atom.String)
			if !ok {
				return nil, argumentVMError(
					csvReadStringFunction,
					"source",
					"valid UTF-8 string",
					args[0],
					csvReadStringFunction+": source must be a valid UTF-8 string",
				)
			}
			options := types.Value(coll.NewEmptyMap())
			if len(args) == 2 {
				options = args[1]
			}
			return csvDecode(csvReadStringFunction, []byte(source), options)
		}},
	},
	{
		Name:       "csv-read*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgRangeCtx(csvReadFunction, args, 1, 2); err != nil {
				return nil, err
			}
			source, ok := args[0].(*coll.Vector)
			if !ok || source == nil {
				return nil, argumentVMError(
					csvReadFunction,
					"source",
					"vector of bytes",
					args[0],
					csvReadFunction+": source must be a vector of bytes",
				)
			}
			data, err := vectorToBytes(source)
			if err != nil {
				return nil, argumentVMError(
					csvReadFunction,
					"source",
					"vector of bytes",
					source,
					csvReadFunction+": source must be a vector of bytes",
				)
			}
			options := types.Value(coll.NewEmptyMap())
			if len(args) == 2 {
				options = args[1]
			}
			return csvDecode(csvReadFunction, data, options)
		}},
	},
	{
		Name:       "csv-write-string*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx(csvWriteStringFunction, args, 1); err != nil {
				return nil, err
			}
			encoded, err := csvEncode(csvWriteStringFunction, args[0])
			if err != nil {
				return nil, err
			}
			return atom.NewString(encoded), nil
		}},
	},
	{
		Name:       "csv-write*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx(csvWriteFunction, args, 1); err != nil {
				return nil, err
			}
			encoded, err := csvEncode(csvWriteFunction, args[0])
			if err != nil {
				return nil, err
			}
			return atom.NewString(encoded), nil
		}},
	},
}

func csvDecode(function string, data []byte, options types.Value) (types.Value, error) {
	const optionsExpected = "map containing only optional :headers boolean"

	if !utf8.Valid(data) {
		return nil, argumentVMError(
			function,
			"source",
			"valid UTF-8 string",
			atom.NewString(string(data)),
			function+": source must be a valid UTF-8 string",
		)
	}

	optionMap, ok := options.(types.Map)
	if !ok {
		return nil, argumentVMError(
			function,
			"options",
			optionsExpected,
			options,
			function+": options must be a map containing only optional :headers boolean",
		)
	}
	headers := false
	for _, key := range sequableToSlice(optionMap.Keys()) {
		keyword, ok := key.(atom.Keyword)
		if !ok || keyword != atom.NewKeyword("headers") {
			return nil, argumentVMError(
				function,
				"options",
				optionsExpected,
				options,
				function+": options must contain only the :headers keyword",
			)
		}
		value := optionMap.Get(keyword)
		enabled, ok := value.(atom.Boolean)
		if !ok {
			return nil, argumentVMError(
				function,
				"options",
				optionsExpected,
				options,
				function+": option :headers must be boolean",
			)
		}
		headers = bool(enabled)
	}

	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = 0
	records, err := reader.ReadAll()
	if err != nil {
		return nil, argumentVMError(
			function,
			"source",
			"valid UTF-8 CSV",
			atom.NewString(string(data)),
			function+": invalid CSV: "+err.Error(),
		)
	}
	if len(records) == 0 {
		return coll.NewVector(), nil
	}

	start := 0
	var header []string
	if headers {
		header = records[0]
		seen := make(map[string]struct{}, len(header))
		values := make([]types.Value, len(header))
		for i := range header {
			values[i] = atom.NewString(header[i])
		}
		for _, name := range header {
			if name == "" {
				return nil, argumentVMError(
					function,
					"headers",
					"unique non-empty header strings",
					coll.NewVector(values...),
					function+": CSV headers must not be empty",
				)
			}
			if _, duplicate := seen[name]; duplicate {
				return nil, argumentVMError(
					function,
					"headers",
					"unique non-empty header strings",
					coll.NewVector(values...),
					function+": CSV headers must be unique",
				)
			}
			seen[name] = struct{}{}
		}
		start = 1
	}

	rows := make([]types.Value, 0, len(records)-start)
	for _, record := range records[start:] {
		if headers {
			pairs := make([]types.Value, 0, len(record)*2)
			for i, cell := range record {
				pairs = append(pairs, atom.NewString(header[i]), atom.NewString(cell))
			}
			rows = append(rows, coll.NewMapFromPairs(pairs))
			continue
		}
		cells := make([]types.Value, len(record))
		for i, cell := range record {
			cells[i] = atom.NewString(cell)
		}
		rows = append(rows, coll.NewVector(cells...))
	}
	return coll.NewVector(rows...), nil
}

func csvEncode(function string, rows types.Value) (string, error) {
	rowVector, ok := rows.(*coll.Vector)
	if !ok {
		return "", argumentVMError(
			function,
			"rows",
			"vector of non-empty equal-width row vectors",
			rows,
			function+": rows must be a vector of row vectors",
		)
	}
	if rowVector == nil || len(rowVector.Items) == 0 {
		return "", nil
	}

	records := make([][]string, len(rowVector.Items))
	width := 0
	for rowIndex, value := range rowVector.Items {
		row, ok := value.(*coll.Vector)
		if !ok || row == nil || len(row.Items) == 0 {
			return "", argumentVMError(
				function,
				"row",
				"non-empty row vector",
				value,
				function+": row "+strconv.Itoa(rowIndex)+" must be a non-empty vector",
			)
		}
		if rowIndex == 0 {
			width = len(row.Items)
		} else if len(row.Items) != width {
			return "", argumentVMError(
				function,
				"row",
				"row vector with "+strconv.Itoa(width)+" cells",
				value,
				function+": row "+strconv.Itoa(rowIndex)+" has inconsistent width",
			)
		}
		record := make([]string, width)
		for cellIndex, value := range row.Items {
			cell, ok := value.(atom.String)
			if !ok || !utf8.ValidString(string(cell)) {
				return "", argumentVMError(
					function,
					"cell",
					"valid UTF-8 string",
					value,
					function+": row "+strconv.Itoa(rowIndex)+" cell "+strconv.Itoa(cellIndex)+" must be a valid UTF-8 string",
				)
			}
			record[cellIndex] = string(cell)
		}
		records[rowIndex] = record
	}

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	for _, record := range records {
		if len(record) == 1 && record[0] == "" {
			writer.Flush()
			if err := writer.Error(); err != nil {
				return "", runtimeVMError(function, function+": failed to encode CSV")
			}
			output.WriteString("\"\"\n")
			continue
		}
		if err := writer.Write(record); err != nil {
			return "", runtimeVMError(function, function+": failed to encode CSV")
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", runtimeVMError(function, function+": failed to encode CSV")
	}
	return output.String(), nil
}
