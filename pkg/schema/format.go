package schema

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"time"
)

// emailFormatRe is intentionally a pragmatic "looks like an email" check
// (single '@', at least one '.' in the domain part), not a full RFC 5322
// implementation -- the same level of strictness commonly used by JSON
// Schema validators for the "email" format.
var emailFormatRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// uuidFormatRe matches the canonical 8-4-4-4-12 hex UUID form (any version).
var uuidFormatRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// validateFormat performs best-effort validation for the JSON Schema
// "format" keywords go-ai treats as validating annotations (LLM tool
// schemas commonly rely on format for input guardrails, e.g. "uuid" record
// IDs or "date-time" timestamps). Per the JSON Schema spec "format" is
// advisory by default; unrecognized format names are treated as
// non-validating annotations (no error) rather than rejected.
func validateFormat(value, format string) error {
	switch format {
	case "date-time":
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return fmt.Errorf("%q is not a valid date-time", value)
		}
	case "date":
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return fmt.Errorf("%q is not a valid date", value)
		}
	case "time":
		if _, err := time.Parse("15:04:05.999999999Z07:00", value); err != nil {
			if _, err2 := time.Parse("15:04:05Z07:00", value); err2 != nil {
				return fmt.Errorf("%q is not a valid time", value)
			}
		}
	case "email":
		if !emailFormatRe.MatchString(value) {
			return fmt.Errorf("%q is not a valid email address", value)
		}
	case "uri":
		u, err := url.ParseRequestURI(value)
		if err != nil || u.Scheme == "" {
			return fmt.Errorf("%q is not a valid uri", value)
		}
	case "uuid":
		if !uuidFormatRe.MatchString(value) {
			return fmt.Errorf("%q is not a valid uuid", value)
		}
	case "ipv4":
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("%q is not a valid ipv4 address", value)
		}
	case "ipv6":
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() != nil || ip.To16() == nil {
			return fmt.Errorf("%q is not a valid ipv6 address", value)
		}
	}
	return nil
}
