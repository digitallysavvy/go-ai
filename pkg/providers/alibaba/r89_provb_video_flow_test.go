package alibaba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// provc CR r89 (test b, add after merging provc 17b0083c): TS AlibabaVideoModel has no
// doGenerate, so TS generateVideo always runs the core start/status flow
// (generate-video.ts useStartStatus): the start POST carries one idempotency
// key and an HTTP 500 on the status poll retries the GET instead of starting
// a second, billable job.
func TestR89provbGenerateVideoUsesCoreStartStatusFlow(t *testing.T) {
	var mu sync.Mutex
	var posts []http.Header
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts = append(posts, r.Header.Clone())
			_, _ = w.Write([]byte(`{"output":{"task_status":"PENDING","task_id":"t1"},"request_id":"q"}`))
			return
		}
		gets++
		if gets == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"busy"}`))
			return
		}
		_, _ = w.Write([]byte(`{"output":{"task_id":"t1","task_status":"SUCCEEDED","video_url":"https://cdn.example/v.mp4"}}`))
	}))
	defer server.Close()

	model := NewVideoModel(New(Config{APIKey: "k", VideoBaseURL: server.URL}), "wan2.6-t2v")
	result, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model:           model,
		Prompt:          ai.VideoPrompt{Text: "a cat"},
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"pollIntervalMs": 1}},
		Download: func(context.Context, string) ([]byte, error) {
			return []byte("video"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("POST count = %d, want 1 (status retried without restarting the job)", len(posts))
	}
	if key := posts[0].Get("Idempotency-Key"); !strings.HasPrefix(key, "aisdk_vid_") {
		t.Fatalf("start idempotency-key = %q, want an aisdk_vid_ key", key)
	}
	if gets != 2 || len(result.Videos) != 1 {
		t.Fatalf("GET count %d, videos %d", gets, len(result.Videos))
	}
}
