package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// TS GoogleVideoModel has no doGenerate, so TS generateVideo always runs the
// core start/status flow (generate-video.ts useStartStatus): one
// predictLongRunning POST with an aisdk_vid_ idempotency key, and an HTTP 500
// on the operation GET is retried without a second, billable start.
func TestR89provgGoogleGenerateVideoUsesCoreStartStatusFlow(t *testing.T) {
	var mu sync.Mutex
	var posts []http.Header
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts = append(posts, r.Header.Clone())
			_, _ = w.Write([]byte(`{"name":"operations/op-1"}`))
			return
		}
		gets++
		if gets == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":500,"message":"busy","status":"INTERNAL"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"operations/op-1","done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"https://cdn.example/v.mp4"}}]}}}`))
	}))
	defer srv.Close()

	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "veo-3.0-generate-001")
	result, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model:           model,
		Prompt:          ai.VideoPrompt{Text: "a paper boat"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": 1}},
		Download:        func(context.Context, string) ([]byte, error) { return []byte("video"), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 1 {
		t.Fatalf("POST count = %d, want 1 (status retried without restarting)", len(posts))
	}
	if key := posts[0].Get("Idempotency-Key"); !strings.HasPrefix(key, "aisdk_vid_") {
		t.Fatalf("start idempotency-key = %q, want an aisdk_vid_ key", key)
	}
	if gets != 2 || len(result.Videos) != 1 {
		t.Fatalf("GET count %d, videos %d", gets, len(result.Videos))
	}
}
