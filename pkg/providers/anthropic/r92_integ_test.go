package anthropic

import (
	"testing"
	"time"
)

// TS anthropic-files.ts: new Date(created_at / expires_at) (Date.parse,
// millisecond precision).
func TestR92integFileTimeIsJSDate(t *testing.T) {
	for in, want := range map[string]time.Time{
		"2026-01-02":                    time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		"2026-01-02T03:04:05.123456Z":   time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC),
		"Fri, 02 Jan 2026 03:04:05 GMT": time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	} {
		s := in
		if got := anthropicFileTime(&s); got == nil || !got.Equal(want) {
			t.Errorf("anthropicFileTime(%q) = %v, want %v", in, got, want)
		}
	}
	bad := "not a date"
	if got := anthropicFileTime(&bad); got != nil {
		t.Errorf("invalid date = %v, want unset", got)
	}
}
