package klingai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// TS KlingAIVideoModel has no doGenerate, so TS generateVideo always runs the
// core start/status flow (generate-video.ts useStartStatus): the start POST
// carries one idempotency key and an HTTP 500 on the status poll retries the
// GET (TS APICallError.isRetryable) instead of starting a second, billable
// task. Before round 89 ai.GenerateVideo used the DoGenerate shim: no
// idempotency key, and the core retry re-ran DoGenerate (a second POST).
func TestR89provcGenerateVideoUsesCoreStartStatusFlow(t *testing.T) {
	var mu sync.Mutex
	var posts []http.Header
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			posts = append(posts, r.Header.Clone())
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1"}}`))
			return
		}
		gets++
		if gets == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":5000,"message":"busy"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1","task_status":"succeed","task_result":{"videos":[{"id":"v1","url":"https://cdn.example/v.mp4"}]}}}`))
	}))
	defer server.Close()

	prov, err := New(Config{APIKey: "key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := newVideoModel(prov, "kling-v2.6-t2v")
	result, err := ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model:  model,
		Prompt: ai.VideoPrompt{Text: "a cat"},
		// v0.5.2 provider poll setting, still honored (TS ignores it).
		ProviderOptions: map[string]interface{}{"klingai": map[string]interface{}{"pollIntervalMs": 1}},
		Download: func(context.Context, string) ([]byte, error) {
			return []byte("video"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("POST count = %d, want 1 (status retried without restarting the task)", len(posts))
	}
	if key := posts[0].Get("Idempotency-Key"); !strings.HasPrefix(key, "aisdk_vid_") {
		t.Fatalf("start idempotency-key = %q, want an aisdk_vid_ key", key)
	}
	if gets != 2 || len(result.Videos) != 1 {
		t.Fatalf("GET count %d, videos %d", gets, len(result.Videos))
	}
}

// A failed task fails with TS's core error text (new Error(statusResult.error)).
func TestR89provcGenerateVideoFailedTaskText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1","task_status":"failed","task_status_msg":"blocked"}}`))
	}))
	defer server.Close()
	prov, err := New(Config{APIKey: "key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := newVideoModel(prov, "kling-v2.6-t2v")
	_, err = ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model:           model,
		Prompt:          ai.VideoPrompt{Text: "a cat"},
		ProviderOptions: map[string]interface{}{"klingai": map[string]interface{}{"pollIntervalMs": 1}},
	})
	if err == nil || err.Error() != "Video generation failed: blocked" {
		t.Fatalf("err = %v, want TS text", err)
	}
}
