package providerutils

import "regexp"

// validHostnamePart matches a 1-63 character ASCII hostname label: letters,
// digits, or hyphens, without a leading or trailing hyphen.
var validHostnamePart = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// IsValidHostnamePart reports whether value is safe to insert as one label
// of a generated hostname (TS isValidHostnamePart,
// provider-utils/src/is-valid-hostname-part.ts).
//
// Use it before inserting a user- or config-supplied resource name, region,
// or location into a generated hostname (e.g. AWS region, Google location).
// Unlike baseUrl, which explicitly directs a request elsewhere, these
// values are often configuration the caller does not expect to affect the
// request's destination. Rejects values such as "evil.example.com/#" and
// "user@localhost:8080/#" that could redirect the request.
func IsValidHostnamePart(value string) bool {
	return validHostnamePart.MatchString(value)
}
