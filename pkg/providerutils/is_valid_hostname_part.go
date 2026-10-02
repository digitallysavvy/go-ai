package providerutils

import "regexp"

// validHostnamePartPattern matches a single ASCII DNS label: 1-63 letters,
// digits, or hyphens, without a leading or trailing hyphen.
var validHostnamePartPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// IsValidHostnamePart checks whether value is a valid ASCII hostname part:
// 1-63 letters, digits, or hyphens, without a leading or trailing hyphen.
//
// Use this before inserting a resource name, region, or location into a
// generated hostname. Rejects values such as "evil.example.com/#" and
// "user@localhost:8080/#" that could change the request destination.
// Mirrors TS provider-utils isValidHostnamePart. (TS compares the full
// match rather than using `.test()` because JS `$` can match just before a
// trailing newline; Go's RE2 `$` has no such exception, so MatchString
// already requires the whole string to match.)
func IsValidHostnamePart(value string) bool {
	return validHostnamePartPattern.MatchString(value)
}
