package retry

import (
	"math"
	"testing"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS getRetryDelayInMs reads retry-after-ms / retry-after with parseFloat
// and an HTTP date with Date.parse.
func TestR92integJSParseFloat(t *testing.T) {
	for in, want := range map[string]float64{
		"5": 5, "5s": 5, " 1.5e3x": 1500, ".5": 0.5, "5.": 5, "1e": 1, "1e+": 1, "-2": -2,
		"Infinity": math.Inf(1), "-Infinityx": math.Inf(-1), "\u00a0\ufeff7": 7, "1e400": math.Inf(1),
	} {
		got, ok := jsParseFloat(in)
		if !ok || got != want {
			t.Errorf("jsParseFloat(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "abc", ".", "+-1", "e5", "\u00857", "infinity", "Fri, 02 Jan 2026 03:04:05 GMT"} {
		if got, ok := jsParseFloat(in); ok {
			t.Errorf("jsParseFloat(%q) = %v, want NaN", in, got)
		}
	}
}

func TestR92integRetryAfterJSSemantics(t *testing.T) {
	headers := func(h map[string]string) error {
		return &providererrors.ProviderError{ResponseHeaders: h}
	}
	if d, ok := retryHeaderDelay(headers(map[string]string{"retry-after": "2s"}), time.Second); !ok || d != 2*time.Second {
		t.Errorf("retry-after 2s = %v %v, want 2s (parseFloat prefix)", d, ok)
	}
	if d, ok := retryHeaderDelay(headers(map[string]string{"retry-after-ms": "250ms"}), time.Second); !ok || d != 250*time.Millisecond {
		t.Errorf("retry-after-ms 250ms = %v %v, want 250ms", d, ok)
	}
	// A date Date.parse reads and http.ParseTime does not (numeric zone).
	when := time.Now().Add(20 * time.Second).UTC().Format(time.RFC1123Z)
	if d, ok := retryHeaderDelay(headers(map[string]string{"retry-after": when}), time.Second); !ok || d < 15*time.Second || d > 21*time.Second {
		t.Errorf("retry-after %s = %v %v, want about 20s", when, d, ok)
	}
	// parseFloat reads an ISO date's year as seconds (2026 s): too long, so
	// the exponential delay is used, as TS.
	iso := time.Now().Add(20 * time.Second).UTC().Format("2006-01-02T15:04:05.000Z")
	if d, ok := retryHeaderDelay(headers(map[string]string{"retry-after": iso}), time.Second); ok {
		t.Errorf("retry-after %s = %v, want the exponential delay", iso, d)
	}
}
