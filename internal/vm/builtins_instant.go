package vm

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var instantTimestampPattern = regexp.MustCompile(`^(\d{4})(?:-(\d{2})(?:-(\d{2})(?:T(\d{2})(?::(\d{2})(?::(\d{2})(?:\.(\d+))?)?)?(?:([+-])(\d{2}):(\d{2})|Z)?)?)?)?$`)
var instantMapTimestampPattern = regexp.MustCompile(`^(-?\d{4,})(?:-(\d{2})(?:-(\d{2})(?:T(\d{2})(?::(\d{2})(?::(\d{2})(?:\.(\d+))?)?)?(?:([+-])(\d{2}):(\d{2})|Z)?)?)?)?$`)

const instantMaxRFC3339OffsetSeconds = 23*60*60 + 59*60

func instantInvalidTimestamp(input string) error {
	return fmt.Errorf("parse-timestamp: invalid timestamp %q", input)
}

func instantParseComponent(input string, fallback int) (int, error) {
	if input == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(input)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func instantZeroFillRight(input string, width int) string {
	if len(input) == width {
		return input
	}
	if len(input) > width {
		return input[:width]
	}
	for len(input) < width {
		input += "0"
	}
	return input
}

func instantDaysInMonth(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func parseTimestampValue(input string) (time.Time, int, error) {
	return parseTimestampComponents(input, instantTimestampPattern)
}

func parseInstantTimestampValue(input string) (time.Time, int, error) {
	return parseTimestampComponents(input, instantMapTimestampPattern)
}

func parseTimestampComponents(input string, pattern *regexp.Regexp) (time.Time, int, error) {
	matches := pattern.FindStringSubmatch(input)
	if matches == nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}

	year, err := instantParseComponent(matches[1], 0)
	if err != nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	month, err := instantParseComponent(matches[2], 1)
	if err != nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	day, err := instantParseComponent(matches[3], 1)
	if err != nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	hour, err := instantParseComponent(matches[4], 0)
	if err != nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	minute, err := instantParseComponent(matches[5], 0)
	if err != nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	second, err := instantParseComponent(matches[6], 0)
	if err != nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	nano, err := instantParseComponent(instantZeroFillRight(matches[7], 9), 0)
	if err != nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}

	offsetSign := 0
	switch matches[8] {
	case "-":
		offsetSign = -1
	case "+":
		offsetSign = 1
	}
	offsetHours, err := instantParseComponent(matches[9], 0)
	if err != nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	offsetMinutes, err := instantParseComponent(matches[10], 0)
	if err != nil {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}

	if month < 1 || month > 12 {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	if day < 1 || day > instantDaysInMonth(year, month) {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	if hour < 0 || hour > 23 {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	if minute < 0 || minute > 59 {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	maxSecond := 59
	if minute == 59 {
		maxSecond = 60
	}
	if second < 0 || second > maxSecond {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	if nano < 0 || nano > 999999999 {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}
	if offsetHours < 0 || offsetHours > 23 || offsetMinutes < 0 || offsetMinutes > 59 {
		return time.Time{}, 0, instantInvalidTimestamp(input)
	}

	offsetSeconds := offsetSign * ((offsetHours * 60 * 60) + (offsetMinutes * 60))
	location := time.FixedZone("", offsetSeconds)
	parsed := time.Date(year, time.Month(month), day, hour, minute, second, nano, location)
	return parsed.UTC(), offsetSeconds, nil
}

func instantResultFromTime(parsed time.Time, offsetSeconds int) types.Value {
	utc := parsed.UTC()
	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("instant"), atom.NewString(utc.Format(time.RFC3339Nano)),
		atom.NewKeyword("epoch-millis"), atom.NewNumberInt(utc.UnixMilli()),
		atom.NewKeyword("offset-seconds"), atom.NewNumberInt(int64(offsetSeconds)),
	})
}

func parseTimestampResult(function, input string) (types.Value, error) {
	parsed, offsetSeconds, err := parseTimestampValue(input)
	if err != nil {
		return nil, argumentVMError(function, "input", "ISO 8601 timestamp string", atom.NewString(input), err.Error())
	}

	return instantResultFromTime(parsed, offsetSeconds), nil
}

func instantValueFromArg(function string, arg types.Value) (string, int64, error) {
	value, err := timeValidateInstantArg(function, arg)
	if err != nil {
		return "", 0, argumentVMError(function, "instant", "instant value", arg, fmt.Sprintf("%s: expected instant value, got %s", function, arg.Type()))
	}

	instantString, ok := value.Get(timeKeywordInstant).(atom.String)
	if !ok {
		return "", 0, argumentVMError(function, "instant", "instant value", arg, fmt.Sprintf("%s: expected instant value, got %s", function, arg.Type()))
	}
	offsetNumber, ok := value.Get(timeKeywordOffset).(types.Number)
	offsetValue, isInteger := types.Int64Value(offsetNumber)
	if !ok || !isInteger {
		return "", 0, argumentVMError(function, "instant", "instant value", arg, fmt.Sprintf("%s: expected instant value, got %s", function, arg.Type()))
	}

	return string(instantString), offsetValue, nil
}

func instantRFC3339OffsetSeconds(function string, offsetSeconds int64) (int, error) {
	if offsetSeconds < -instantMaxRFC3339OffsetSeconds || offsetSeconds > instantMaxRFC3339OffsetSeconds || offsetSeconds%60 != 0 {
		return 0, argumentVMError(function, "offset-seconds", "whole minutes within RFC3339 range", atom.NewNumberInt(offsetSeconds), fmt.Sprintf("%s: offset-seconds must be whole minutes within RFC3339 range, got %d", function, offsetSeconds))
	}
	return int(offsetSeconds), nil
}

func formatTimestampResult(arg types.Value) (types.Value, error) {
	const function = "doggo.time/format"
	instantString, offsetSeconds, err := instantValueFromArg(function, arg)
	if err != nil {
		return nil, err
	}
	validatedOffsetSeconds, err := instantRFC3339OffsetSeconds(function, offsetSeconds)
	if err != nil {
		return nil, err
	}

	parsed, _, parseErr := parseInstantTimestampValue(instantString)
	if parseErr != nil {
		return nil, argumentVMError(function, "instant", "valid instant value", arg, parseErr.Error())
	}
	if offsetSeconds == 0 {
		return atom.NewString(parsed.UTC().Format(time.RFC3339Nano)), nil
	}

	location := time.FixedZone("", validatedOffsetSeconds)
	return atom.NewString(parsed.In(location).Format(time.RFC3339Nano)), nil
}

var builtinsInstant = []BuiltinDefinition{
	{
		Name:       "parse-timestamp*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("parse-timestamp*", args, 1); err != nil {
				return nil, err
			}
			input, ok := args[0].(atom.String)
			if !ok {
				return nil, argumentVMError("doggo.instant/parse-timestamp", "input", "string", args[0], fmt.Sprintf("parse-timestamp: expected string, got %s", args[0].Type()))
			}
			return parseTimestampResult("doggo.instant/parse-timestamp", string(input))
		}},
	},
	{
		Name:       "time-format*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("time-format*", args, 1); err != nil {
				return nil, err
			}
			return formatTimestampResult(args[0])
		}},
	},
}
