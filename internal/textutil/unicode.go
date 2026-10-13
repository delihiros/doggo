package textutil

import (
	"strings"
	"unicode/utf8"
)

func CodePointLen(s string) int {
	return utf8.RuneCountInString(s)
}

func CodePointSlice(s string, start, end int) (string, bool) {
	runes := []rune(s)
	if start < 0 || end < start || end > len(runes) {
		return "", false
	}
	return string(runes[start:end]), true
}

func CodePointAt(s string, index int) (rune, bool) {
	runes := []rune(s)
	if index < 0 || index >= len(runes) {
		return 0, false
	}
	return runes[index], true
}

func CodePointIndex(s, substr string) int {
	byteIndex := strings.Index(s, substr)
	if byteIndex < 0 {
		return -1
	}
	return utf8.RuneCountInString(s[:byteIndex])
}

func CodePointLastIndex(s, substr string) int {
	byteIndex := strings.LastIndex(s, substr)
	if byteIndex < 0 {
		return -1
	}
	return utf8.RuneCountInString(s[:byteIndex])
}
