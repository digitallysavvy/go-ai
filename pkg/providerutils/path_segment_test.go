package providerutils

import "testing"

func TestEncodePathSegment(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abc123", "abc123"},
		{"abc/../../internal", "abc%2F..%2F..%2Finternal"},
		{"abc/../../secret", "abc%2F..%2F..%2Fsecret"},
		{".", "%252E"},
		{"..", "%252E%252E"},
		{"...", "..."},
		{"a b?c#d%e", "a%20b%3Fc%23d%25e"},
		{"-_.!~*'()", "-_.!~*'()"},
		{"arn:aws:x", "arn%3Aaws%3Ax"},
		{"ü", "%C3%BC"},
	}
	for _, tt := range tests {
		if got := EncodePathSegment(tt.in); got != tt.want {
			t.Errorf("EncodePathSegment(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
