package klingai

import "testing"

// TS parseProviderOptions(klingaiVideoModelOptionsSchema): nullish members,
// loose object (unknown keys pass through).
func TestProviderOptionsValidation(t *testing.T) {
	for _, o := range []map[string]interface{}{
		{"mode": "ultra"}, {"pollIntervalMs": 0}, {"pollTimeoutMs": "x"}, {"sound": "maybe"}, {"cfgScale": "1"},
		{"cameraControl": map[string]interface{}{}},
		{"cameraControl": map[string]interface{}{"type": "spin"}},
		{"cameraControl": map[string]interface{}{"type": "simple", "config": map[string]interface{}{"pan": "x"}}},
		{"multiShot": 1}, {"shotType": "auto"},
		{"multiPrompt": []interface{}{map[string]interface{}{"index": 1, "prompt": "p"}}},
		{"elementList": []interface{}{map[string]interface{}{"element_id": "1"}}},
		{"voiceList": []interface{}{map[string]interface{}{"voice_id": 1}}},
		{"dynamicMasks": []interface{}{map[string]interface{}{"mask": "m"}}},
		{"dynamicMasks": []interface{}{map[string]interface{}{"mask": "m", "trajectories": []interface{}{map[string]interface{}{"x": 1}}}}},
		{"characterOrientation": "up"}, {"keepOriginalSound": true}, {"watermarkEnabled": "on"},
	} {
		if validateProviderOptions(map[string]interface{}{"klingai": o}) == nil {
			t.Errorf("%v accepted", o)
		}
	}
	for _, o := range []map[string]interface{}{
		{}, {"mode": nil, "pollIntervalMs": nil, "cameraControl": nil, "unknown": []int{1}},
		{"mode": "pro", "pollIntervalMs": 1.5, "sound": "on", "cfgScale": 0.5},
		{"cameraControl": map[string]interface{}{"type": "simple", "config": map[string]interface{}{"pan": 1, "zoom": nil}}},
		{"multiPrompt": []interface{}{map[string]interface{}{"index": 1, "prompt": "p", "duration": "5"}}},
		{"dynamicMasks": []interface{}{map[string]interface{}{"mask": "m", "trajectories": []interface{}{map[string]interface{}{"x": 1, "y": 2}}}}},
	} {
		if err := validateProviderOptions(map[string]interface{}{"klingai": o}); err != nil {
			t.Errorf("%v rejected: %v", o, err)
		}
	}
	if err := validateProviderOptions(map[string]interface{}{"klingai": ProviderOptions{}}); err != nil {
		t.Errorf("typed options rejected: %v", err)
	}
}
