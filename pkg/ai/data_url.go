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
	base64Content := parts[1]

	byteString, err := base64.StdEncoding.DecodeString(base64Content)
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
