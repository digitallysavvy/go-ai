package githubcopilot

import (
	"encoding/json"
	"strings"
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
	text := stripJSONCComments(string(data))
	text = stripTrailingCommas(text)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func stripJSONCComments(text string) string {
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
			for i < n && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && text[i+1] == '*':
			i += 2
			for i+1 < n && (text[i] != '*' || text[i+1] != '/') {
				i++
			}
			i += 2
			if i > n {
				i = n
			}
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
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
