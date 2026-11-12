package retry

import (
	"testing"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS getRetryDelayInMs checks the delay as a JS number (0 <= ms and ms < 60s
// or below the exponential delay). A Retry-After date centuries ahead, or a
// retry-after-ms far beyond int64 nanoseconds, is too long and falls back to
// the exponential delay; it must not wrap into a short delay.
func TestR93carryRetryAfterHugeDelayFallsBack(t *testing.T) {
	headers := func(h map[string]string) error {
		return &providererrors.ProviderError{ResponseHeaders: h}
	}
	for _, h := range []map[string]string{
		{"retry-after": "Sat, 01 Jan 275000 00:00:00 GMT"},
		{"retry-after": "Mon, 01 Jan 0001 00:00:00 GMT"},
		{"retry-after-ms": "1e300"},
		{"retry-after-ms": "Infinity"},
		{"retry-after": "1e20"},
	} {
		if d, ok := retryHeaderDelay(headers(h), time.Second); ok {
			t.Errorf("%v: delay %v, want the exponential delay", h, d)
		}
	}
	if d, ok := retryHeaderDelay(headers(map[string]string{"retry-after-ms": "1500.5"}), time.Second); !ok || d != 1500500*time.Microsecond {
		t.Errorf("retry-after-ms 1500.5 = %v %v", d, ok)
	}
}
