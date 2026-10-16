package providerutils

import "strings"

// IsJSWhiteSpace reports whether r is stripped by JavaScript's
// String.prototype.trim (WhiteSpace and LineTerminator in ECMA-262). It
// differs from unicode.IsSpace: U+FEFF is trimmed by JS but not by Go, and
// U+0085 is trimmed by Go but not by JS.
func IsJSWhiteSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// JSTrim is JavaScript's String.prototype.trim.
func JSTrim(s string) string {
	return strings.TrimFunc(s, IsJSWhiteSpace)
}

// JSTrimStart is JavaScript's String.prototype.trimStart.
func JSTrimStart(s string) string {
	return strings.TrimLeftFunc(s, IsJSWhiteSpace)
}

// JSTrimEnd is JavaScript's String.prototype.trimEnd.
func JSTrimEnd(s string) string {
	return strings.TrimRightFunc(s, IsJSWhiteSpace)
}
