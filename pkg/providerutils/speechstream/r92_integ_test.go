package speechstream

import (
	"testing"
	"time"
)

// TS speech-stream-envelope.ts 335: new Date(timestamp) (Date.parse,
// millisecond precision); a NaN date makes the part invalid.
func TestR92integResponseMetadataTimestampIsJSDate(t *testing.T) {
	for in, want := range map[string]time.Time{
		"2026-01-02":                    time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		"2026-01-02T03:04:05.123456Z":   time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC),
		"Fri, 02 Jan 2026 03:04:05 GMT": time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	} {
		part, ok := ParsePart(`{"type":"response-metadata","modelId":"m","timestamp":"` + in + `"}`)
		if !ok || !part.Timestamp.Equal(want) {
			t.Errorf("%q: %+v %v, want %v", in, part, ok, want)
		}
	}
	if _, ok := ParsePart(`{"type":"response-metadata","timestamp":"not a date"}`); ok {
		t.Error("an invalid date was accepted")
	}
}
