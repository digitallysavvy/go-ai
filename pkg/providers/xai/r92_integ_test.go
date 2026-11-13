package xai

import "testing"

// TS xai-batch.ts isPastDate: Date.parse(value), NaN is not past.
func TestR92integIsPastDateIsJSDate(t *testing.T) {
	for in, want := range map[string]bool{
		"2000-01-02":                    true,
		"Sun, 02 Jan 2000 03:04:05 GMT": true,
		"2000-01-02T03:04:05Z":          true,
		"2999-01-02":                    false,
		"":                              false,
		"not a date":                    false,
	} {
		if got := xaiIsPastDate(in); got != want {
			t.Errorf("xaiIsPastDate(%q) = %v, want %v", in, got, want)
		}
	}
}

// TS mapResponseUsage keeps the JS number of each usage member.
func TestR92integRealtimeUsageKeepsFractionalTokens(t *testing.T) {
	u := xaiRealtimeResponseUsage(map[string]interface{}{"input_tokens": 10.5, "output_tokens": float64(2)})
	if u == nil || u.InputTokens == nil || *u.InputTokens != 10.5 || *u.OutputTokens != 2 {
		t.Fatalf("usage = %+v", u)
	}
}
