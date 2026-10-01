package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func newTestVertexVideoModel(t *testing.T, handler http.HandlerFunc) (*VideoModel, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prov, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "test-token", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return NewVideoModel(prov, "veo-3.1-generate-preview"), srv
}

func TestVertexVideoModel_ConstructorInfo(t *testing.T) {
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {})

	if model.SpecificationVersion() != "v3" {
		t.Errorf("SpecificationVersion() = %q", model.SpecificationVersion())
	}
	if model.Provider() != "google-vertex" {
		t.Errorf("Provider() = %q", model.Provider())
	}
	if model.ModelID() != "veo-3.1-generate-preview" {
		t.Errorf("ModelID() = %q", model.ModelID())
	}
	if got := model.MaxVideosPerCall(); got == nil || *got != 4 {
		t.Errorf("MaxVideosPerCall() = %v, want 4", got)
	}
}

func TestVertexVideoModel_DoStart_RequestBody(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models/veo-3.1-generate-preview:predictLongRunning" {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/start-op", "done": false})
			return
		}
		http.NotFound(w, r)
	})

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "A rocket launching into space", N: 1},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	var op vertexVideoOperation
	if err := json.Unmarshal(result.Operation, &op); err != nil {
		t.Fatalf("unmarshal operation: %v", err)
	}
	if op.OperationName != "operations/start-op" {
		t.Errorf("OperationName = %q", op.OperationName)
	}

	instances := captured["instances"].([]interface{})
	instance := instances[0].(map[string]interface{})
	if instance["prompt"] != "A rocket launching into space" {
		t.Errorf("prompt = %v", instance["prompt"])
	}
	params := captured["parameters"].(map[string]interface{})
	if params["sampleCount"] != float64(1) {
		t.Errorf("sampleCount = %v", params["sampleCount"])
	}
}

func TestVertexVideoModel_DoStart_ResolutionMapping(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x", Resolution: "1920x1080"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	params := captured["parameters"].(map[string]interface{})
	if params["resolution"] != "1080p" {
		t.Errorf("resolution = %v, want 1080p", params["resolution"])
	}
}

func TestVertexVideoModel_DoStart_NoOperationName(t *testing.T) {
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"done": false})
	})

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err == nil {
		t.Fatal("expected error for missing operation name")
	}
}

func TestVertexVideoModel_DoStart_ProviderOptionsVertexKeyFallback(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})

	// providerOptions.vertex (legacy key) should be used when
	// providerOptions.googleVertex is absent.
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x",
			ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{
				"negativePrompt": "blurry",
			}},
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	params := captured["parameters"].(map[string]interface{})
	if params["negativePrompt"] != "blurry" {
		t.Errorf("negativePrompt = %v", params["negativePrompt"])
	}
}

func TestVertexVideoModel_DoStart_GenerateAudio(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})

	generateAudio := true
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x", GenerateAudio: &generateAudio},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	params := captured["parameters"].(map[string]interface{})
	if params["generateAudio"] != true {
		t.Errorf("generateAudio = %v", params["generateAudio"])
	}
}

func TestVertexVideoModel_DoStatus_CompletedBase64(t *testing.T) {
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/veo-3.1-generate-preview:fetchPredictOperation" {
			http.NotFound(w, r)
			return
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["operationName"] != "operations/status-op" {
			t.Errorf("operationName = %v", body["operationName"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "operations/status-op",
			"done": true,
			"response": map[string]interface{}{
				"videos": []map[string]interface{}{
					{"bytesBase64Encoded": "abc123", "mimeType": "video/mp4"},
				},
			},
		})
	})

	op, _ := json.Marshal(vertexVideoOperation{OperationName: "operations/status-op"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if result.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if len(result.Videos) != 1 || result.Videos[0].Type != "base64" || result.Videos[0].Data != "abc123" {
		t.Fatalf("Videos = %#v", result.Videos)
	}
	for _, key := range []string{"googleVertex", "google-vertex", "vertex"} {
		if _, ok := result.ProviderMetadata[key]; !ok {
			t.Errorf("ProviderMetadata missing key %q: %#v", key, result.ProviderMetadata)
		}
	}
}

func TestVertexVideoModel_DoStatus_CompletedGCS(t *testing.T) {
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "operations/status-op",
			"done": true,
			"response": map[string]interface{}{
				"videos": []map[string]interface{}{
					{"gcsUri": "gs://bucket/video.mp4", "mimeType": "video/mp4"},
				},
			},
		})
	})

	op, _ := json.Marshal(vertexVideoOperation{OperationName: "operations/status-op"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if result.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if len(result.Videos) != 1 || result.Videos[0].Type != "url" || result.Videos[0].URL != "gs://bucket/video.mp4" {
		t.Fatalf("Videos = %#v", result.Videos)
	}
}

func TestVertexVideoModel_DoStatus_Pending(t *testing.T) {
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/pending-op", "done": false})
	})

	op, _ := json.Marshal(vertexVideoOperation{OperationName: "operations/pending-op"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if result.Status != provider.VideoOperationStatusPending {
		t.Fatalf("Status = %q, want pending", result.Status)
	}
}

func TestVertexVideoModel_DoStatus_Error(t *testing.T) {
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "operations/error-op", "done": true,
			"error": map[string]interface{}{"code": 400, "message": "policy violation"},
		})
	})

	op, _ := json.Marshal(vertexVideoOperation{OperationName: "operations/error-op"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if result.Status != provider.VideoOperationStatusError {
		t.Fatalf("Status = %q, want error", result.Status)
	}
}

func TestVertexVideoModel_DoStatus_NoVideos(t *testing.T) {
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "operations/empty-op", "done": true,
			"response": map[string]interface{}{"videos": []interface{}{}},
		})
	})

	op, _ := json.Marshal(vertexVideoOperation{OperationName: "operations/empty-op"})
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err == nil {
		t.Fatal("expected error for empty videos")
	}
}

func TestVertexVideoModel_DoGenerate_EndToEnd(t *testing.T) {
	pollCount := 0
	model, _ := newTestVertexVideoModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models/veo-3.1-generate-preview:predictLongRunning":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/test-op", "done": false})
		case "/models/veo-3.1-generate-preview:fetchPredictOperation":
			pollCount++
			if pollCount == 1 {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/test-op", "done": false})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name": "operations/test-op",
				"done": true,
				"response": map[string]interface{}{
					"videos": []map[string]interface{}{{"gcsUri": "gs://bucket/video.mp4"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "A calm ocean at sunrise",
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{"pollIntervalMs": 1, "pollTimeoutMs": 5000},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(resp.Videos) != 1 {
		t.Fatalf("Videos = %#v", resp.Videos)
	}
}

// TestVideoModel_GetPollOptions_JSONNumbers is a regression test:
// getPollOptions read pollIntervalMs/pollTimeoutMs via a bare `.(int)` type
// assertion. ProviderOptions built via encoding/json.Unmarshal (a JSON
// config file, or a request body forwarded straight into ProviderOptions)
// decodes numbers as float64, so the assertion silently failed and the
// user's poll interval/timeout was dropped in favor of the hardcoded
// default, with no warning. Covers both the "googleVertex" and legacy
// "vertex" provider-options keys.
func TestVideoModel_GetPollOptions_JSONNumbers(t *testing.T) {
	m := &VideoModel{}

	for _, key := range []string{"googleVertex", "vertex"} {
		raw := []byte(`{"` + key + `":{"pollIntervalMs":111,"pollTimeoutMs":222}}`)
		var providerOpts map[string]interface{}
		if err := json.Unmarshal(raw, &providerOpts); err != nil {
			t.Fatal(err)
		}
		got := m.getPollOptions(providerOpts)
		if got.PollIntervalMs != 111 || got.PollTimeoutMs != 222 {
			t.Fatalf("[%s] JSON-sourced poll options were dropped: got PollIntervalMs=%d PollTimeoutMs=%d, want 111/222", key, got.PollIntervalMs, got.PollTimeoutMs)
		}
	}
}
