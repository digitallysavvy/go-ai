package errors

import "testing"

// cr-r86b-stream-to-integrator 1: TS normalize-stream-provider-error.ts 69
// switch (message.trim().toLowerCase()): JS trim strips U+FEFF but not
// U+0085, and JS lowercases U+0130 to "i̇", so "\u0130nternal server
// error" is no known message.
func TestR86bintegInferredMessageMetadataJSTrimAndLower(t *testing.T) {
	for _, tc := range []struct {
		msg    string
		status int
		ok     bool
	}{
		{"Overloaded\ufeff", 503, true},
		{"\u3000internal server error", 500, true},
		{"\u0085overloaded", 0, false},
		{"\u0130nternal server error", 0, false},
		{"SERVICE UNAVAILABLE", 503, true},
	} {
		status, _, ok := inferredMessageMetadata(tc.msg)
		if status != tc.status || ok != tc.ok {
			t.Errorf("inferredMessageMetadata(%+q) = %d, %v; want %d, %v", tc.msg, status, ok, tc.status, tc.ok)
		}
	}
}
