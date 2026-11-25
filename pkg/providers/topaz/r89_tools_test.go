package topaz

import "testing"

// provc CR r89: TS TopazVideoModel has no doGenerate, so ai.GenerateVideo
// must use the core start/status flow. The marker returns the Go-only
// Config poll settings when positive, else 0 (core defaults).
func TestR89toolsTopazVideoStartStatusPollDefaults(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), VideoModelStarlightPrecise26)
	if i, to := m.VideoStartStatusPollDefaults(nil); i != 0 || to != 0 {
		t.Fatalf("defaults = %d, %d, want 0, 0", i, to)
	}
	m = NewVideoModel(New(Config{APIKey: "k", VideoPollIntervalMillis: 250, VideoPollTimeoutMillis: 90000}), VideoModelStarlightPrecise26)
	if i, to := m.VideoStartStatusPollDefaults(map[string]interface{}{"topaz": map[string]interface{}{"pollIntervalMs": 1}}); i != 250 || to != 90000 {
		t.Fatalf("defaults = %d, %d, want 250, 90000", i, to)
	}
}
