package replicate

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ports of the doStart input-mapping tests in
// packages/replicate/src/replicate-video-model.test.ts.
func TestVideoModel_DoStart_InputMapping(t *testing.T) {
	dur, fps, seed := 6.0, 24, 42
	cases := []struct {
		name string
		opts provider.VideoModelV3CallOptions
		want map[string]interface{}
	}{
		{"prompt only", provider.VideoModelV3CallOptions{Prompt: "p"}, map[string]interface{}{"prompt": "p"}},
		{"seed", provider.VideoModelV3CallOptions{Prompt: "p", Seed: &seed}, map[string]interface{}{"prompt": "p", "seed": float64(42)}},
		{"aspect ratio 16:9", provider.VideoModelV3CallOptions{Prompt: "p", AspectRatio: "16:9"}, map[string]interface{}{"prompt": "p", "aspect_ratio": "16:9"}},
		{"aspect ratio 9:16", provider.VideoModelV3CallOptions{Prompt: "p", AspectRatio: "9:16"}, map[string]interface{}{"prompt": "p", "aspect_ratio": "9:16"}},
		{"resolution becomes size", provider.VideoModelV3CallOptions{Prompt: "p", Resolution: "1920x1080"}, map[string]interface{}{"prompt": "p", "size": "1920x1080"}},
		{"duration", provider.VideoModelV3CallOptions{Prompt: "p", Duration: &dur}, map[string]interface{}{"prompt": "p", "duration": float64(6)}},
		{"fps", provider.VideoModelV3CallOptions{Prompt: "p", FPS: &fps}, map[string]interface{}{"prompt": "p", "fps": float64(24)}},
		{"url image sent directly", provider.VideoModelV3CallOptions{Prompt: "p", Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/image.png"}},
			map[string]interface{}{"prompt": "p", "image": "https://example.com/image.png"}},
		{"file image becomes a data URI", provider.VideoModelV3CallOptions{Prompt: "p", Image: &provider.VideoModelV3File{Type: "file", Data: []byte("hi"), MediaType: "image/png"}},
			map[string]interface{}{"prompt": "p", "image": "data:image/png;base64,aGk="}},
		{"provider options", provider.VideoModelV3CallOptions{Prompt: "p", ProviderOptions: map[string]interface{}{"replicate": map[string]interface{}{
			"guidance_scale": 7.5, "num_inference_steps": 30, "motion_bucket_id": 127, "prompt_optimizer": true, "custom_param": "custom_value",
			"pollIntervalMs": 10, "pollTimeoutMs": 20, "maxWaitTimeInSeconds": 30,
		}}}, map[string]interface{}{"prompt": "p", "guidance_scale": 7.5, "num_inference_steps": float64(30), "motion_bucket_id": float64(127), "prompt_optimizer": true, "custom_param": "custom_value"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]interface{}
			var gotPrefer string
			mux := http.NewServeMux()
			mux.HandleFunc("/models/minimax/video-01/predictions", func(w http.ResponseWriter, r *http.Request) {
				gotPrefer = r.Header.Get("Prefer")
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"id": "pred-1", "status": "starting",
					"urls": map[string]interface{}{"get": "http://" + r.Host + "/predictions/pred-1"},
				})
			})
			model, _ := newTestVideoModelServer(t, mux)
			if _, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: tc.opts}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotBody["input"], tc.want) {
				t.Errorf("input = %#v\nwant   %#v", gotBody["input"], tc.want)
			}
			if gotPrefer != "" {
				t.Errorf("Prefer header = %q, want none", gotPrefer)
			}
		})
	}
}
