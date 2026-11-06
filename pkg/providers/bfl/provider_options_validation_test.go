package bfl

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestBFLImageOptionsAreValidatedLikeTheTSSchemas(t *testing.T) {
	bad := []struct {
		name  string
		model string
		opts  map[string]interface{}
	}{
		{"width below minimum", "flux-pro-1.1", map[string]interface{}{"width": 100}},
		{"height above maximum", "flux-pro-1.1", map[string]interface{}{"height": 4000}},
		{"steps not positive", "flux-pro-1.1", map[string]interface{}{"steps": 0}},
		{"guidance negative", "flux-pro-1.1", map[string]interface{}{"guidance": -1.0}},
		{"strength above one", "flux-pro-1.1", map[string]interface{}{"imagePromptStrength": 1.5}},
		{"bad output format", "flux-pro-1.1", map[string]interface{}{"outputFormat": "webp"}},
		{"safety above six", "flux-pro-1.1", map[string]interface{}{"safetyTolerance": 7}},
		{"flux3 safety above seven", flux3ImageModelID, map[string]interface{}{"safetyTolerance": 8}},
		{"klein safety above seven", "flux-3-image-klein", map[string]interface{}{"safetyTolerance": 8}},
		{"webhook url", "flux-pro-1.1", map[string]interface{}{"webhookUrl": "not a url"}},
		{"raw as string", "flux-pro-1.1", map[string]interface{}{"raw": "yes"}},
		{"null option", "flux-pro-1.1", map[string]interface{}{"steps": nil}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			err := validateBFLImageOptions(c.model, map[string]interface{}{"blackForestLabs": c.opts})
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) {
				t.Fatalf("err = %v, want InvalidArgumentError", err)
			}
		})
	}
	good := map[string]interface{}{"width": 256, "height": 1920, "steps": 4, "guidance": 0, "safetyTolerance": 6,
		"webhookUrl": "https://example.com/hook", "outputFormat": "png", "imagePromptStrength": 1, "pollIntervalMillis": 10}
	if err := validateBFLImageOptions("flux-pro-1.1", map[string]interface{}{"blackForestLabs": good}); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
}

func TestBFLVideoOptionsAreValidatedLikeTheTSSchemas(t *testing.T) {
	bad := []map[string]interface{}{
		{"resolution": "4k"},
		{"aspectRatio": "5:4"},
		{"keyframes": []interface{}{}},
		{"keyframes": []interface{}{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}},
		{"keyframes": []interface{}{[]interface{}{2.0, "a"}, []interface{}{1.0, "b"}}},
		{"keyframes": []interface{}{[]interface{}{21.0, "a"}}},
		{"keyframes": []interface{}{"a", []interface{}{1.0, "b"}}},
		{"safetyTolerance": 5},
		{"draft": "yes"},
		{"version": "v1"},
	}
	for i, opts := range bad {
		if err := validateBFLVideoOptions(map[string]interface{}{"blackForestLabs": opts}); err == nil {
			t.Fatalf("case %d (%v) accepted", i, opts)
		}
	}
	good := []map[string]interface{}{
		{"resolution": "fhd", "aspectRatio": "auto", "safetyTolerance": 0, "draft": true, "version": "latest"},
		{"keyframes": []interface{}{"a", "b"}},
		{"keyframes": []interface{}{[]interface{}{0, "a"}, []interface{}{5.5, "b"}}},
	}
	for i, opts := range good {
		if err := validateBFLVideoOptions(map[string]interface{}{"blackForestLabs": opts}); err != nil {
			t.Fatalf("good case %d rejected: %v", i, err)
		}
	}
}
