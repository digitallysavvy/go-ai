package bytedance

import "testing"

// TS ByteDanceVideoModel has no doGenerate, so TS generateVideo always drives it through the
// core start/status flow. The model marks itself for ai.GenerateVideo with
// VideoStartStatusPollDefaults, returning the v0.5.2 pollIntervalMs /
// pollTimeoutMs provider options (0 = the core default).
func TestR89provcVideoStartStatusPollDefaults(t *testing.T) {
	var model interface {
		VideoStartStatusPollDefaults(map[string]interface{}) (int, int)
	} = r89provcByteDanceModel(t)
	if i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{}); i != 0 || to != 0 {
		t.Fatalf("defaults without options = %d, %d; want 0, 0", i, to)
	}
	i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{"bytedance": map[string]interface{}{"pollIntervalMs": float64(250), "pollTimeoutMs": float64(90000)}})
	if i != 250 || to != 90000 {
		t.Fatalf("defaults = %d, %d; want 250, 90000", i, to)
	}
}

func r89provcByteDanceModel(t *testing.T) *VideoModel {
	t.Helper()
	prov, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	return newVideoModel(prov, "seedance-1-0-pro-250528")
}
