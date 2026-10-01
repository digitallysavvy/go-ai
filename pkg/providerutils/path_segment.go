package providerutils

import "strings"

// EncodePathSegment encodes a provider-returned identifier for use as a single
// URL path segment in a credentialed follow-up request (TS encodePathSegment in
// anthropic-skills.ts, google-files.ts and xai-video-model.ts, 7de3612).
//
// It applies JavaScript encodeURIComponent semantics (so "/", "?", "#" and
// "%" cannot change the request target), then double-encodes the dot
// segments "." and ".." as "%252E" / "%252E%252E" because URL parsers
// normalize both literal and percent-encoded dot segments.
func EncodePathSegment(value string) string {
	encoded := encodeURIComponent(value)
	switch encoded {
	case ".":
		return "%252E"
	case "..":
		return "%252E%252E"
	}
	return encoded
}

const upperHex = "0123456789ABCDEF"

// encodeURIComponent mirrors ECMAScript encodeURIComponent: every byte of the
// UTF-8 encoding is percent-encoded except A-Z a-z 0-9 - _ . ! ~ * ' ( ).
func encodeURIComponent(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex[c>>4])
		b.WriteByte(upperHex[c&0x0f])
	}
	return b.String()
}
