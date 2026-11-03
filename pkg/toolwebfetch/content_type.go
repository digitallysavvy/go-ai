package toolwebfetch

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/encoding/htmlindex"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
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
	start := strings.ToLower(strings.TrimLeftFunc(prompt.DecodeUTF8Text(head), isJSWhitespace))
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
	// TS new TextDecoder(charset).decode(body) drops the BOM of that
	// encoding; the UTF-8 paths decode the whole body the same way.
	full := body
	body = body[bomLen:]

	enc, err := htmlindex.Get(charset)
	if err != nil {
		return prompt.DecodeUTF8Text(full)
	}
	if name, _ := htmlindex.Name(enc); name == "utf-8" {
		return prompt.DecodeUTF8Text(full)
	}
	decoded, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		return prompt.DecodeUTF8Text(full)
	}
	return string(decoded)
}

// isJSWhitespace is the set JavaScript's String.prototype.trimStart removes.
func isJSWhitespace(r rune) bool { return unicode.IsSpace(r) || r == 0xFEFF }
