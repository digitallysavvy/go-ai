package googlevertex

import "testing"

// provc CR (TS generate-video.ts useStartStatus): TS GoogleVertexVideoModel
// has no doGenerate, so the model marks itself start/status only for
// ai.GenerateVideo, returning the v0.5.2 poll provider options (0 = core default).
func TestR89provgVideoStartStatusPollDefaults(t *testing.T) {
	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	var model interface {
		VideoStartStatusPollDefaults(map[string]interface{}) (int, int)
	} = NewVideoModel(p, "veo-3.0-generate-001")
	if i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{}); i != 0 || to != 0 {
		t.Fatalf("defaults = %d, %d; want 0, 0", i, to)
	}
	for _, key := range []string{"googleVertex", "vertex"} {
		i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{key: map[string]interface{}{"pollIntervalMs": float64(250), "pollTimeoutMs": float64(90000)}})
		if i != 250 || to != 90000 {
			t.Fatalf("%s: defaults = %d, %d; want 250, 90000", key, i, to)
		}
	}
}
