package google

import "testing"

// provc CR (TS generate-video.ts useStartStatus): TS GoogleVideoModel has no
// doGenerate, so the model marks itself start/status only for
// ai.GenerateVideo, returning the v0.5.2 poll provider options (0 = core default).
func TestR89provgVideoStartStatusPollDefaults(t *testing.T) {
	var model interface {
		VideoStartStatusPollDefaults(map[string]interface{}) (int, int)
	} = NewVideoModel(New(Config{APIKey: "k"}), "veo-3.0-generate-001")
	if i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{}); i != 0 || to != 0 {
		t.Fatalf("defaults = %d, %d; want 0, 0", i, to)
	}
	i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": float64(250), "pollTimeoutMs": float64(90000)}})
	if i != 250 || to != 90000 {
		t.Fatalf("defaults = %d, %d; want 250, 90000", i, to)
	}
}
