package vm

import "unicode/utf8"

// xmlValidChars implements the XML 1.0 Fifth Edition Char production [2]:
// https://www.w3.org/TR/xml/#charsets
func xmlValidChars(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == '\t' || r == '\n' || r == '\r' ||
			r >= 0x20 && r <= 0xD7FF ||
			r >= 0xE000 && r <= 0xFFFD ||
			r >= 0x10000 && r <= 0x10FFFF {
			continue
		}
		return false
	}
	return true
}
