package minimax

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS minimax-video-model.ts 549-581: postJsonToApi stringifies
// {...getArgs body, callback_url} in insertion order ({model, content,
// resolution, duration, ratio, aigc_watermark}); Go sorted the map keys.
func TestR90toolsMiniMaxVideoBodyKeyOrder(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"t1"}`))
	}))
	defer srv.Close()
	model := newVideoModel(New(Config{APIKey: "k", VideoBaseURL: srv.URL}), ModelH3)
	opts := &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "a cat", PromptSet: true, N: 1,
			ProviderOptions: map[string]interface{}{"minimax": map[string]interface{}{"aigcWatermark": true}},
		},
		WebhookURL: "https://example.com/hook",
	}
	if _, err := model.DoStart(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	want := `{"model":"MiniMax-H3","content":[{"type":"text","text":"a cat"}],"resolution":"2K","duration":5,"ratio":"16:9","aigc_watermark":true,"callback_url":"https://example.com/hook"}`
	if body != want {
		t.Errorf("body\n got %s\nwant %s", body, want)
	}
}
