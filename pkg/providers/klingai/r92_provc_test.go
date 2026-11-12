package klingai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR92provcDurationIsJSString (r92 provc-10): TS String(options.duration)
// for text-to-video, image-to-video and multi-image bodies.
func TestR92provcDurationIsJSString(t *testing.T) {
	d := 1e21
	for _, tc := range []struct {
		modelID string
		mode    VideoMode
		opts    provider.VideoModelV3CallOptions
	}{
		{"kling-v2.6-t2v", VideoModeT2V, provider.VideoModelV3CallOptions{Prompt: "x", Duration: &d}},
		{"kling-v2.6-i2v", VideoModeI2V, provider.VideoModelV3CallOptions{Prompt: "x", Duration: &d, Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/a.png"}}},
	} {
		model := &VideoModel{prov: &Provider{}, modelID: tc.modelID, mode: tc.mode}
		provOpts, _ := extractProviderOptions(nil)
		body, _, _, err := model.buildRequestBody(&tc.opts, provOpts)
		if err != nil {
			t.Fatal(err)
		}
		if body["duration"] != "1e+21" {
			t.Errorf("%s: duration = %#v", tc.modelID, body["duration"])
		}
	}
}
