package tsfixture

import "testing"

// Go sending fewer identical polls than TS is not a difference; a poll that
// TS got a different response for, or one with a different URL, still must
// be sent.
func TestIsSeenPollRepeat(t *testing.T) {
	poll := func(url, body string) WireHTTP {
		return WireHTTP{Method: "GET", URL: url, Response: &WireResponse{Type: "json-value", Body: body}}
	}
	rec := []WireHTTP{
		{Method: "POST", URL: "https://x/create"},
		poll("https://x/q/1", "running"),
		poll("https://x/q/1", "running"),
		poll("https://x/q/1", "done"),
		poll("https://x/q/2", "running"),
	}
	seen := map[int]bool{0: true, 1: true}
	for i, want := range map[int]bool{2: true, 3: false, 4: false} {
		if got := isSeenPollRepeat(rec, seen, i); got != want {
			t.Errorf("isSeenPollRepeat(%d) = %v, want %v", i, got, want)
		}
	}
	if isSeenPollRepeat(rec, map[int]bool{0: true}, 2) {
		t.Error("a repeat of a poll Go never sent must still be reported")
	}
}
