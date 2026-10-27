package toolwebfetch

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"
)

type contentKind int

const (
	kindHTML contentKind = iota
	kindText
	kindUnsupported
)

var textMediaTypes = map[string]bool{
	"application/ecmascript":   true,
	"application/javascript":   true,
	"application/json":         true,
	"application/ld+json":      true,
	"application/toml":         true,
	"application/x-javascript": true,
	"application/x-ndjson":     true,
	"application/x-yaml":       true,
	"application/xml":          true,
	"application/yaml":         true,
}

// getMediaType returns the lowercase media type of a Content-Type header, or
// "" when absent.
func getMediaType(contentType string) string {
	mediaType, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func looksLikeHTML(body []byte) bool {
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	start := strings.ToLower(strings.TrimLeftFunc(decodeUTF8(head), isJSWhitespace))
	return strings.HasPrefix(start, "<!doctype html") || strings.HasPrefix(start, "<html")
}

// classifyContent decides how to handle a response body. It uses the
// Content-Type header and falls back to sniffing when the server omits it or
// sends a generic type.
func classifyContent(mediaType string, body []byte) contentKind {
	if mediaType == "" || mediaType == "application/octet-stream" {
		if looksLikeHTML(body) {
			return kindHTML
		}
		return kindUnsupported
	}
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return kindHTML
	}
	if strings.HasPrefix(mediaType, "text/") || textMediaTypes[mediaType] ||
		strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
		return kindText
	}
	return kindUnsupported
}

var (
	charsetFromContentType = regexp.MustCompile(`(?i);\s*charset\s*=\s*"?([^";\s]+)"?`)
	charsetFromMetaTag     = regexp.MustCompile(`(?i)<meta[^>]+charset\s*=\s*["']?\s*([\w.:-]+)`)
)

func charsetFromBOM(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte{0xef, 0xbb, 0xbf}):
		return "utf-8"
	case bytes.HasPrefix(body, []byte{0xfe, 0xff}):
		return "utf-16be"
	case bytes.HasPrefix(body, []byte{0xff, 0xfe}):
		return "utf-16le"
	}
	return ""
}

// decodeText decodes a text response. The byte order mark takes precedence,
// then the Content-Type charset, then (for HTML) a <meta charset>
// declaration. Unknown or missing encodings fall back to UTF-8. Invalid
// sequences become U+FFFD.
func decodeText(body []byte, contentType string, isHTML bool) string {
	charset := charsetFromBOM(body)
	bomLen := 0
	switch charset {
	case "utf-8":
		bomLen = 3
	case "utf-16be", "utf-16le":
		bomLen = 2
	}
	if charset == "" {
		if m := charsetFromContentType.FindStringSubmatch(contentType); m != nil {
			charset = strings.ToLower(m[1])
		}
	}
	if charset == "" && isHTML {
		head := body
		if len(head) > 1024 {
			head = head[:1024]
		}
		// The prescan reads bytes as windows-1252 (every byte maps to a rune).
		runes := make([]rune, len(head))
		for i, b := range head {
			runes[i] = rune(b)
		}
		if m := charsetFromMetaTag.FindStringSubmatch(string(runes)); m != nil {
			charset = strings.ToLower(m[1])
		}
	}
	if charset == "" {
		charset = "utf-8"
	}
	body = body[bomLen:]

	enc, err := htmlindex.Get(charset)
	if err != nil {
		return decodeUTF8(body)
	}
	if name, _ := htmlindex.Name(enc); name == "utf-8" {
		return decodeUTF8(body)
	}
	decoded, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		return decodeUTF8(body)
	}
	return string(decoded)
}

// decodeUTF8 is TextDecoder('utf-8'): each invalid byte becomes U+FFFD.
func decodeUTF8(body []byte) string {
	if utf8.Valid(body) {
		return string(body)
	}
	var b strings.Builder
	b.Grow(len(body))
	for len(body) > 0 {
		r, size := utf8.DecodeRune(body)
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.Write(body[:size])
		}
		body = body[size:]
	}
	return b.String()
}

// isJSWhitespace is the set JavaScript's String.prototype.trimStart removes.
func isJSWhitespace(r rune) bool { return unicode.IsSpace(r) || r == 0xFEFF }
