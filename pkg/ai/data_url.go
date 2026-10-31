package ai

import (
	"encoding/base64"
	"regexp"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// dataURLCharsetRe extracts a declared charset from a data URL header, e.g.
// "data:text/plain;charset=iso-8859-1;base64" -> "iso-8859-1". Mirrors TS
// getTextFromDataUrl's charset regex.
var dataURLCharsetRe = regexp.MustCompile(`(?i)(?:^|;)\s*charset\s*=\s*(?:"([^"]+)"|([^;\s]+))`)

// GetTextFromDataURL decodes a base64-encoded data URL of type text/* into a
// string, honoring the URL's declared charset (UTF-8 when none is declared).
// Mirrors TS util/data-url.ts's getTextFromDataUrl (audit row c415657 /
// WG14).
func GetTextFromDataURL(dataURL string) (string, error) {
	parts := strings.SplitN(dataURL, ",", 2)
	header := parts[0]

	// mediaType = header.split(';')[0].split(':')[1]
	beforeSemi := strings.SplitN(header, ";", 2)[0]
	colonParts := strings.SplitN(beforeSemi, ":", 2)
	hasMediaType := len(colonParts) == 2

	var charset string
	if m := dataURLCharsetRe.FindStringSubmatch(header); m != nil {
		if m[1] != "" {
			charset = m[1]
		} else {
			charset = m[2]
		}
	}

	if !hasMediaType || len(parts) < 2 {
		return "", &providererrors.InvalidArgumentError{
			Field:   "dataUrl",
			Message: "Invalid data URL format",
		}
	}
	// TS: const [header, base64Content] = dataUrl.split(',') keeps only the
	// segment between the first and second comma.
	base64Content := strings.SplitN(parts[1], ",", 2)[0]

	byteString, err := atob(base64Content)
	if err != nil {
		return "", &providererrors.InvalidArgumentError{
			Field:   "dataUrl",
			Message: "Error decoding data URL",
		}
	}

	if charset == "" {
		return string(byteString), nil
	}

	decoded, err := decodeSandboxText(byteString, charset)
	if err != nil {
		return "", &providererrors.InvalidArgumentError{
			Field:   "dataUrl",
			Message: "Error decoding data URL",
		}
	}
	return decoded, nil
}

// atob implements the WHATWG forgiving-base64 decode behind JavaScript's
// atob: ASCII whitespace is ignored, one or two trailing "=" are accepted
// when the length is a multiple of four, padding may otherwise be omitted,
// and a length of 1 mod 4 or any non-alphabet byte is an error.
func atob(s string) ([]byte, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\t', '\n', '\f', '\r', ' ':
			continue
		}
		b.WriteByte(s[i])
	}
	data := b.String()
	if len(data)%4 == 0 {
		if strings.HasSuffix(data, "==") {
			data = data[:len(data)-2]
		} else if strings.HasSuffix(data, "=") {
			data = data[:len(data)-1]
		}
	}
	if len(data)%4 == 1 {
		return nil, base64.CorruptInputError(len(data))
	}
	return base64.RawStdEncoding.DecodeString(data)
}
