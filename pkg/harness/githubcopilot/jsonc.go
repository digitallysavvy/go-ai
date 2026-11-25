package githubcopilot

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// ParseJSONC parses a JSONC (JSON with Comments) document: `//` line
// comments, `/* */` block comments, and trailing commas before a closing `}`
// or `]` are stripped (all outside string literals) before standard JSON
// decoding. Mirrors the `jsonc-parser` `parse(text, errors, {
// allowTrailingComma: true })` call in TS `readGitHubCopilotConfig`; unlike
// the TS helper (which tolerates and reports individual errors), this
// returns the first parse error, matching how the caller already discards
// the config on any error.
func ParseJSONC(data []byte) (map[string]any, error) {
	text, err := stripJSONCComments(string(data))
	if err != nil {
		return nil, err
	}
	text = stripTrailingCommas(text)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// isJSONCWhiteSpace reports the characters jsonc-parser's scanner treats as
// whitespace outside strings (its isWhiteSpace): beyond JSON's own four, the
// vertical tab, form feed, no-break and Unicode spaces, and the byte order
// mark. encoding/json rejects them, so they are normalized to a plain space.
func isJSONCWhiteSpace(r rune) bool {
	switch r {
	case '\v', '\f', 0x00a0, 0x1680, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// errUnterminatedBlockComment is jsonc-parser's UnexpectedEndOfComment.
var errUnterminatedBlockComment = errors.New("jsonc: unexpected end of comment")

func stripJSONCComments(text string) (string, error) {
	var out strings.Builder
	inString := false
	escaped := false
	n := len(text)
	for i := 0; i < n; {
		c := text[i]
		if inString {
			out.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			i++
			continue
		}
		switch {
		case c == '"':
			inString = true
			out.WriteByte(c)
			i++
		case c == '/' && i+1 < n && text[i+1] == '/':
			i += 2
			// A line comment ends at \n or a lone \r (jsonc-parser isLineBreak).
			for i < n && text[i] != '\n' && text[i] != '\r' {
				i++
			}
		case c == '/' && i+1 < n && text[i+1] == '*':
			i += 2
			for i+1 < n && (text[i] != '*' || text[i+1] != '/') {
				i++
			}
			if i+1 >= n {
				return "", errUnterminatedBlockComment
			}
			i += 2
		default:
			if c >= utf8.RuneSelf || c == '\v' || c == '\f' {
				if r, size := utf8.DecodeRuneInString(text[i:]); isJSONCWhiteSpace(r) {
					out.WriteByte(' ')
					i += size
					continue
				}
			}
			out.WriteByte(c)
			i++
		}
	}
	return out.String(), nil
}

func stripTrailingCommas(text string) string {
	var out strings.Builder
	inString := false
	escaped := false
	n := len(text)
	for i := 0; i < n; i++ {
		c := text[i]
		if inString {
			out.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < n && isJSONWhitespace(text[j]) {
				j++
			}
			if j < n && (text[j] == '}' || text[j] == ']') {
				continue // drop the trailing comma
			}
		}
		out.WriteByte(c)
	}
	return out.String()
}

func isJSONWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
