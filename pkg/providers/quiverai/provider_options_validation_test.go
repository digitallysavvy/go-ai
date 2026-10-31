package quiverai

import "testing"

// TS parseProviderOptions(quiveraiImageModelOptionsSchema): .optional()
// members reject null, referenceImages entries are strict unions.
func TestImageProviderOptionsValidation(t *testing.T) {
	bad := []map[string]interface{}{
		{"operation": nil}, {"operation": "draw"}, {"instructions": ""}, {"temperature": nil}, {"temperature": 3},
		{"topP": -1}, {"maxReviewSteps": 6}, {"maxOutputTokens": 0}, {"targetSize": 100}, {"autoCrop": 1},
		{"referenceImages": []interface{}{map[string]interface{}{"url": "u", "base64": "b"}}},
		{"referenceImages": []interface{}{map[string]interface{}{"url": "u", "extra": 1}}},
		{"referenceImages": []interface{}{map[string]interface{}{"url": ""}}},
		{"referenceImages": []interface{}{map[string]interface{}{"base64": ""}}},
		{"referenceImages": []interface{}{map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}}},
		{"attributes": map[string]interface{}{"viewBox": map[string]interface{}{"minX": 0, "minY": 0, "width": 0, "height": 1}}},
		{"attributes": map[string]interface{}{"viewBox": map[string]interface{}{"minX": 0}}},
		{"shallowMaxOutputTokens": 70000},
	}
	for _, o := range bad {
		if validateImageOptions(map[string]interface{}{"quiverai": o}) == nil {
			t.Errorf("%v accepted", o)
		}
	}
	good := []map[string]interface{}{
		{},
		{"presencePenalty": nil, "operation": "edit", "reasoningEffort": "xhigh", "temperature": 2, "targetSize": 4096},
		{"referenceImages": []interface{}{map[string]interface{}{"url": "u"}, map[string]interface{}{"base64": "b"}}},
		{"attributes": map[string]interface{}{"viewBox": map[string]interface{}{"minX": -1, "minY": 0, "width": 2, "height": 3}}},
	}
	for _, o := range good {
		if err := validateImageOptions(map[string]interface{}{"quiverai": o}); err != nil {
			t.Errorf("%v rejected: %v", o, err)
		}
	}
}

// Go-typed nested values read like their JSON form (they were validated in
// that form, so parsing must not reject or drop them).
func TestQuiverRawOptionsNormalizesGoTypedValues(t *testing.T) {
	po := map[string]interface{}{"quiverai": map[string]interface{}{
		"referenceImages": []map[string]string{{"url": "https://example.com/a.png"}},
		"maxReviewSteps":  3,
		"temperature":     float32(0.5),
	}}
	if err := validateImageOptions(po); err != nil {
		t.Fatal(err)
	}
	opts, err := parseQuiverAIImageModelOptions(quiverRawOptions(po))
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.ReferenceImages) != 1 || opts.ReferenceImages[0].URL != "https://example.com/a.png" || opts.MaxReviewSteps == nil || *opts.MaxReviewSteps != 3 {
		t.Fatalf("opts = %+v", opts)
	}
}
