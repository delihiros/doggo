package vm

import "unicode/utf8"

// xmlValidName implements XML 1.0 Fifth Edition productions [4], [4a], and [5]:
// https://www.w3.org/TR/xml/#NT-Name
// It checks lexical names only, preserving prefixes without resolving namespaces.
func xmlValidName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for i, r := range name {
		if xmlNameStartChar(r) {
			continue
		}
		if i > 0 && (r == '-' || r == '.' || r >= '0' && r <= '9' ||
			r == 0xB7 || r >= 0x300 && r <= 0x36F || r >= 0x203F && r <= 0x2040) {
			continue
		}
		return false
	}
	return true
}

func xmlNameStartChar(r rune) bool {
	return r == ':' || r >= 'A' && r <= 'Z' || r == '_' || r >= 'a' && r <= 'z' ||
		r >= 0xC0 && r <= 0xD6 ||
		r >= 0xD8 && r <= 0xF6 ||
		r >= 0xF8 && r <= 0x2FF ||
		r >= 0x370 && r <= 0x37D ||
		r >= 0x37F && r <= 0x1FFF ||
		r >= 0x200C && r <= 0x200D ||
		r >= 0x2070 && r <= 0x218F ||
		r >= 0x2C00 && r <= 0x2FEF ||
		r >= 0x3001 && r <= 0xD7FF ||
		r >= 0xF900 && r <= 0xFDCF ||
		r >= 0xFDF0 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0xEFFFF
}
