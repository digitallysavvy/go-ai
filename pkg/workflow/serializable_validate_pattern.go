package workflow

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// JavaScript `\s` (WhiteSpace and LineTerminator, the set
// providerutils.IsJSWhiteSpace reports) as RE2 class members, and its
// complement. RE2's `\s` is only [\t\n\f\r ].
const (
	jsPatternSpace    = providerutils.JSRegexpSpaceChars
	jsPatternNonSpace = `\x00-\x08\x0e-\x1f\x21-\x9f\x{a1}-\x{167f}\x{1681}-\x{1fff}\x{200b}-\x{2027}\x{202a}-\x{202e}\x{2030}-\x{205e}\x{2060}-\x{2fff}\x{3001}-\x{fefe}\x{ff00}-\x{10ffff}`
	// JS `.` without the s flag excludes the four line terminators; RE2's
	// excludes only \n.
	jsPatternDot = providerutils.JSRegexpDot
)

// compileAjvPattern compiles a JSON Schema `pattern` / `patternProperties`
// key the way Ajv does (`new RegExp(pattern, "u")`), as far as RE2 can: `\s`,
// `\S` and `.` get their JavaScript sets, `\uXXXX` (also a surrogate pair)
// and `\u{...}` become code points, and `[]` / `[^]` match nothing /
// anything. Constructs RE2 lacks (lookaround, backreferences) still fail to
// compile, and the caller skips the keyword.
func compileAjvPattern(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(ajvPatternToRE2(pattern))
}

func ajvPatternToRE2(p string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(p); {
		c := p[i]
		switch {
		case c == '\\' && i+1 < len(p):
			e := p[i+1]
			switch e {
			case 's':
				if inClass {
					b.WriteString(jsPatternSpace)
				} else {
					b.WriteString("[" + jsPatternSpace + "]")
				}
				i += 2
				continue
			case 'S':
				if inClass {
					b.WriteString(jsPatternNonSpace)
				} else {
					b.WriteString("[" + jsPatternNonSpace + "]")
				}
				i += 2
				continue
			case 'u':
				if r, n, ok := jsUnicodeEscape(p[i:]); ok {
					b.WriteString(`\x{` + strconv.FormatInt(int64(r), 16) + `}`)
					i += n
					continue
				}
			}
			_, size := utf8.DecodeRuneInString(p[i+1:])
			b.WriteString(p[i : i+1+size])
			i += 1 + size
			continue
		case inClass:
			if c == ']' {
				inClass = false
			}
		case c == '[':
			if strings.HasPrefix(p[i:], "[]") {
				b.WriteString(`[^\x00-\x{10ffff}]`)
				i += 2
				continue
			}
			if strings.HasPrefix(p[i:], "[^]") {
				b.WriteString(`[\x00-\x{10ffff}]`)
				i += 3
				continue
			}
			inClass = true
			b.WriteByte(c)
			if strings.HasPrefix(p[i+1:], "^") {
				b.WriteByte('^')
				i++
			}
			i++
			continue
		case c == '.':
			b.WriteString(jsPatternDot)
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// jsUnicodeEscape decodes a u-mode `\uXXXX` (joining a high and a low
// surrogate escape into one code point) or `\u{X...}` at the start of s.
func jsUnicodeEscape(s string) (rune, int, bool) {
	if strings.HasPrefix(s, `\u{`) {
		end := strings.IndexByte(s, '}')
		if end < 4 {
			return 0, 0, false
		}
		v, err := strconv.ParseUint(s[3:end], 16, 32)
		if err != nil || v > 0x10ffff {
			return 0, 0, false
		}
		return rune(v), end + 1, true
	}
	hex4 := func(s string) (rune, bool) {
		if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
			return 0, false
		}
		v, err := strconv.ParseUint(s[2:6], 16, 32)
		return rune(v), err == nil
	}
	hi, ok := hex4(s)
	if !ok {
		return 0, 0, false
	}
	if hi >= 0xd800 && hi <= 0xdbff {
		if lo, ok := hex4(s[6:]); ok && lo >= 0xdc00 && lo <= 0xdfff {
			return 0x10000 + (hi-0xd800)<<10 + (lo - 0xdc00), 12, true
		}
	}
	return hi, 6, true
}
