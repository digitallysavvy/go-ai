package fal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// newTestVideoModel builds a Provider whose queueHost points at an httptest
// server, mirroring how fal-video-model.test.ts intercepts
// "https://queue.fal.run/fal-ai/{modelId}" and its "response_url" with
// createTestServer.
func newTestVideoModel(t *testing.T, modelID string, mux *http.ServeMux) (*VideoModel, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	p := New(Config{APIKey: "test-key"})
	p.queueHost = srv.URL
	return NewVideoModel(p, modelID), srv
}

func TestVideoModel_ConstructorInfo(t *testing.T) {
	model, _ := newTestVideoModel(t, "luma-dream-machine", http.NewServeMux())

	if model.Provider() != "fal" {
		t.Errorf("Provider() = %q, want fal", model.Provider())
	}
	if model.ModelID() != "luma-dream-machine" {
		t.Errorf("ModelID() = %q, want luma-dream-machine", model.ModelID())
	}
	if model.SpecificationVersion() != "v3" {
		t.Errorf("SpecificationVersion() = %q, want v3", model.SpecificationVersion())
	}
	if got := model.MaxVideosPerCall(); got == nil || *got != 1 {
		t.Errorf("MaxVideosPerCall() = %v, want 1", got)
	}
}

func TestVideoModel_NormalizedModelID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"luma-dream-machine", "luma-dream-machine"},
		{"fal-ai/kling-video/v2.5-turbo", "kling-video/v2.5-turbo"},
		{"fal/luma-ray-2", "luma-ray-2"},
	}
	for _, tt := range tests {
		m := NewVideoModel(&Provider{}, tt.in)
		if got := m.normalizedModelID(); got != tt.want {
			t.Errorf("normalizedModelID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func videoQueueHandler(t *testing.T, requestID, responseURLPath string, captured *map[string]interface{}, capturedHeaders *http.Header) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if captured != nil {
			*captured = body
		}
		if capturedHeaders != nil {
			*capturedHeaders = r.Header.Clone()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"request_id":   requestID,
			"response_url": "http://" + r.Host + responseURLPath,
		})
	}
}

func TestVideoModel_DoStart_SubmitsToQueue(t *testing.T) {
	var captured map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "test-request-id-123", "/fal-ai/luma-dream-machine/requests/test-request-id-123", &captured, nil))

	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "A futuristic city with flying cars"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("Warnings = %v, want empty", result.Warnings)
	}
	if result.Response.ModelID != "luma-dream-machine" {
		t.Errorf("ModelID = %q", result.Response.ModelID)
	}

	var op falVideoOperation
	if err := json.Unmarshal(result.Operation, &op); err != nil {
		t.Fatalf("failed to unmarshal operation: %v", err)
	}
	if op.ResponseURL == "" {
		t.Error("ResponseURL is empty")
	}

	if captured["prompt"] != "A futuristic city with flying cars" {
		t.Errorf("prompt = %v", captured["prompt"])
	}
}

func TestVideoModel_DoStart_RequestBody(t *testing.T) {
	var captured map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", &captured, nil))

	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	seed := 42
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt:      "A futuristic city with flying cars",
			Seed:        &seed,
			AspectRatio: "16:9",
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}

	want := map[string]interface{}{
		"prompt":       "A futuristic city with flying cars",
		"seed":         float64(42),
		"aspect_ratio": "16:9",
	}
	for k, v := range want {
		if captured[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, captured[k], v)
		}
	}
}

func TestVideoModel_DoStart_DurationFormat(t *testing.T) {
	var captured map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", &captured, nil))

	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	duration := 5.0
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x", Duration: &duration},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if captured["duration"] != "5s" {
		t.Errorf("duration = %v, want \"5s\"", captured["duration"])
	}
}

func TestVideoModel_DoStart_Headers(t *testing.T) {
	var capturedHeaders http.Header
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", nil, &capturedHeaders))

	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt:  "x",
			Headers: map[string]string{"Custom-Request-Header": "request-header-value"},
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if capturedHeaders.Get("Custom-Request-Header") != "request-header-value" {
		t.Errorf("Custom-Request-Header = %q", capturedHeaders.Get("Custom-Request-Header"))
	}
	if capturedHeaders.Get("Authorization") == "" {
		t.Error("Authorization header missing")
	}
}

func TestVideoModel_DoStart_NoResponseURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})

	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err == nil {
		t.Fatal("expected error for missing response URL")
	}
}

func TestVideoModel_DoStart_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid prompt","code":400}}`))
	})

	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVideoModel_DoStart_WebhookQueryParam(t *testing.T) {
	var gotURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"request_id":   "webhook-test-id",
			"response_url": "http://" + r.Host + "/fal-ai/luma-dream-machine/requests/webhook-test-id",
		})
	})

	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
		WebhookURL:              "https://smee.io/abc123",
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if gotURL != "/fal-ai/luma-dream-machine?fal_webhook=https%3A%2F%2Fsmee.io%2Fabc123" {
		t.Errorf("submit URL = %q", gotURL)
	}
}

func TestVideoModel_DoStart_ImageFileAndURL(t *testing.T) {
	mux := http.NewServeMux()
	var captured map[string]interface{}
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", &captured, nil))
	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x",
			Image:  &provider.VideoModelV3File{Type: "file", Data: []byte{137, 80, 78, 71}, MediaType: "image/png"},
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if captured["image_url"] != "data:image/png;base64,iVBORw==" {
		t.Errorf("image_url = %v", captured["image_url"])
	}

	_, err = model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x",
			Image:  &provider.VideoModelV3File{Type: "url", URL: "https://example.com/input-image.png"},
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if captured["image_url"] != "https://example.com/input-image.png" {
		t.Errorf("image_url = %v", captured["image_url"])
	}
}

func TestVideoModel_DoStart_ProviderOptions(t *testing.T) {
	mux := http.NewServeMux()
	var captured map[string]interface{}
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", &captured, nil))
	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	cases := []struct {
		name    string
		options map[string]interface{}
		key     string
		want    interface{}
	}{
		{"loop", map[string]interface{}{"loop": true}, "loop", true},
		{"motionStrength", map[string]interface{}{"motionStrength": 0.8}, "motion_strength", 0.8},
		{"negativePrompt", map[string]interface{}{"negativePrompt": "blurry, low quality"}, "negative_prompt", "blurry, low quality"},
		{"promptOptimizer", map[string]interface{}{"promptOptimizer": true}, "prompt_optimizer", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
				VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
					Prompt:          "x",
					ProviderOptions: map[string]interface{}{"fal": tc.options},
				},
			})
			if err != nil {
				t.Fatalf("DoStart() error = %v", err)
			}
			if captured[tc.key] != tc.want {
				t.Errorf("body[%q] = %v, want %v", tc.key, captured[tc.key], tc.want)
			}
		})
	}

	t.Run("passthrough", func(t *testing.T) {
		_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
			VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
				Prompt: "x",
				ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{
					"custom_param":  "custom_value",
					"another_param": float64(123),
				}},
			},
		})
		if err != nil {
			t.Fatalf("DoStart() error = %v", err)
		}
		if captured["custom_param"] != "custom_value" || captured["another_param"] != float64(123) {
			t.Errorf("passthrough body = %v", captured)
		}
	})
}

func TestVideoModel_DoStatus_Completed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", nil, nil))
	mux.HandleFunc("/fal-ai/luma-dream-machine/requests/id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"video": map[string]interface{}{
				"url":          "https://fal.media/files/video-output.mp4",
				"width":        1920,
				"height":       1080,
				"duration":     5.0,
				"fps":          24,
				"content_type": "video/mp4",
			},
			"seed": 12345,
			"timings": map[string]interface{}{
				"inference": 45.5,
			},
		})
	})
	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	startResult, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}

	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: startResult.Operation})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if status.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("Status = %q, want completed", status.Status)
	}
	if len(status.Videos) != 1 || status.Videos[0].URL != "https://fal.media/files/video-output.mp4" {
		t.Fatalf("Videos = %#v", status.Videos)
	}
	if status.Videos[0].MediaType != "video/mp4" {
		t.Fatalf("MediaType = %q", status.Videos[0].MediaType)
	}
	falMeta, ok := status.ProviderMetadata["fal"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fal provider metadata")
	}
	if falMeta["seed"] != 12345 {
		t.Errorf("seed = %v", falMeta["seed"])
	}
}

func TestVideoModel_DoStatus_Pending(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", nil, nil))
	callCount := 0
	mux.HandleFunc("/fal-ai/luma-dream-machine/requests/id", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"detail":"Request is still in progress"}`))
	})
	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	startResult, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}

	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: startResult.Operation})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if status.Status != provider.VideoOperationStatusPending {
		t.Fatalf("Status = %q, want pending", status.Status)
	}
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1", callCount)
	}
}

func TestVideoModel_DoStatus_MissingVideoURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", nil, nil))
	mux.HandleFunc("/fal-ai/luma-dream-machine/requests/id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	startResult, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}

	_, err = model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: startResult.Operation})
	if err == nil {
		t.Fatal("expected error for missing video URL")
	}
}

func TestVideoModel_DoStatus_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", nil, nil))
	mux.HandleFunc("/fal-ai/luma-dream-machine/requests/id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "Internal server error", "code": 500},
		})
	})
	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	startResult, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}

	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: startResult.Operation})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if status.Status != provider.VideoOperationStatusError {
		t.Fatalf("Status = %q, want error", status.Status)
	}
	if status.Error != "Internal server error" {
		t.Fatalf("Error = %q", status.Error)
	}
}

func TestVideoModel_DoGenerate_EndToEnd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/luma-dream-machine", videoQueueHandler(t, "id", "/fal-ai/luma-dream-machine/requests/id", nil, nil))
	mux.HandleFunc("/fal-ai/luma-dream-machine/requests/id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"video": map[string]interface{}{"url": "https://fal.media/files/video-output.mp4", "content_type": "video/mp4"},
		})
	})
	model, _ := newTestVideoModel(t, "luma-dream-machine", mux)

	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "x",
		ProviderOptions: map[string]interface{}{
			"fal": map[string]interface{}{"pollIntervalMs": 1, "pollTimeoutMs": 5000},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(resp.Videos) != 1 {
		t.Fatalf("Videos = %#v", resp.Videos)
	}
	if resp.Videos[0].URL != "https://fal.media/files/video-output.mp4" {
		t.Fatalf("URL = %q", resp.Videos[0].URL)
	}
}

func TestVideoModel_DoGenerate_MissingRequestID(t *testing.T) {
	// Use a provider whose queueHost is an invalid address so submit fails quickly.
	prov := New(Config{APIKey: "k"})
	prov.queueHost = "http://127.0.0.1:1"
	model := NewVideoModel(prov, "luma-ray")
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "hello"})
	if err == nil {
		t.Fatal("DoGenerate should return provider error when submit fails")
	}
}

// TestVideoModel_GetPollOptions_JSONNumbers is a regression test:
// getPollOptions read pollIntervalMs/pollTimeoutMs via a bare `.(int)` type
// assertion. ProviderOptions built via encoding/json.Unmarshal (a JSON
// config file, or a request body forwarded straight into ProviderOptions)
// decodes numbers as float64, so the assertion silently failed and the
// user's poll interval/timeout was dropped in favor of the hardcoded
// default, with no warning.
func TestVideoModel_GetPollOptions_JSONNumbers(t *testing.T) {
	m := &VideoModel{}
	raw := []byte(`{"fal":{"pollIntervalMs":111,"pollTimeoutMs":222}}`)
	var providerOpts map[string]interface{}
	if err := json.Unmarshal(raw, &providerOpts); err != nil {
		t.Fatal(err)
	}
	got := m.getPollOptions(providerOpts)
	if got.PollIntervalMs != 111 || got.PollTimeoutMs != 222 {
		t.Fatalf("JSON-sourced poll options were dropped: got PollIntervalMs=%d PollTimeoutMs=%d, want 111/222", got.PollIntervalMs, got.PollTimeoutMs)
	}
}
