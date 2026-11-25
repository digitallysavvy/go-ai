package minimax

import "testing"

// TS parseProviderOptions(minimaxVideoModelOptionsSchema).
func TestVideoProviderOptionsValidation(t *testing.T) {
	for _, o := range []map[string]interface{}{
		{"resolution": "1080P"}, {"ratio": "2:1"}, {"referenceAudioUrls": "u"}, {"aigcWatermark": "y"},
		{"pollIntervalMs": 0}, {"pollTimeoutMs": -1}, {"pollIntervalMs": 1.5}, {"resolution": nil},
	} {
		if _, err := extractVideoProviderOptions(map[string]interface{}{"minimax": o}); err == nil {
			t.Errorf("%v accepted", o)
		}
	}
	if _, err := extractVideoProviderOptions(map[string]interface{}{"minimax": map[string]interface{}{
		"resolution": "2K", "ratio": "adaptive", "referenceAudioUrls": []string{"u"}, "pollIntervalMs": 5,
	}}); err != nil {
		t.Errorf("valid options rejected: %v", err)
	}
	if _, err := extractVideoProviderOptions(map[string]interface{}{"minimax": VideoModelOptions{}}); err != nil {
		t.Errorf("typed options rejected: %v", err)
	}
}
