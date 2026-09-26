package providerutils

import (
	"errors"
	"strings"
)

// ErrEmptyBaseURL is returned by ValidateBaseURL for a base URL that is set
// but empty after trimming whitespace.
var ErrEmptyBaseURL = errors.New("baseURL must be a non-empty string.")

// ValidateBaseURL mirrors validateBaseURL from the TS provider-utils: an unset
// ("") base URL is allowed (the provider default applies), but a base URL made
// only of whitespace is rejected.
func ValidateBaseURL(baseURL string) error {
	if baseURL != "" && strings.TrimSpace(baseURL) == "" {
		return ErrEmptyBaseURL
	}
	return nil
}
