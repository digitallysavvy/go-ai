package http

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
)

// FormEscape is the application/x-www-form-urlencoded serializer
// URLSearchParams uses for one name or value: ASCII alphanumerics and "*-._"
// stay literal, space is "+", everything else (including "~") is
// percent-encoded as UTF-8; invalid UTF-8 is U+FFFD.
func FormEscape(s string) string {
	var b strings.Builder
	formEscape(&b, s)
	return b.String()
}

// SearchParams is an ordered list of name/value pairs with the semantics of
// the WHATWG URLSearchParams the TS SDK uses (url.searchParams), unlike
// net/url.Values, which sorts its keys and escapes differently.
type SearchParams struct{ pairs [][2]string }

// ParseSearchParams parses raw (a query without "?") as
// application/x-www-form-urlencoded, like new URLSearchParams(raw): pairs
// split on "&", "+" is a space, percent escapes are decoded leniently (an
// invalid escape stays as written) and the bytes read as UTF-8 with U+FFFD
// replacement.
func ParseSearchParams(raw string) *SearchParams {
	p := &SearchParams{}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		p.pairs = append(p.pairs, [2]string{formDecode(name), formDecode(value)})
	}
	return p
}

// Set is URLSearchParams.set: the first pair named name takes value (later
// ones are dropped), or the pair is appended.
func (p *SearchParams) Set(name, value string) {
	out := p.pairs[:0:0]
	found := false
	for _, kv := range p.pairs {
		if kv[0] != name {
			out = append(out, kv)
			continue
		}
		if !found {
			out = append(out, [2]string{name, value})
			found = true
		}
	}
	if !found {
		out = append(out, [2]string{name, value})
	}
	p.pairs = out
}

// NewSearchParams returns an empty ordered parameter list (new
// URLSearchParams()).
func NewSearchParams() *SearchParams { return &SearchParams{} }

// Len is the number of pairs; a nil list has none.
func (p *SearchParams) Len() int {
	if p == nil {
		return 0
	}
	return len(p.pairs)
}

// Append is URLSearchParams.append.
func (p *SearchParams) Append(name, value string) {
	p.pairs = append(p.pairs, [2]string{name, value})
}

// Delete is URLSearchParams.delete(name).
func (p *SearchParams) Delete(name string) {
	out := p.pairs[:0:0]
	for _, kv := range p.pairs {
		if kv[0] != name {
			out = append(out, kv)
		}
	}
	p.pairs = out
}

// Encode is URLSearchParams.toString(): the pairs in order, FormEscape'd.
func (p *SearchParams) Encode() string {
	var b strings.Builder
	for i, kv := range p.pairs {
		if i > 0 {
			b.WriteByte('&')
		}
		formEscape(&b, kv[0])
		b.WriteByte('=')
		formEscape(&b, kv[1])
	}
	return b.String()
}

// SetSearchParam is TS url.searchParams.set(name, value) on a URL whose query
// is rawQuery: the whole query is re-serialized (new URLSearchParams order
// and escaping) and returned without "?".
func SetSearchParam(rawQuery, name, value string) string {
	p := ParseSearchParams(rawQuery)
	p.Set(name, value)
	return p.Encode()
}

// formDecode is the WHATWG form-urlencoded value decode: "+" to space, then
// percent-decode (an escape without two hex digits stays literal), then
// UTF-8 decode without BOM, invalid sequences as U+FFFD.
func formDecode(s string) string {
	if !strings.ContainsAny(s, "+%") {
		return textdecode.ReplaceInvalidString(s)
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '+':
			b = append(b, ' ')
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b = append(b, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
		default:
			b = append(b, c)
		}
	}
	return string(textdecode.ReplaceInvalid(b))
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}
