package providerutils

import (
	"strings"

	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// ExtractLines returns the 1-based inclusive line range [startLine, endLine]
// of text, mirroring TS `extractLines` in @ai-sdk/provider-utils: the line
// ending is detected (CRLF, then LF, then CR, default LF), nil bounds default
// to the whole text, and the slice follows JavaScript `Array.prototype.slice`
// semantics (a negative end counts from the last line).
func ExtractLines(text string, startLine, endLine *int) string {
	if startLine == nil && endLine == nil {
		return text
	}
	lineEnding := "\n"
	switch {
	case strings.Contains(text, "\r\n"):
		lineEnding = "\r\n"
	case strings.Contains(text, "\n"):
		lineEnding = "\n"
	case strings.Contains(text, "\r"):
		lineEnding = "\r"
	}
	lines := strings.Split(text, lineEnding)
	n := len(lines)
	start := 1
	if startLine != nil {
		start = *startLine
	}
	if start < 1 {
		start = 1
	}
	start--
	end := n
	if endLine != nil && *endLine < n {
		end = *endLine
	}
	if end < 0 {
		end += n
		if end < 0 {
			end = 0
		}
	}
	if start >= end {
		return ""
	}
	return strings.Join(lines[start:end], lineEnding)
}

// DecodeSandboxText decodes bytes using a WHATWG encoding label ("" and
// utf-8 are passthrough), mirroring TS `Buffer.toString(encoding)` in the
// sandbox sessions' readTextFile.
func DecodeSandboxText(data []byte, encodingLabel string) (string, error) {
	if encodingLabel == "" || strings.EqualFold(encodingLabel, "utf-8") || strings.EqualFold(encodingLabel, "utf8") {
		return string(data), nil
	}
	enc, err := htmlindex.Get(encodingLabel)
	if err != nil {
		return "", err
	}
	decoded, _, err := transform.Bytes(enc.NewDecoder(), data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// EncodeSandboxText is the inverse of DecodeSandboxText, mirroring TS
// `Buffer.from(content, encoding)` in writeTextFile.
func EncodeSandboxText(text, encodingLabel string) ([]byte, error) {
	if encodingLabel == "" || strings.EqualFold(encodingLabel, "utf-8") || strings.EqualFold(encodingLabel, "utf8") {
		return []byte(text), nil
	}
	enc, err := htmlindex.Get(encodingLabel)
	if err != nil {
		return nil, err
	}
	encoded, _, err := transform.Bytes(enc.NewEncoder(), []byte(text))
	return encoded, err
}
