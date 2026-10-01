package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// intPtr is a small test helper shared across the google package's test
// files.
func intPtr(i int) *int { return &i }

// newTestVideoServer builds a Provider + httptest server that handles the
// predictLongRunning submission and the operation status GET, mirroring
// google-video-model.test.ts's createMockModel fetch stub.
func newTestVideoServer(t *testing.T, handler http.HandlerFunc) (*VideoModel, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := New(Config{APIKey: "test-api-key", BaseURL: srv.URL})
	return NewVideoModel(p, "veo-3.1-generate-preview"), srv
}

func TestVideoModel_ConstructorInfo(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, "veo-3.1-generate-preview")

	if model.SpecificationVersion() != "v3" {
		t.Errorf("SpecificationVersion() = %q", model.SpecificationVersion())
	}
	if model.Provider() != "google.generative-ai" {
		t.Errorf("Provider() = %q", model.Provider())
	}
	if model.ModelID() != "veo-3.1-generate-preview" {
		t.Errorf("ModelID() = %q", model.ModelID())
	}
	if got := model.MaxVideosPerCall(); got == nil || *got != 4 {
		t.Errorf("MaxVideosPerCall() = %v, want 4", got)
	}
}

func TestVideoModel_DoStart_RequestBody(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models/veo-3.1-generate-preview:predictLongRunning" {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/start-test-op", "done": false})
			return
		}
		http.NotFound(w, r)
	})

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "A futuristic city with flying cars", N: 1},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	var op googleVideoOperation
	if err := json.Unmarshal(result.Operation, &op); err != nil {
		t.Fatalf("unmarshal operation: %v", err)
	}
	if op.OperationName != "operations/start-test-op" {
		t.Errorf("OperationName = %q", op.OperationName)
	}

	want := map[string]interface{}{
		"instances":  []interface{}{map[string]interface{}{"prompt": "A futuristic city with flying cars"}},
		"parameters": map[string]interface{}{"sampleCount": float64(1)},
	}
	gotJSON, _ := json.Marshal(captured)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("body = %s, want %s", gotJSON, wantJSON)
	}
}

func TestVideoModel_DoStart_SeedAspectRatioResolutionDuration(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})

	seed := 42
	duration := 5.0
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x", N: 1, Seed: &seed, AspectRatio: "16:9", Resolution: "1920x1080", Duration: &duration,
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	params := captured["parameters"].(map[string]interface{})
	if params["seed"] != float64(42) {
		t.Errorf("seed = %v", params["seed"])
	}
	if params["aspectRatio"] != "16:9" {
		t.Errorf("aspectRatio = %v", params["aspectRatio"])
	}
	if params["resolution"] != "1080p" {
		t.Errorf("resolution = %v, want 1080p", params["resolution"])
	}
	if params["durationSeconds"] != float64(5) {
		t.Errorf("durationSeconds = %v", params["durationSeconds"])
	}
}

func TestVideoModel_DoStart_SampleCountFromN(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x", N: 2},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	params := captured["parameters"].(map[string]interface{})
	if params["sampleCount"] != float64(2) {
		t.Errorf("sampleCount = %v, want 2", params["sampleCount"])
	}
}

func TestVideoModel_DoStart_Headers(t *testing.T) {
	var capturedHeaders http.Header
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt:  "x",
			Headers: map[string]string{"x-custom-header": "custom-value"},
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if capturedHeaders.Get("x-custom-header") != "custom-value" {
		t.Errorf("x-custom-header = %q", capturedHeaders.Get("x-custom-header"))
	}
	if capturedHeaders.Get("x-goog-api-key") != "test-api-key" {
		t.Errorf("x-goog-api-key = %q", capturedHeaders.Get("x-goog-api-key"))
	}
}

func TestVideoModel_DoStart_NoOperationName(t *testing.T) {
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestVideoModel_DoStart_ImageAsInlineData(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x",
			Image:  &provider.VideoModelV3File{Type: "file", Data: []byte("base64-image-data"), MediaType: "image/png"},
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	instances := captured["instances"].([]interface{})
	instance := instances[0].(map[string]interface{})
	image := instance["image"].(map[string]interface{})
	if image["mimeType"] != "image/png" {
		t.Errorf("mimeType = %v", image["mimeType"])
	}
	if _, ok := image["bytesBase64Encoded"]; !ok {
		t.Error("expected bytesBase64Encoded")
	}
}

func TestVideoModel_DoStart_URLImageWarns(t *testing.T) {
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x",
			Image:  &provider.VideoModelV3File{Type: "url", URL: "https://example.com/image.png"},
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "URL-based image input" {
		t.Errorf("Warnings = %#v", result.Warnings)
	}
}

func TestVideoModel_DoStart_PersonGenerationNegativePromptReferenceImages(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x",
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
				"personGeneration": "allow_adult",
				"negativePrompt":   "blurry, low quality",
			}},
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	params := captured["parameters"].(map[string]interface{})
	if params["personGeneration"] != "allow_adult" {
		t.Errorf("personGeneration = %v", params["personGeneration"])
	}
	if params["negativePrompt"] != "blurry, low quality" {
		t.Errorf("negativePrompt = %v", params["negativePrompt"])
	}

	_, err = model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "x",
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
				"referenceImages": []interface{}{
					map[string]interface{}{"bytesBase64Encoded": "reference-image-data"},
					map[string]interface{}{"gcsUri": "gs://bucket/reference.png"},
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	instances := captured["instances"].([]interface{})
	instance := instances[0].(map[string]interface{})
	refs := instance["referenceImages"].([]interface{})
	if len(refs) != 2 {
		t.Fatalf("referenceImages len = %d, want 2", len(refs))
	}
}

func TestVideoModel_DoStatus_CompletedAppendsAPIKey(t *testing.T) {
	var srvURL string
	var model *VideoModel
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "operations/status-test-op",
			"done": true,
			"response": map[string]interface{}{
				"generateVideoResponse": map[string]interface{}{
					"generatedSamples": []map[string]interface{}{
						{"video": map[string]interface{}{"uri": srvURL + "/files/video-456.mp4"}},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	p := New(Config{APIKey: "test-api-key", BaseURL: srv.URL})
	model = NewVideoModel(p, "veo-3.1-generate-preview")

	op, _ := json.Marshal(googleVideoOperation{OperationName: "operations/status-test-op"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if result.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if len(result.Videos) != 1 {
		t.Fatalf("Videos = %#v", result.Videos)
	}
	if result.Videos[0].URL != srvURL+"/files/video-456.mp4?key=test-api-key" {
		t.Errorf("URL = %q", result.Videos[0].URL)
	}
}

func TestVideoModel_DoStatus_Pending(t *testing.T) {
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/pending-op", "done": false})
	})

	op, _ := json.Marshal(googleVideoOperation{OperationName: "operations/pending-op"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if result.Status != provider.VideoOperationStatusPending {
		t.Fatalf("Status = %q, want pending", result.Status)
	}
}

func TestVideoModel_DoStatus_Error(t *testing.T) {
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "operations/error-op",
			"done": true,
			"error": map[string]interface{}{
				"code": 400, "message": "Content policy violation", "status": "FAILED_PRECONDITION",
			},
		})
	})

	op, _ := json.Marshal(googleVideoOperation{OperationName: "operations/error-op"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if result.Status != provider.VideoOperationStatusError {
		t.Fatalf("Status = %q, want error", result.Status)
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestVideoModel_DoStatus_NoVideos(t *testing.T) {
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "operations/empty-op",
			"done": true,
			"response": map[string]interface{}{
				"generateVideoResponse": map[string]interface{}{"generatedSamples": []interface{}{}},
			},
		})
	})

	op, _ := json.Marshal(googleVideoOperation{OperationName: "operations/empty-op"})
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err == nil {
		t.Fatal("expected error for empty videos")
	}
}

func TestVideoModel_DoGenerate_EndToEnd(t *testing.T) {
	pollCount := 0
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models/veo-3.1-generate-preview:predictLongRunning":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/test-op", "done": false})
		case "/operations/test-op":
			pollCount++
			if pollCount == 1 {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/test-op", "done": false})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name": "operations/test-op",
				"done": true,
				"response": map[string]interface{}{
					"generateVideoResponse": map[string]interface{}{
						"generatedSamples": []map[string]interface{}{
							{"video": map[string]interface{}{"uri": "https://example.com/video.mp4"}},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "A calm ocean at sunrise",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{"pollIntervalMs": 1, "pollTimeoutMs": 5000},
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
// default, with no warning.
func TestVideoModel_GetPollOptions_JSONNumbers(t *testing.T) {
	m := &VideoModel{}
	raw := []byte(`{"google":{"pollIntervalMs":111,"pollTimeoutMs":222}}`)
	var providerOpts map[string]interface{}
	if err := json.Unmarshal(raw, &providerOpts); err != nil {
		t.Fatal(err)
	}
	got := m.getPollOptions(providerOpts)
	if got.PollIntervalMs != 111 || got.PollTimeoutMs != 222 {
		t.Fatalf("JSON-sourced poll options were dropped: got PollIntervalMs=%d PollTimeoutMs=%d, want 111/222", got.PollIntervalMs, got.PollTimeoutMs)
	}
}
