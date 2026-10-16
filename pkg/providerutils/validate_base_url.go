package providerutils

import (
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

const emptyBaseURLMessage = "baseURL must be a non-empty string."

// emptyBaseURLError is the ValidateBaseURL failure. Its text is the TS
// InvalidArgumentError message verbatim and errors.As reaches an
// *InvalidArgumentError{Field: "baseURL"}, like the TS error class.
type emptyBaseURLError struct{}

func (emptyBaseURLError) Error() string { return emptyBaseURLMessage }

// As lets callers match the TS InvalidArgumentError type.
func (emptyBaseURLError) As(target interface{}) bool {
	if t, ok := target.(**providererrors.InvalidArgumentError); ok {
		*t = &providererrors.InvalidArgumentError{Field: "baseURL", Message: emptyBaseURLMessage}
		return true
	}
	return false
}

// ErrEmptyBaseURL is returned by ValidateBaseURL for a base URL that is set
// but empty after trimming whitespace.
var ErrEmptyBaseURL error = emptyBaseURLError{}

// ValidateBaseURL mirrors validateBaseURL from the TS provider-utils: an unset
// ("") base URL is allowed (the provider default applies), but a base URL made
// only of whitespace is rejected.
func ValidateBaseURL(baseURL string) error {
	if baseURL != "" && JSTrim(baseURL) == "" {
		return ErrEmptyBaseURL
	}
	return nil
}
