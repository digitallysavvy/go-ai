// Package textdecode is the WHATWG UTF-8 decode (TS TextDecoder,
// Response.text()) shared by packages that cannot import providerutils/prompt.
package textdecode

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

var bom = []byte{0xEF, 0xBB, 0xBF}

// UTF8 is TS new TextDecoder().decode(bytes) (not fatal): a leading BOM is
// dropped and every maximal invalid subpart becomes one U+FFFD.
func UTF8(b []byte) string {
	b = bytes.TrimPrefix(b, bom)
	if utf8.Valid(b) {
		return string(b)
	}
	return replaceInvalid(b)
}

// ReplaceInvalid is the WHATWG replacement for text that is not at the start
// of a stream (no BOM handling): valid input is returned as is, and every
// maximal invalid subpart becomes one U+FFFD.
func ReplaceInvalid(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	return []byte(replaceInvalid(b))
}

// ReplaceInvalidString is ReplaceInvalid for a string.
func ReplaceInvalidString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return replaceInvalid([]byte(s))
}

// HasBOM reports whether b starts with the UTF-8 byte order mark.
func HasBOM(b []byte) bool { return bytes.HasPrefix(b, bom) }

func replaceInvalid(b []byte) string {
	var out strings.Builder
	out.Grow(len(b))
	for i := 0; i < len(b); {
		if r, size := utf8.DecodeRune(b[i:]); r != utf8.RuneError || size > 1 {
			out.Write(b[i : i+size])
			i += size
			continue
		}
		lead := b[i]
		need, lower, upper := 0, byte(0x80), byte(0xBF)
		switch {
		case lead >= 0xC2 && lead <= 0xDF:
			need = 1
		case lead >= 0xE0 && lead <= 0xEF:
			need = 2
			switch lead {
			case 0xE0:
				lower = 0xA0
			case 0xED:
				upper = 0x9F
			}
		case lead >= 0xF0 && lead <= 0xF4:
			need = 3
			switch lead {
			case 0xF0:
				lower = 0x90
			case 0xF4:
				upper = 0x8F
			}
		}
		i++
		for ; need > 0 && i < len(b) && b[i] >= lower && b[i] <= upper; need-- {
			i++
			lower, upper = 0x80, 0xBF
		}
		out.WriteRune(utf8.RuneError)
	}
	return out.String()
}
