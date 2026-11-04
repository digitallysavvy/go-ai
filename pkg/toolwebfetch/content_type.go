package toolwebfetch

import (
	"bytes"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/htmlindex"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	// JS trim (not Go TrimSpace: U+0085 is not JS whitespace, U+FEFF is).
	return strings.ToLower(providerutils.JSTrim(mediaType))
}

func looksLikeHTML(body []byte) bool {
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	start := strings.ToLower(providerutils.JSTrimStart(prompt.DecodeUTF8Text(head)))
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

// TS /;\s*charset\s*=\s*"?([^";\s]+)"?/i and
// /<meta[^>]+charset\s*=\s*["']?\s*([\w.:-]+)/i with JS `\s`. The header is
// read as Latin-1 (FetchHeaderText) and the prescan as windows-1252, so no
// input holds the runes Go `(?i)` folds to s or `\w` (U+017F, U+212A).
var (
	charsetFromContentType = regexp.MustCompile(`(?i);` + providerutils.JSRegexpSpace + `*charset` + providerutils.JSRegexpSpace + `*=` + providerutils.JSRegexpSpace + `*"?([^";` + providerutils.JSRegexpSpaceChars + `]+)"?`)
	charsetFromMetaTag     = regexp.MustCompile(`(?i)<meta[^>]+charset` + providerutils.JSRegexpSpace + `*=` + providerutils.JSRegexpSpace + `*["']?` + providerutils.JSRegexpSpace + `*([\w.:-]+)`)
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
	if err != nil || !isEncodingLabel(charset) {
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

// isEncodingLabel reports whether TS new TextDecoder(label) could accept the
// label. It trims only ASCII whitespace and matches ASCII case-insensitively;
// htmlindex.Get also trims Unicode spaces (U+0085, U+00A0) and lowercases
// U+212A to k. Every WHATWG label is printable ASCII.
func isEncodingLabel(label string) bool {
	label = strings.Trim(label, "\t\n\f\r ")
	for i := 0; i < len(label); i++ {
		if label[i] <= ' ' || label[i] > '~' {
			return false
		}
	}
	return label != ""
}
