// Package jscase is JavaScript's String.prototype.toLowerCase and
// toUpperCase (Unicode default case conversion with the unconditional
// SpecialCasing mappings and the Final_Sigma context), which Go's
// strings.ToLower / ToUpper (simple per-rune mappings) do not match: U+0130
// lowercases to "i̇", a word-final capital sigma to final sigma, U+00DF
// uppercases to "SS", ligatures expand, and Go's Unicode tables lag Node's.
//
// The exception tables are generated from Node 24 (Unicode 17, ICU 78.3)
// over every code point, keeping only those where the JS result differs
// from Go's unicode.ToLower / unicode.ToUpper. providerutils.JSToLower and
// JSToUpper export it; this package exists so provider/errors, which
// providerutils imports, can use it too.
package jscase

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ToLower is JavaScript's String.prototype.toLowerCase.
func ToLower(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		if r == 0x03A3 {
			if finalSigma(s, i) {
				b.WriteRune(0x03C2)
			} else {
				b.WriteRune(0x03C3)
			}
			continue
		}
		if m, ok := jsLowerSpecial[r]; ok {
			b.WriteString(m)
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// ToUpper is JavaScript's String.prototype.toUpperCase.
func ToUpper(s string) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if m, ok := jsUpperSpecial[r]; ok {
			b.WriteString(m)
			continue
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// finalSigma is the Unicode Final_Sigma condition for the capital sigma at
// byte offset i: a cased letter before it (skipping case-ignorable
// characters) and none after it.
func finalSigma(s string, i int) bool {
	before := false
	for j := i; j > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:j])
		j -= size
		if caseIgnorable(r) {
			continue
		}
		before = cased(r)
		break
	}
	if !before {
		return false
	}
	for j := i + utf8.RuneLen(0x03A3); j < len(s); {
		r, size := utf8.DecodeRuneInString(s[j:])
		j += size
		if caseIgnorable(r) {
			continue
		}
		return !cased(r)
	}
	return true
}

// cased is the Unicode Cased property: Lowercase, Uppercase or Lt.
func cased(r rune) bool {
	return unicode.In(r, unicode.Ll, unicode.Lu, unicode.Lt, unicode.Other_Lowercase, unicode.Other_Uppercase)
}

// caseIgnorable is the Unicode Case_Ignorable property: Mn, Me, Cf, Lm, Sk
// and the Word_Break MidLetter, MidNumLet and Single_Quote characters.
func caseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', 0x00B7, 0x0387, 0x055F, 0x05F4, 0x2018, 0x2019, 0x2024, 0x2027, 0xFE13, 0xFE52, 0xFE55, 0xFF07, 0xFF0E, 0xFF1A:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// jsLowerSpecial: 56 code points
var jsLowerSpecial = map[rune]string{
	0x0130:  "i\u0307",
	0x1C89:  "\u1c8a",
	0xA7CB:  "\u0264",
	0xA7CC:  "\ua7cd",
	0xA7CE:  "\ua7cf",
	0xA7D2:  "\ua7d3",
	0xA7D4:  "\ua7d5",
	0xA7DA:  "\ua7db",
	0xA7DC:  "\u019b",
	0x10D50: "\U00010d70",
	0x10D51: "\U00010d71",
	0x10D52: "\U00010d72",
	0x10D53: "\U00010d73",
	0x10D54: "\U00010d74",
	0x10D55: "\U00010d75",
	0x10D56: "\U00010d76",
	0x10D57: "\U00010d77",
	0x10D58: "\U00010d78",
	0x10D59: "\U00010d79",
	0x10D5A: "\U00010d7a",
	0x10D5B: "\U00010d7b",
	0x10D5C: "\U00010d7c",
	0x10D5D: "\U00010d7d",
	0x10D5E: "\U00010d7e",
	0x10D5F: "\U00010d7f",
	0x10D60: "\U00010d80",
	0x10D61: "\U00010d81",
	0x10D62: "\U00010d82",
	0x10D63: "\U00010d83",
	0x10D64: "\U00010d84",
	0x10D65: "\U00010d85",
	0x16EA0: "\U00016ebb",
	0x16EA1: "\U00016ebc",
	0x16EA2: "\U00016ebd",
	0x16EA3: "\U00016ebe",
	0x16EA4: "\U00016ebf",
	0x16EA5: "\U00016ec0",
	0x16EA6: "\U00016ec1",
	0x16EA7: "\U00016ec2",
	0x16EA8: "\U00016ec3",
	0x16EA9: "\U00016ec4",
	0x16EAA: "\U00016ec5",
	0x16EAB: "\U00016ec6",
	0x16EAC: "\U00016ec7",
	0x16EAD: "\U00016ec8",
	0x16EAE: "\U00016ec9",
	0x16EAF: "\U00016eca",
	0x16EB0: "\U00016ecb",
	0x16EB1: "\U00016ecc",
	0x16EB2: "\U00016ecd",
	0x16EB3: "\U00016ece",
	0x16EB4: "\U00016ecf",
	0x16EB5: "\U00016ed0",
	0x16EB6: "\U00016ed1",
	0x16EB7: "\U00016ed2",
	0x16EB8: "\U00016ed3",
}

// jsUpperSpecial: 157 code points
var jsUpperSpecial = map[rune]string{
	0x00DF:  "SS",
	0x0149:  "\u02bcN",
	0x019B:  "\ua7dc",
	0x01F0:  "J\u030c",
	0x0264:  "\ua7cb",
	0x0390:  "\u0399\u0308\u0301",
	0x03B0:  "\u03a5\u0308\u0301",
	0x0587:  "\u0535\u0552",
	0x1C8A:  "\u1c89",
	0x1E96:  "H\u0331",
	0x1E97:  "T\u0308",
	0x1E98:  "W\u030a",
	0x1E99:  "Y\u030a",
	0x1E9A:  "A\u02be",
	0x1F50:  "\u03a5\u0313",
	0x1F52:  "\u03a5\u0313\u0300",
	0x1F54:  "\u03a5\u0313\u0301",
	0x1F56:  "\u03a5\u0313\u0342",
	0x1F80:  "\u1f08\u0399",
	0x1F81:  "\u1f09\u0399",
	0x1F82:  "\u1f0a\u0399",
	0x1F83:  "\u1f0b\u0399",
	0x1F84:  "\u1f0c\u0399",
	0x1F85:  "\u1f0d\u0399",
	0x1F86:  "\u1f0e\u0399",
	0x1F87:  "\u1f0f\u0399",
	0x1F88:  "\u1f08\u0399",
	0x1F89:  "\u1f09\u0399",
	0x1F8A:  "\u1f0a\u0399",
	0x1F8B:  "\u1f0b\u0399",
	0x1F8C:  "\u1f0c\u0399",
	0x1F8D:  "\u1f0d\u0399",
	0x1F8E:  "\u1f0e\u0399",
	0x1F8F:  "\u1f0f\u0399",
	0x1F90:  "\u1f28\u0399",
	0x1F91:  "\u1f29\u0399",
	0x1F92:  "\u1f2a\u0399",
	0x1F93:  "\u1f2b\u0399",
	0x1F94:  "\u1f2c\u0399",
	0x1F95:  "\u1f2d\u0399",
	0x1F96:  "\u1f2e\u0399",
	0x1F97:  "\u1f2f\u0399",
	0x1F98:  "\u1f28\u0399",
	0x1F99:  "\u1f29\u0399",
	0x1F9A:  "\u1f2a\u0399",
	0x1F9B:  "\u1f2b\u0399",
	0x1F9C:  "\u1f2c\u0399",
	0x1F9D:  "\u1f2d\u0399",
	0x1F9E:  "\u1f2e\u0399",
	0x1F9F:  "\u1f2f\u0399",
	0x1FA0:  "\u1f68\u0399",
	0x1FA1:  "\u1f69\u0399",
	0x1FA2:  "\u1f6a\u0399",
	0x1FA3:  "\u1f6b\u0399",
	0x1FA4:  "\u1f6c\u0399",
	0x1FA5:  "\u1f6d\u0399",
	0x1FA6:  "\u1f6e\u0399",
	0x1FA7:  "\u1f6f\u0399",
	0x1FA8:  "\u1f68\u0399",
	0x1FA9:  "\u1f69\u0399",
	0x1FAA:  "\u1f6a\u0399",
	0x1FAB:  "\u1f6b\u0399",
	0x1FAC:  "\u1f6c\u0399",
	0x1FAD:  "\u1f6d\u0399",
	0x1FAE:  "\u1f6e\u0399",
	0x1FAF:  "\u1f6f\u0399",
	0x1FB2:  "\u1fba\u0399",
	0x1FB3:  "\u0391\u0399",
	0x1FB4:  "\u0386\u0399",
	0x1FB6:  "\u0391\u0342",
	0x1FB7:  "\u0391\u0342\u0399",
	0x1FBC:  "\u0391\u0399",
	0x1FC2:  "\u1fca\u0399",
	0x1FC3:  "\u0397\u0399",
	0x1FC4:  "\u0389\u0399",
	0x1FC6:  "\u0397\u0342",
	0x1FC7:  "\u0397\u0342\u0399",
	0x1FCC:  "\u0397\u0399",
	0x1FD2:  "\u0399\u0308\u0300",
	0x1FD3:  "\u0399\u0308\u0301",
	0x1FD6:  "\u0399\u0342",
	0x1FD7:  "\u0399\u0308\u0342",
	0x1FE2:  "\u03a5\u0308\u0300",
	0x1FE3:  "\u03a5\u0308\u0301",
	0x1FE4:  "\u03a1\u0313",
	0x1FE6:  "\u03a5\u0342",
	0x1FE7:  "\u03a5\u0308\u0342",
	0x1FF2:  "\u1ffa\u0399",
	0x1FF3:  "\u03a9\u0399",
	0x1FF4:  "\u038f\u0399",
	0x1FF6:  "\u03a9\u0342",
	0x1FF7:  "\u03a9\u0342\u0399",
	0x1FFC:  "\u03a9\u0399",
	0xA7CD:  "\ua7cc",
	0xA7CF:  "\ua7ce",
	0xA7D3:  "\ua7d2",
	0xA7D5:  "\ua7d4",
	0xA7DB:  "\ua7da",
	0xFB00:  "FF",
	0xFB01:  "FI",
	0xFB02:  "FL",
	0xFB03:  "FFI",
	0xFB04:  "FFL",
	0xFB05:  "ST",
	0xFB06:  "ST",
	0xFB13:  "\u0544\u0546",
	0xFB14:  "\u0544\u0535",
	0xFB15:  "\u0544\u053b",
	0xFB16:  "\u054e\u0546",
	0xFB17:  "\u0544\u053d",
	0x10D70: "\U00010d50",
	0x10D71: "\U00010d51",
	0x10D72: "\U00010d52",
	0x10D73: "\U00010d53",
	0x10D74: "\U00010d54",
	0x10D75: "\U00010d55",
	0x10D76: "\U00010d56",
	0x10D77: "\U00010d57",
	0x10D78: "\U00010d58",
	0x10D79: "\U00010d59",
	0x10D7A: "\U00010d5a",
	0x10D7B: "\U00010d5b",
	0x10D7C: "\U00010d5c",
	0x10D7D: "\U00010d5d",
	0x10D7E: "\U00010d5e",
	0x10D7F: "\U00010d5f",
	0x10D80: "\U00010d60",
	0x10D81: "\U00010d61",
	0x10D82: "\U00010d62",
	0x10D83: "\U00010d63",
	0x10D84: "\U00010d64",
	0x10D85: "\U00010d65",
	0x16EBB: "\U00016ea0",
	0x16EBC: "\U00016ea1",
	0x16EBD: "\U00016ea2",
	0x16EBE: "\U00016ea3",
	0x16EBF: "\U00016ea4",
	0x16EC0: "\U00016ea5",
	0x16EC1: "\U00016ea6",
	0x16EC2: "\U00016ea7",
	0x16EC3: "\U00016ea8",
	0x16EC4: "\U00016ea9",
	0x16EC5: "\U00016eaa",
	0x16EC6: "\U00016eab",
	0x16EC7: "\U00016eac",
	0x16EC8: "\U00016ead",
	0x16EC9: "\U00016eae",
	0x16ECA: "\U00016eaf",
	0x16ECB: "\U00016eb0",
	0x16ECC: "\U00016eb1",
	0x16ECD: "\U00016eb2",
	0x16ECE: "\U00016eb3",
	0x16ECF: "\U00016eb4",
	0x16ED0: "\U00016eb5",
	0x16ED1: "\U00016eb6",
	0x16ED2: "\U00016eb7",
	0x16ED3: "\U00016eb8",
}
