package providerutils

import "strings"

// WithoutTrailingSlash is TS withoutTrailingSlash: it removes a single
// trailing "/" (url?.replace(/\/$/, '')), so a configured base URL such as
// "https://api.example.com/v1/" does not produce "//" when a request path
// starting with "/" is appended.
func WithoutTrailingSlash(url string) string {
	return strings.TrimSuffix(url, "/")
}
