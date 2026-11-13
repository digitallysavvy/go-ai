package alibaba

import "testing"

// provc CR (TS generate-video.ts useStartStatus: AlibabaVideoModel has no
// doGenerate): the model reports its v0.5.2 poll options to the core
// start/status flow.
func TestR89provbVideoStartStatusPollDefaults(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), "wan2.6-t2v")
	if i, to := m.VideoStartStatusPollDefaults(nil); i != 0 || to != 0 {
		t.Errorf("empty options = %d, %d; want 0, 0", i, to)
	}
	i, to := m.VideoStartStatusPollDefaults(map[string]interface{}{"alibaba": map[string]interface{}{"pollIntervalMs": 250, "pollTimeoutMs": 90000}})
	if i != 250 || to != 90000 {
		t.Errorf("poll options = %d, %d; want 250, 90000", i, to)
	}
}

// TS extractResponseHeaders: repeated lines are joined with ", " (keys stay
// Go canonical, S6).
func TestR89provbVideoHeadersJoinRepeatedLines(t *testing.T) {
	got := flattenHTTPHeaders(map[string][]string{"X-Multi": {"a", "b"}})
	if got["X-Multi"] != "a, b" {
		t.Fatalf("headers = %v", got)
	}
}
