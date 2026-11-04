package providerutils

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
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

// nodeBufferEncoding resolves a Node Buffer encoding name (Node
// normalizeEncoding: case-insensitive, with its aliases) to its canonical
// form; ok is false for a name Buffer rejects.
func nodeBufferEncoding(label string) (string, bool) {
	switch strings.ToLower(label) {
	case "utf8", "utf-8":
		return "utf8", true
	case "ucs2", "ucs-2", "utf16le", "utf-16le":
		return "utf16le", true
	case "latin1", "binary":
		return "latin1", true
	case "base64", "base64url", "hex", "ascii":
		return strings.ToLower(label), true
	}
	return "", false
}

func unknownNodeEncoding(label string) error {
	return fmt.Errorf("Unknown encoding: %s", label) //nolint:staticcheck // Node ERR_UNKNOWN_ENCODING text
}

// DecodeSandboxText is TS Buffer.from(bytes).toString(encoding) in the
// sandbox sessions' readTextFile (encoding defaults to "utf-8"; "" is the
// default here). It takes Node Buffer encodings, not WHATWG labels: utf8 /
// utf-8 (invalid sequences as U+FFFD), utf16le / ucs2 (a trailing odd byte
// dropped), latin1 / binary, ascii (the high bit cleared), hex, base64 and
// base64url; any other name fails with Node's "Unknown encoding: <name>".
func DecodeSandboxText(data []byte, encodingLabel string) (string, error) {
	if encodingLabel == "" {
		return string(textdecode.ReplaceInvalid(data)), nil
	}
	enc, ok := nodeBufferEncoding(encodingLabel)
	if !ok {
		return "", unknownNodeEncoding(encodingLabel)
	}
	switch enc {
	case "utf8":
		return string(textdecode.ReplaceInvalid(data)), nil
	case "utf16le":
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
		}
		return string(utf16.Decode(units)), nil
	case "latin1", "ascii":
		var b strings.Builder
		b.Grow(len(data) * 2)
		for _, c := range data {
			if enc == "ascii" {
				c &= 0x7f
			}
			b.WriteRune(rune(c))
		}
		return b.String(), nil
	case "hex":
		return hex.EncodeToString(data), nil
	case "base64":
		return base64.StdEncoding.EncodeToString(data), nil
	default: // base64url
		return base64.RawURLEncoding.EncodeToString(data), nil
	}
}

// EncodeSandboxText is TS Buffer.from(content, encoding) in writeTextFile,
// with the Node Buffer encodings DecodeSandboxText takes. The text is
// handled as the JavaScript string (UTF-16 code units): latin1 / binary and
// ascii keep each unit's low byte; hex decodes digit pairs up to the first
// pair that is not hex; base64 / base64url accept both alphabets, skip other
// characters (a unit counts by its low byte) and stop at "=".
func EncodeSandboxText(text, encodingLabel string) ([]byte, error) {
	enc := "utf8"
	if encodingLabel != "" {
		var ok bool
		if enc, ok = nodeBufferEncoding(encodingLabel); !ok {
			return nil, unknownNodeEncoding(encodingLabel)
		}
	}
	if enc == "utf8" {
		return []byte(textdecode.ReplaceInvalidString(text)), nil
	}
	units := utf16.Encode([]rune(textdecode.ReplaceInvalidString(text)))
	switch enc {
	case "utf16le":
		out := make([]byte, 0, 2*len(units))
		for _, u := range units {
			out = append(out, byte(u), byte(u>>8))
		}
		return out, nil
	case "latin1", "ascii":
		out := make([]byte, len(units))
		for i, u := range units {
			out[i] = byte(u)
		}
		return out, nil
	case "hex":
		out := make([]byte, 0, len(units)/2)
		for i := 0; i+1 < len(units); i += 2 {
			hi, ok1 := hexDigitValue(units[i])
			lo, ok2 := hexDigitValue(units[i+1])
			if !ok1 || !ok2 {
				break
			}
			out = append(out, hi<<4|lo)
		}
		return out, nil
	default: // base64, base64url: Node's forgiving legacy decoder
		return nodeBase64Decode(units), nil
	}
}

func hexDigitValue(u uint16) (byte, bool) {
	switch {
	case u >= '0' && u <= '9':
		return byte(u - '0'), true
	case u >= 'a' && u <= 'f':
		return byte(u-'a') + 10, true
	case u >= 'A' && u <= 'F':
		return byte(u-'A') + 10, true
	}
	return 0, false
}

// nodeBase64Decode is Node's base64_decode_slow: each unit is read by its
// low byte, characters outside both alphabets are skipped, "=" (or the end)
// stops decoding, and a trailing group of n > 1 characters gives n-1 bytes.
func nodeBase64Decode(units []uint16) []byte {
	value := func(c byte) (byte, bool) {
		switch {
		case c >= 'A' && c <= 'Z':
			return c - 'A', true
		case c >= 'a' && c <= 'z':
			return c - 'a' + 26, true
		case c >= '0' && c <= '9':
			return c - '0' + 52, true
		case c == '+' || c == '-':
			return 62, true
		case c == '/' || c == '_':
			return 63, true
		}
		return 0, false
	}
	out := make([]byte, 0, len(units)*3/4)
	var group [4]byte
	n := 0
	for _, u := range units {
		c := byte(u)
		if c == '=' {
			break
		}
		v, ok := value(c)
		if !ok {
			continue
		}
		group[n] = v
		n++
		if n == 4 {
			out = append(out, group[0]<<2|group[1]>>4, group[1]<<4|group[2]>>2, group[2]<<6|group[3])
			n = 0
		}
	}
	switch n {
	case 2:
		out = append(out, group[0]<<2|group[1]>>4)
	case 3:
		out = append(out, group[0]<<2|group[1]>>4, group[1]<<4|group[2]>>2)
	}
	return out
}
