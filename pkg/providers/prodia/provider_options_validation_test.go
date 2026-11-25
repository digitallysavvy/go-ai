package prodia

import "testing"

// TS parseProviderOptions(prodiaImage/Language/VideoModelOptionsSchema).
func TestProviderOptionsValidation(t *testing.T) {
	image := func(o map[string]interface{}) error { return validateImageOptions(map[string]interface{}{"prodia": o}) }
	for _, o := range []map[string]interface{}{
		{"steps": 5}, {"steps": 0}, {"width": 100}, {"height": 2000}, {"width": 300.5},
		{"stylePreset": "oil"}, {"loras": []interface{}{"a", "b", "c", "d"}}, {"loras": []interface{}{1}},
		{"progressive": "yes"}, {"steps": nil},
	} {
		if image(o) == nil {
			t.Errorf("image %v accepted", o)
		}
	}
	if err := image(map[string]interface{}{"steps": 4, "width": 256, "height": 1920, "stylePreset": "craft-clay",
		"loras": []interface{}{"a"}, "progressive": true}); err != nil {
		t.Errorf("image valid rejected: %v", err)
	}
	if validateLanguageOptions(map[string]interface{}{"prodia": map[string]interface{}{"aspectRatio": "3:1"}}) == nil {
		t.Error("language aspectRatio accepted")
	}
	if err := validateLanguageOptions(map[string]interface{}{"prodia": map[string]interface{}{"aspectRatio": "21:9"}}); err != nil {
		t.Error(err)
	}
	if validateVideoOptions(map[string]interface{}{"prodia": map[string]interface{}{"resolution": 720}}) == nil {
		t.Error("video resolution number accepted")
	}
	if err := validateImageOptions(map[string]interface{}{"prodia": ProdiaImageProviderOptions{}}); err != nil {
		t.Errorf("typed options rejected: %v", err)
	}
}
