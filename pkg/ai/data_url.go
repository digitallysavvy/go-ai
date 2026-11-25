package ai

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// dataURLCharsetRe extracts a declared charset from a data URL header, e.g.
// "data:text/plain;charset=iso-8859-1;base64" -> "iso-8859-1". Mirrors TS
// getTextFromDataUrl's charset regex /(?:^|;)\s*charset\s*=\s*(?:"([^"]+)"|
// ([^;\s]+))/i with JS \s, and the "s" of charset without Go's (?i) fold to
// U+017F (a JS /i regexp without the u flag does not apply it).
var dataURLCharsetRe = regexp.MustCompile(`(?i)(?:^|;)` + providerutils.JSRegexpSpace + `*char(?-i:[sS])et` + providerutils.JSRegexpSpace + `*=` +
	providerutils.JSRegexpSpace + `*(?:"([^"]+)"|([^;` + providerutils.JSRegexpSpaceChars + `]+))`)

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
			Value:   dataURL,
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
			Value:   dataURL,
			Message: "Error decoding data URL",
		}
	}

	if charset == "" {
		// TS returns atob's binary string: each byte one character.
		return providerutils.IsomorphicDecode(string(byteString)), nil
	}

	decoded, err := textDecoderDecode(byteString, charset)
	if err != nil {
		return "", &providererrors.InvalidArgumentError{
			Field:   "dataUrl",
			Value:   dataURL,
			Message: "Error decoding data URL",
		}
	}
	return decoded, nil
}

// textDecoderDecode is TS new TextDecoder(label).decode(data) (not fatal):
// the label is an encoding label (ASCII whitespace trimmed, ASCII
// case-insensitive; "replacement" labels and unknown names throw), UTF-8 and
// UTF-16 drop their BOM, and invalid input decodes as U+FFFD.
func textDecoderDecode(data []byte, label string) (string, error) {
	label = strings.Trim(label, "\t\n\f\r ")
	for i := 0; i < len(label); i++ {
		// Every WHATWG label is printable ASCII; htmlindex also trims
		// Unicode spaces and folds U+212A, which TextDecoder does not.
		if label[i] <= ' ' || label[i] > '~' {
			return "", fmt.Errorf("The encoding label provided ('%s') is invalid.", label) //nolint:staticcheck // TextDecoder RangeError text
		}
	}
	enc, err := htmlindex.Get(label)
	if err != nil {
		return "", fmt.Errorf("The encoding label provided ('%s') is invalid.", label) //nolint:staticcheck // TextDecoder RangeError text
	}
	name, _ := htmlindex.Name(enc)
	switch name {
	case "replacement":
		return "", fmt.Errorf("The encoding label provided ('%s') is invalid.", label) //nolint:staticcheck // TextDecoder RangeError text
	case "utf-8":
		return textdecode.UTF8(data), nil
	case "utf-16le":
		data = bytes.TrimPrefix(data, []byte{0xff, 0xfe})
	case "utf-16be":
		data = bytes.TrimPrefix(data, []byte{0xfe, 0xff})
	}
	decoded, _, err := transform.Bytes(enc.NewDecoder(), data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
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
