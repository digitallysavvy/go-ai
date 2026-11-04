package providerutils

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
)

// FetchHeaderValue returns the bytes TS fetch's Headers.get(name) holds:
// every field line of name joined with ", " ("" when absent). Use it where TS
// passes the value on unchanged (an echoed session id, for example); use
// FetchHeaderText where TS reads the value as text.
func FetchHeaderValue(h http.Header, name string) string {
	return strings.Join(h.Values(name), ", ")
}

// FetchHeaderText is FetchHeaderValue read as the JavaScript string TS sees:
// a header value is a ByteString, so each byte is one character (see
// IsomorphicDecode). A raw 0xA0 byte is then a JS `\s` and trim() strips it,
// and a raw UTF-8 "é" reads as "Ã©", as in TS.
func FetchHeaderText(h http.Header, name string) string {
	return IsomorphicDecode(FetchHeaderValue(h, name))
}

// IsomorphicDecode reads s byte by byte as U+0000..U+00FF (WHATWG isomorphic
// decode), how a JS string holding an HTTP header value reads as text.
func IsomorphicDecode(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		b.WriteRune(rune(s[i]))
	}
	return b.String()
}

// FetchURL is how TS fetch(input) reads a URL string: new URL(input).href
// (percent-encoding, IDNA, dot segments), or the TypeError text "Failed to
// parse URL from <input>" when input does not parse.
func FetchURL(input string) (string, error) {
	href, err := urlnorm.Href(input)
	if err != nil {
		return "", fmt.Errorf("Failed to parse URL from %s", input) //nolint:staticcheck // TS fetch TypeError text
	}
	return href, nil
}

// FetchURLFromHeader reads a URL TS takes from response.headers.get(name) and
// passes to fetch: FetchHeaderText then FetchURL. It returns "" and no error
// when the header is absent or empty (TS `if (!url)`).
func FetchURLFromHeader(h http.Header, name string) (string, error) {
	text := FetchHeaderText(h, name)
	if text == "" {
		return "", nil
	}
	return FetchURL(text)
}
