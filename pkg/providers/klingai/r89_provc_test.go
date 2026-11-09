package klingai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// TS klingaiFailedResponseHandler gives an APICallError with responseHeaders,
// responseBody and data, so the core retry waits retry-after-ms. Go's
// *Error now unwraps to that ProviderError; before, the headers were dropped
// and a 429 retry waited the 2 s exponential delay.
func TestR89provcErrorCarriesResponseHeaders(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			posts++
			if posts == 1 {
				w.Header().Set("Retry-After-Ms", "10")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"code":1302,"message":"slow down"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1"}}`))
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

	no := 0
	_, err = ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{Model: model, Prompt: ai.VideoPrompt{Text: "a"}, MaxRetries: &no})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err %T %v: want a ProviderError in the chain", err, err)
	}
	if pe.StatusCode != 429 || pe.ResponseHeaders["Retry-After-Ms"] != "10" || pe.ResponseBody != `{"code":1302,"message":"slow down"}` || pe.Message != "slow down" {
		t.Fatalf("provider error = %+v", pe)
	}
	if data, _ := pe.Data.(map[string]interface{}); data["message"] != "slow down" {
		t.Fatalf("data = %v", pe.Data)
	}
	var ke *Error
	if !errors.As(err, &ke) || ke.Code != 1302 {
		t.Fatalf("klingai error not kept: %v", err)
	}

	start := time.Now()
	_, err = ai.GenerateVideo(context.Background(), ai.GenerateVideoOptions{
		Model: model, Prompt: ai.VideoPrompt{Text: "a"},
		ProviderOptions: map[string]interface{}{"klingai": map[string]interface{}{"pollIntervalMs": 1}},
		Download:        func(context.Context, string) ([]byte, error) { return []byte("v"), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("retry waited %v: retry-after-ms 10 was ignored", elapsed)
	}
}
