package klingai

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestKlingAIProviderUnsupportedMethodsAndClient(t *testing.T) {
	p := &Provider{}
	if p.Name() != "klingai" {
		t.Fatalf("Name() = %q, want klingai", p.Name())
	}
	if _, err := p.LanguageModel("x"); err == nil {
		t.Fatal("LanguageModel should be unsupported")
	}
	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("EmbeddingModel should be unsupported")
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("ImageModel should be unsupported")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel should be unsupported")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel should be unsupported")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel should be unsupported")
	}
	if _, err := p.VideoModel(""); err == nil {
		t.Fatal("VideoModel should require model ID")
	}
	if p.Client() != nil {
		t.Fatal("Client() should be nil on zero-value provider")
	}
}

func TestKlingAIVideoModelHelperBranches(t *testing.T) {
	mode := VideoModeT2V
	model := &VideoModel{
		prov:    &Provider{},
		modelID: "kling-v2.6-t2v",
		mode:    mode,
	}

	if _, _, err := model.buildRequestBody(&provider.VideoModelV3CallOptions{Prompt: "hello"}, &ProviderOptions{}); err != nil {
		t.Fatalf("buildRequestBody(t2v) error = %v", err)
	}

	interval := 25
	timeout := 50
	poll := model.getPollOptions(&ProviderOptions{
		PollIntervalMs: &interval,
		PollTimeoutMs:  &timeout,
	})
	if poll.PollIntervalMs != interval || poll.PollTimeoutMs != timeout {
		t.Fatalf("getPollOptions() = %#v", poll)
	}

	headers := convertHTTPHeaders(http.Header{
		"X-Request-Id": {"req-1"},
		"X-Multi":      {"a", "b"},
	})
	if headers["X-Request-Id"] != "req-1" || headers["X-Multi"] != "a" {
		t.Fatalf("convertHTTPHeaders() = %#v", headers)
	}
	if empty := convertHTTPHeaders(nil); len(empty) != 0 {
		t.Fatalf("convertHTTPHeaders(nil) = %#v", empty)
	}

	var status taskStatusResponse
	err := json.Unmarshal([]byte(`{
		"code": 0,
		"message": "ok",
		"data": {
			"task_id": "task-1",
			"task_status": "succeed",
			"task_result": {
				"videos": [
					{"id": "v1", "url": "https://example.com/v1.mp4", "watermark_url": "https://example.com/w.mp4", "duration": "5"}
				]
			}
		}
	}`), &status)
	if err != nil {
		t.Fatalf("unmarshal status error = %v", err)
	}

	resp := model.convertResponse(&status, "task-1", nil, time.Unix(10, 0), map[string]string{"X-Req": "abc"})
	if len(resp.Videos) != 1 || resp.Videos[0].URL == "" {
		t.Fatalf("convertResponse() videos = %#v", resp.Videos)
	}
	if resp.Response.Headers["X-Req"] != "abc" {
		t.Fatalf("convertResponse headers = %#v", resp.Response.Headers)
	}
}
