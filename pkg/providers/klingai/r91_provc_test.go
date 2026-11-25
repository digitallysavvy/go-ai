package klingai

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR91provcI2VBodyOrderAndHeaders (provc-07, provc-06): the image2video
// body keeps the TS builder order (duration after watermark_info,
// passthrough keys, then callback_url), and repeated response headers are
// joined with ", ".
func TestR91provcI2VBodyOrderAndHeaders(t *testing.T) {
	var mu sync.Mutex
	var raw string
	model, server := newKlingAITestModel(t, "kling-v2.6-i2v", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = string(b)
		mu.Unlock()
		w.Header().Add("X-Test", "a")
		w.Header().Add("X-Test", "b")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"ok","data":{"task_id":"t1","task_status":"submitted"}}`)
	})
	defer server.Close()
	d := 5.0
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x", Duration: &d,
			Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/a.png"},
			ProviderOptions: map[string]interface{}{"klingai": map[string]interface{}{
				"negativePrompt": "dogs", "watermarkEnabled": true, "mode": "pro", "zeta": 1,
			}},
		},
		WebhookURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := raw
	mu.Unlock()
	if want := `{"model_name":"kling-v2-6","prompt":"x","image":"https://example.com/a.png","negative_prompt":"dogs","mode":"pro","watermark_info":{"enabled":true},"duration":"5","zeta":1,"callback_url":"https://example.com/hook"}`; got != want {
		t.Errorf("body = %s\nwant %s", got, want)
	}
	if h := start.Response.Headers["X-Test"]; h != "a, b" {
		t.Errorf("X-Test = %q", h)
	}
}
