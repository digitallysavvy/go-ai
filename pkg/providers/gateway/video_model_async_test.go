package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func newAsyncTestVideoModel(t *testing.T, handler http.HandlerFunc) (*VideoModel, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	p, err := New(Config{APIKey: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return NewVideoModel(p, "google/veo-3.1"), server
}

func baseAsyncCallOptions() provider.VideoModelV3CallOptions {
	return provider.VideoModelV3CallOptions{Prompt: "A beautiful sunset over mountains", N: 1}
}

// TestVideoModel_DoStart mirrors gateway-video-model.test.ts "doStart".
func TestVideoModel_DoStart(t *testing.T) {
	var capturedBody map[string]interface{}
	var capturedHeaders http.Header

	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/video-model/start" {
			t.Fatalf("path = %s, want /video-model/start", r.URL.Path)
		}
		capturedHeaders = r.Header
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"operation": map[string]interface{}{"gatewayJobId": "job_123"},
			"warnings":  []map[string]interface{}{{"type": "other", "message": "queued"}},
			"providerMetadata": map[string]interface{}{
				"gateway": map[string]interface{}{"asyncJob": map[string]interface{}{"jobId": "job_123", "status": "queued"}},
			},
		})
	})
	defer server.Close()

	callOpts := baseAsyncCallOptions()
	aspectRatio := "16:9"
	callOpts.AspectRatio = aspectRatio
	duration := 5.0
	callOpts.Duration = &duration
	fps := 24
	callOpts.FPS = &fps
	seed := 123
	callOpts.Seed = &seed
	generateAudio := true
	callOpts.GenerateAudio = &generateAudio
	callOpts.ProviderOptions = map[string]interface{}{"gateway": map[string]interface{}{"tags": []interface{}{"async"}}}
	callOpts.Resolution = "1280x720"

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: callOpts,
		WebhookURL:              "https://example.com/webhook",
	})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}

	if string(result.Operation) != `{"gatewayJobId":"job_123"}` {
		t.Errorf("Operation = %s", result.Operation)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Message != "queued" {
		t.Errorf("Warnings = %+v", result.Warnings)
	}
	if result.Response.ModelID != "google/veo-3.1" {
		t.Errorf("ModelID = %s", result.Response.ModelID)
	}

	if capturedHeaders.Get("ai-video-model-specification-version") != "4" {
		t.Errorf("missing spec version header: %v", capturedHeaders)
	}
	if capturedBody["callbackUrl"] != "https://example.com/webhook" {
		t.Errorf("callbackUrl = %v, want webhook URL", capturedBody["callbackUrl"])
	}
	if capturedBody["aspectRatio"] != "16:9" {
		t.Errorf("aspectRatio = %v", capturedBody["aspectRatio"])
	}
	if capturedBody["generateAudio"] != true {
		t.Errorf("generateAudio = %v", capturedBody["generateAudio"])
	}
}

func TestVideoModel_DoStart_OmitsCallbackUrlWithoutWebhook(t *testing.T) {
	var capturedBody map[string]interface{}
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"operation": map[string]interface{}{"gatewayJobId": "job_123"}})
	})
	defer server.Close()

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: baseAsyncCallOptions()})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	if _, ok := capturedBody["callbackUrl"]; ok {
		t.Errorf("callbackUrl should be omitted, body = %v", capturedBody)
	}
	if result.Warnings == nil || len(result.Warnings) != 0 {
		t.Errorf("Warnings = %+v, want empty slice when omitted by gateway", result.Warnings)
	}
}

func TestVideoModel_DoStart_ForwardsIdempotencyKey(t *testing.T) {
	var capturedHeaders http.Header
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"operation": map[string]interface{}{"gatewayJobId": "job_123"}})
	})
	defer server.Close()

	callOpts := baseAsyncCallOptions()
	callOpts.Headers = map[string]string{"idempotency-key": "aisdk_vid_abc123"}
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: callOpts})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	if capturedHeaders.Get("idempotency-key") != "aisdk_vid_abc123" {
		t.Errorf("idempotency-key = %q, want aisdk_vid_abc123", capturedHeaders.Get("idempotency-key"))
	}
}

func TestVideoModel_DoStart_MapsErrors(t *testing.T) {
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{"message": "Invalid request"}})
	})
	defer server.Close()

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: baseAsyncCallOptions()})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestVideoModel_DoStatus mirrors gateway-video-model.test.ts "doStatus".
func TestVideoModel_DoStatus_Pending(t *testing.T) {
	var capturedBody map[string]interface{}
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/video-model/status" {
			t.Fatalf("path = %s, want /video-model/status", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "pending",
			"providerMetadata": map[string]interface{}{
				"gateway": map[string]interface{}{"asyncJob": map[string]interface{}{"jobId": "job_123", "status": "running"}},
			},
		})
	})
	defer server.Close()

	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"gatewayJobId":"job_123"}`)})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusPending {
		t.Errorf("Status = %s, want pending", result.Status)
	}
	if capturedBody["operation"] == nil {
		t.Errorf("operation not forwarded: %v", capturedBody)
	}
}

func TestVideoModel_DoStatus_Completed(t *testing.T) {
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "completed",
			"videos": []map[string]interface{}{
				{"type": "url", "url": "https://cdn.example.com/v.mp4", "mediaType": "video/mp4"},
			},
			"warnings": []map[string]interface{}{{"type": "other", "message": "complete"}},
		})
	})
	defer server.Close()

	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"gatewayJobId":"job_123"}`)})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusCompleted {
		t.Errorf("Status = %s, want completed", result.Status)
	}
	if len(result.Videos) != 1 || result.Videos[0].URL != "https://cdn.example.com/v.mp4" {
		t.Errorf("Videos = %+v", result.Videos)
	}
}

func TestVideoModel_DoStatus_Error(t *testing.T) {
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "error": "Async video job failed"})
	})
	defer server.Close()

	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusError || result.Error != "Async video job failed" {
		t.Errorf("result = %+v", result)
	}
}

func TestVideoModel_DoStatus_CancelledMapsToError(t *testing.T) {
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "cancelled"})
	})
	defer server.Close()

	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusError || result.Error != "Video generation was cancelled." {
		t.Errorf("result = %+v", result)
	}
}

func TestVideoModel_DoStatus_MapsErrors(t *testing.T) {
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{"message": "Unauthorized"}})
	})
	defer server.Close()

	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVideoModel_HandleWebhookOption_PassesThrough(t *testing.T) {
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {})
	defer server.Close()

	factoryCalled := false
	factory := func(ctx context.Context) (string, provider.VideoWebhookReceived, error) {
		factoryCalled = true
		return "https://example.com/hook", func(context.Context) (*provider.VideoOperationWebhook, error) {
			return &provider.VideoOperationWebhook{Body: json.RawMessage(`{"type":"video.generation.completed"}`)}, nil
		}, nil
	}

	url, received, err := model.HandleWebhookOption(context.Background(), factory)
	if err != nil {
		t.Fatalf("HandleWebhookOption() error: %v", err)
	}
	if !factoryCalled {
		t.Fatal("expected webhook factory to be called")
	}
	if url != "https://example.com/hook" {
		t.Errorf("url = %s", url)
	}
	if received == nil {
		t.Fatal("expected received callback")
	}
}

// TestVideoModel_DoStart_EncodesFrameImagesAndInputReferences mirrors the TS
// "frameImages and inputReferences encoding" doGenerate tests, applied to the
// shared buildRequestBody used by DoStart.
func TestVideoModel_DoStart_EncodesFrameImagesAndInputReferences(t *testing.T) {
	var capturedBody map[string]interface{}
	model, server := newAsyncTestVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"operation": map[string]interface{}{"id": 1}})
	})
	defer server.Close()

	callOpts := baseAsyncCallOptions()
	callOpts.FrameImages = []provider.VideoFrameImage{
		{FrameType: provider.VideoFrameTypeFirstFrame, Image: provider.VideoModelV3File{Type: "file", MediaType: "image/png", Data: []byte("Hello")}},
		{FrameType: provider.VideoFrameTypeLastFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/last-frame.png"}},
	}

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: callOpts})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}

	frameImages, ok := capturedBody["frameImages"].([]interface{})
	if !ok || len(frameImages) != 2 {
		t.Fatalf("frameImages = %v", capturedBody["frameImages"])
	}
	first := frameImages[0].(map[string]interface{})
	if first["frameType"] != "first_frame" {
		t.Errorf("frameType = %v", first["frameType"])
	}
	firstImage := first["image"].(map[string]interface{})
	if firstImage["data"] != "SGVsbG8=" {
		t.Errorf("data = %v, want base64 of 'Hello'", firstImage["data"])
	}
}
