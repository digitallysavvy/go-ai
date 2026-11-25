package topaz

import "testing"

// TS parseProviderOptions(topazImage/VideoModelOptionsSchema).
func TestProviderOptionsValidation(t *testing.T) {
	image := func(o map[string]interface{}) error {
		return validateImageOptions(map[string]interface{}{"topaz": o})
	}
	video := func(o map[string]interface{}) error {
		return validateVideoOptions(map[string]interface{}{"topaz": o})
	}
	for _, o := range []map[string]interface{}{
		{"enhancementStrength": "max"}, {"grainDensity": 2}, {"grainSize": 0}, {"outputWidth": 40000},
		{"outputFormat": "gif"}, {"pollIntervalMillis": 0}, {"inputWidth": 1.5}, {"webhookUrl": nil}, {"grain": "on"},
	} {
		if image(o) == nil {
			t.Errorf("image %v accepted", o)
		}
	}
	for _, o := range []map[string]interface{}{
		{"compression": 2}, {"prenoise": 0.5}, {"videoCodec": "h264"}, {"watermark": 1},
		{"source": map[string]interface{}{"container": "gif"}},
		{"output": map[string]interface{}{"audioCodec": "MP3"}},
		{"additionalFilters": []interface{}{"x"}}, {"filter": "x"}, {"sharpness": 6},
	} {
		if video(o) == nil {
			t.Errorf("video %v accepted", o)
		}
	}
	if err := image(map[string]interface{}{"enhancementStrength": "low", "outputWidth": 100, "grainSize": 5, "pollTimeoutMillis": 10}); err != nil {
		t.Errorf("image valid rejected: %v", err)
	}
	if err := video(map[string]interface{}{"compression": -1, "videoProfile": "422",
		"source": map[string]interface{}{"container": "mp4", "width": 10},
		"filter": map[string]interface{}{"a": 1}, "additionalFilters": []interface{}{map[string]interface{}{"b": 2}}}); err != nil {
		t.Errorf("video valid rejected: %v", err)
	}
	if err := validateImageOptions(map[string]interface{}{"topaz": ImageModelOptions{}}); err != nil {
		t.Errorf("typed options rejected: %v", err)
	}
}
