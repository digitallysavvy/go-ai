package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// asyncTestServer simulates the Fireworks async image generation API.
// It handles:
//   - POST /v1/workflows/{modelID}           → submit (returns request_id)
//   - POST /v1/workflows/{modelID}/get_result → poll (cycles through pollResponses)
//   - GET  /fake-image                        → image download (returns fake PNG bytes)
type asyncTestServer struct {
	server        *httptest.Server
	submitBody    map[string]interface{}
	pollCallCount int
}

// fakeImageBytes is a minimal 1-byte stand-in for image data returned by the download endpoint.
var fakeImageBytes = []byte{0x89, 0x50, 0x4e, 0x47} // PNG magic bytes

// newAsyncTestServer creates a mock server for async image generation tests.
// pollResponses is a list of JSON strings cycled on each poll call.
// The image URL embedded in Ready poll responses should point to the server's /fake-image path.
func newAsyncTestServer(t *testing.T, modelID, requestID string, pollResponses []string) *asyncTestServer {
	t.Helper()
	ts := &asyncTestServer{}
	mux := http.NewServeMux()

	submitPath := "/v1/workflows/" + modelID
	pollPath := "/v1/workflows/" + modelID + "/get_result"

	mux.HandleFunc("/v1/workflows/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		switch r.URL.Path {
		case submitPath:
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Logf("failed to decode submit body: %v", err)
			}
			ts.submitBody = body

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"request_id": %q}`, requestID)

		case pollPath:
			idx := ts.pollCallCount
			if idx >= len(pollResponses) {
				idx = len(pollResponses) - 1
			}
			ts.pollCallCount++

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, pollResponses[idx])

		default:
			http.NotFound(w, r)
		}
	})

	// Serve fake image binary for download tests
	mux.HandleFunc("/fake-image", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(fakeImageBytes)
	})

	ts.server = httptest.NewServer(mux)
	return ts
}

// providerForAsyncServer creates a Fireworks provider pointed at a test server with fast polling.
func providerForAsyncServer(serverURL string) *Provider {
	return New(Config{
		APIKey:              "test-key",
		BaseURL:             serverURL,
		ImagePollIntervalMs: 10, // fast for tests
	})
}

// TestIsAsyncModel verifies that only flux-kontext-* models use the async path.
func TestIsAsyncModel(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"accounts/fireworks/models/flux-kontext-dev", true},
		{"accounts/fireworks/models/flux-kontext-pro", true},
		{"accounts/fireworks/models/flux-kontext-max", true},
		{"accounts/fireworks/models/flux-1-dev-fp8", false},
		{"accounts/fireworks/models/stable-diffusion-xl-1024-v1-0", false},
		{"accounts/fireworks/models/playground-v2-5-1024px-aesthetic", false},
	}

	p := New(Config{APIKey: "test"})
	for _, tt := range tests {
		m := NewImageModel(p, tt.modelID)
		if got := m.isAsyncModel(); got != tt.want {
			t.Errorf("isAsyncModel(%q) = %v, want %v", tt.modelID, got, tt.want)
		}
	}
}

// TestDoGenerateAsync_ImmediateSuccess tests the happy path where polling succeeds on the first attempt.
func TestDoGenerateAsync_ImmediateSuccess(t *testing.T) {
	const modelID = "accounts/fireworks/models/flux-kontext-dev"
	const requestID = "req-abc123"

	// Use a placeholder; the real image URL will reference the test server after it starts.
	// We build the server first, then construct the poll response with the correct URL.
	ts := &asyncTestServer{}
	mux := http.NewServeMux()

	submitPath := "/v1/workflows/" + modelID
	pollPath := "/v1/workflows/" + modelID + "/get_result"
	pollCalled := false

	mux.HandleFunc("/v1/workflows/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case submitPath:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"request_id": %q}`, requestID)
		case pollPath:
			pollCalled = true
			imageURL := ts.server.URL + "/fake-image"
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id": %q, "status": "Ready", "result": {"sample": %q}}`, requestID, imageURL)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/fake-image", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(fakeImageBytes)
	})

	ts.server = httptest.NewServer(mux)
	defer ts.server.Close()

	prov := providerForAsyncServer(ts.server.URL)
	model := NewImageModel(prov, modelID)

	n := 1
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "A beautiful sunset",
		N:           &n,
		AspectRatio: "16:9",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pollCalled {
		t.Error("expected poll endpoint to be called")
	}
	if len(result.Image) == 0 {
		t.Error("expected non-empty image bytes")
	}
	if result.MimeType != "image/png" {
		t.Errorf("expected MimeType image/png, got %q", result.MimeType)
	}
}

// TestDoGenerateAsync_MultiPollSuccess tests that polling retries through Pending states.
func TestDoGenerateAsync_MultiPollSuccess(t *testing.T) {
	const modelID = "accounts/fireworks/models/flux-kontext-pro"
	const requestID = "req-xyz789"

	// Build server first so we can embed its URL in the Ready response.
	ts := &asyncTestServer{}
	mux := http.NewServeMux()

	submitPath := "/v1/workflows/" + modelID
	pollPath := "/v1/workflows/" + modelID + "/get_result"

	mux.HandleFunc("/v1/workflows/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case submitPath:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"request_id": %q}`, requestID)
		case pollPath:
			idx := ts.pollCallCount
			ts.pollCallCount++
			w.Header().Set("Content-Type", "application/json")
			switch idx {
			case 0:
				fmt.Fprintf(w, `{"id": %q, "status": "Pending", "result": null}`, requestID)
			case 1:
				fmt.Fprintf(w, `{"id": %q, "status": "Running", "result": null}`, requestID)
			default:
				imageURL := ts.server.URL + "/fake-image"
				fmt.Fprintf(w, `{"id": %q, "status": "Ready", "result": {"sample": %q}}`, requestID, imageURL)
			}
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/fake-image", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(fakeImageBytes)
	})

	ts.server = httptest.NewServer(mux)
	defer ts.server.Close()

	// ImagePollIntervalMs: 10 keeps the test fast
	prov := New(Config{APIKey: "test-key", BaseURL: ts.server.URL, ImagePollIntervalMs: 10})
	model := NewImageModel(prov, modelID)

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Futuristic city skyline",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.pollCallCount != 3 {
		t.Errorf("expected 3 poll calls, got %d", ts.pollCallCount)
	}
	if len(result.Image) == 0 {
		t.Error("expected non-empty image bytes")
	}
	if result.MimeType != "image/png" {
		t.Errorf("expected MimeType image/png, got %q", result.MimeType)
	}
}

// TestDoGenerateAsync_SubmitRequestBody verifies the submit request body is correctly built.
func TestDoGenerateAsync_SubmitRequestBody(t *testing.T) {
	const modelID = "accounts/fireworks/models/flux-kontext-dev"
	const requestID = "req-body-check"

	ts := &asyncTestServer{}
	mux := http.NewServeMux()
	submitPath := "/v1/workflows/" + modelID
	pollPath := "/v1/workflows/" + modelID + "/get_result"

	mux.HandleFunc("/v1/workflows/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case submitPath:
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			ts.submitBody = body
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"request_id": %q}`, requestID)
		case pollPath:
			imageURL := ts.server.URL + "/fake-image"
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id": %q, "status": "Ready", "result": {"sample": %q}}`, requestID, imageURL)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/fake-image", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(fakeImageBytes)
	})
	ts.server = httptest.NewServer(mux)
	defer ts.server.Close()

	prov := providerForAsyncServer(ts.server.URL)
	model := NewImageModel(prov, modelID)

	seed := 42
	n := 1
	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "A mountain lake",
		N:           &n,
		AspectRatio: "4:3",
		Seed:        &seed,
		ProviderOptions: map[string]interface{}{
			"fireworks": map[string]interface{}{
				"output_format": "jpeg",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := ts.submitBody
	if body["prompt"] != "A mountain lake" {
		t.Errorf("expected prompt 'A mountain lake', got %v", body["prompt"])
	}
	if body["aspect_ratio"] != "4:3" {
		t.Errorf("expected aspect_ratio '4:3', got %v", body["aspect_ratio"])
	}
	if body["samples"] != float64(1) {
		t.Errorf("expected samples 1, got %v", body["samples"])
	}
	if body["seed"] != float64(42) {
		t.Errorf("expected seed 42, got %v", body["seed"])
	}
	if body["output_format"] != "jpeg" {
		t.Errorf("expected output_format 'jpeg', got %v", body["output_format"])
	}
}

// TestDoGenerateAsync_Failure tests that a failed generation returns a descriptive error.
func TestDoGenerateAsync_Failure(t *testing.T) {
	const modelID = "accounts/fireworks/models/flux-kontext-dev"
	const requestID = "req-fail"

	failResp := fmt.Sprintf(`{"id": %q, "status": "Error", "result": null}`, requestID)
	ts := newAsyncTestServer(t, modelID, requestID, []string{failResp})
	defer ts.server.Close()

	prov := providerForAsyncServer(ts.server.URL)
	model := NewImageModel(prov, modelID)

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Will fail",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Error") {
		t.Errorf("expected error to mention 'Error', got: %v", err)
	}
}

// TestDoGenerateAsync_FailedStatus tests that a "Failed" status also returns an error.
func TestDoGenerateAsync_FailedStatus(t *testing.T) {
	const modelID = "accounts/fireworks/models/flux-kontext-pro"
	const requestID = "req-failed"

	failResp := fmt.Sprintf(`{"id": %q, "status": "Failed", "result": null}`, requestID)
	ts := newAsyncTestServer(t, modelID, requestID, []string{failResp})
	defer ts.server.Close()

	prov := providerForAsyncServer(ts.server.URL)
	model := NewImageModel(prov, modelID)

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Will fail",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Failed") {
		t.Errorf("expected error to mention 'Failed', got: %v", err)
	}
}

// TestDoGenerateAsync_ContextCancellation tests that cancelling the context stops polling.
func TestDoGenerateAsync_ContextCancellation(t *testing.T) {
	const modelID = "accounts/fireworks/models/flux-kontext-dev"
	const requestID = "req-cancel"

	// Server always returns Pending so polling would loop forever without cancellation
	pendingResp := fmt.Sprintf(`{"id": %q, "status": "Pending", "result": null}`, requestID)
	ts := newAsyncTestServer(t, modelID, requestID, []string{pendingResp})
	defer ts.server.Close()

	prov := providerForAsyncServer(ts.server.URL)
	model := NewImageModel(prov, modelID)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately after the first tick by cancelling before calling DoGenerate
	// The ticker fires at 2s so we cancel the context right away.
	cancel()

	_, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt: "Will be cancelled",
	})
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in error chain, got: %v", err)
	}
}

// TestDoGenerateAsync_ReadyMissingSample tests error when Ready but result.sample is nil.
func TestDoGenerateAsync_ReadyMissingSample(t *testing.T) {
	const modelID = "accounts/fireworks/models/flux-kontext-dev"
	const requestID = "req-no-sample"

	// Ready but result has no sample field
	badResp := fmt.Sprintf(`{"id": %q, "status": "Ready", "result": {}}`, requestID)
	ts := newAsyncTestServer(t, modelID, requestID, []string{badResp})
	defer ts.server.Close()

	prov := providerForAsyncServer(ts.server.URL)
	model := NewImageModel(prov, modelID)

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Missing sample",
	})
	if err == nil {
		t.Fatal("expected error for missing sample, got nil")
	}
	if !strings.Contains(err.Error(), "missing result.sample") {
		t.Errorf("expected error about missing sample, got: %v", err)
	}
}

// TestDoGenerateSync_NonAsyncModel verifies that non-flux-kontext models use the sync path.
// This is a regression test to ensure backward compatibility.
func TestDoGenerateSync_NonAsyncModel(t *testing.T) {
	const modelID = "accounts/fireworks/models/stable-diffusion-xl-1024-v1-0"

	// The sync path POSTs to /v1/images/generations and returns JSON with a data array
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/images/generations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"data": [{"url": "https://example.com/sync.png"}]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prov := providerForAsyncServer(server.URL)
	model := NewImageModel(prov, modelID)

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Mountains",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.URL != "https://example.com/sync.png" {
		t.Errorf("expected sync URL, got %q", result.URL)
	}
}

// TestCheckAsyncStatus_Cancelled verifies that checkAsyncStatus maps "cancelled" to no-op (continue polling).
func TestCheckAsyncStatus_Statuses(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		wantDone   bool
		wantErrStr string
	}{
		{"Ready", "Ready", true, ""},
		{"Error", "Error", false, "Error"},
		{"Failed", "Failed", false, "Failed"},
		{"Pending", "Pending", false, ""},
		{"Running", "Running", false, ""},
		{"Unknown", "Unknown", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const modelID = "accounts/fireworks/models/flux-kontext-dev"
			const requestID = "req-status-test"

			var respBody string
			if tt.status == "Ready" {
				respBody = fmt.Sprintf(`{"id": %q, "status": "Ready", "result": {"sample": "https://example.com/img.png"}}`, requestID)
			} else {
				respBody = fmt.Sprintf(`{"id": %q, "status": %q, "result": null}`, requestID, tt.status)
			}

			mux := http.NewServeMux()
			mux.HandleFunc("/v1/workflows/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, respBody)
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			prov := providerForAsyncServer(server.URL)
			model := NewImageModel(prov, modelID)

			_, done, err := model.checkAsyncStatus(context.Background(), requestID)

			if tt.wantErrStr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrStr)
				}
				if !strings.Contains(err.Error(), tt.wantErrStr) {
					t.Errorf("expected error containing %q, got: %v", tt.wantErrStr, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if done != tt.wantDone {
				t.Errorf("expected done=%v, got %v", tt.wantDone, done)
			}
		})
	}
}

// TestBuildAsyncRequestBody verifies that buildAsyncRequestBody includes all expected fields.
func TestBuildAsyncRequestBody(t *testing.T) {
	prov := New(Config{APIKey: "test"})
	model := NewImageModel(prov, "accounts/fireworks/models/flux-kontext-dev")

	seed := 42
	n := 1
	opts := &provider.ImageGenerateOptions{
		Prompt:      "Test prompt",
		N:           &n,
		AspectRatio: "16:9",
		Seed:        &seed,
		ProviderOptions: map[string]interface{}{
			"fireworks": map[string]interface{}{
				"custom_param": "value",
			},
		},
	}

	body := model.buildAsyncRequestBody(opts)

	if body["prompt"] != "Test prompt" {
		t.Errorf("expected prompt, got %v", body["prompt"])
	}
	if body["samples"] != 1 {
		t.Errorf("expected samples=1, got %v", body["samples"])
	}
	if body["aspect_ratio"] != "16:9" {
		t.Errorf("expected aspect_ratio, got %v", body["aspect_ratio"])
	}
	if body["seed"] != 42 {
		t.Errorf("expected seed=42, got %v", body["seed"])
	}
	if body["custom_param"] != "value" {
		t.Errorf("expected custom_param passthrough, got %v", body["custom_param"])
	}
}

// TestDoGenerateAsync_Warnings verifies that unsupported options produce the correct warnings.
func TestDoGenerateAsync_Warnings(t *testing.T) {
	const modelID = "accounts/fireworks/models/flux-kontext-dev"
	const requestID = "req-warnings"

	ts := &asyncTestServer{}
	mux := http.NewServeMux()
	submitPath := "/v1/workflows/" + modelID
	pollPath := "/v1/workflows/" + modelID + "/get_result"

	mux.HandleFunc("/v1/workflows/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case submitPath:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"request_id": %q}`, requestID)
		case pollPath:
			imageURL := ts.server.URL + "/fake-image"
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id": %q, "status": "Ready", "result": {"sample": %q}}`, requestID, imageURL)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/fake-image", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(fakeImageBytes)
	})
	ts.server = httptest.NewServer(mux)
	defer ts.server.Close()

	prov := providerForAsyncServer(ts.server.URL)
	model := NewImageModel(prov, modelID)

	// Provide a second file and mask to trigger all three warning types
	mask := &provider.ImageFile{Type: "url", URL: "https://example.com/mask.png"}
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Trigger warnings",
		Size:   "1024x1024",
		Files: []provider.ImageFile{
			{Type: "url", URL: "https://example.com/a.png"},
			{Type: "url", URL: "https://example.com/b.png"},
		},
		Mask: mask,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 3 {
		t.Errorf("expected 3 warnings, got %d: %v", len(result.Warnings), result.Warnings)
	}

	warnTypes := make(map[string]bool)
	for _, w := range result.Warnings {
		warnTypes[w.Type] = true
		if w.Type != "unsupported-setting" {
			t.Errorf("unexpected warning type %q", w.Type)
		}
	}

	warnMessages := make(map[string]bool)
	for _, w := range result.Warnings {
		warnMessages[w.Message] = true
	}
	if !warnMessages["size is not supported for flux-kontext models; use aspect_ratio instead"] {
		t.Errorf("expected size warning, got: %v", result.Warnings)
	}
	if !warnMessages["only the first input image is used; multiple files are not supported"] {
		t.Errorf("expected multiple-files warning, got: %v", result.Warnings)
	}
	if !warnMessages["mask is not supported for flux-kontext models"] {
		t.Errorf("expected mask warning, got: %v", result.Warnings)
	}
}

// TestFireworksFluxKontextAsync is an integration test that runs against the live Fireworks API.
// It is skipped unless FIREWORKS_API_KEY is set.
func TestFireworksFluxKontextAsync(t *testing.T) {
	if os.Getenv("FIREWORKS_API_KEY") == "" {
		t.Skip("Skipping: FIREWORKS_API_KEY not set")
	}

	prov := New(Config{
		APIKey: os.Getenv("FIREWORKS_API_KEY"),
	})
	model, err := prov.ImageModel("accounts/fireworks/models/flux-kontext-dev")
	if err != nil {
		t.Fatalf("failed to get model: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "A serene mountain lake at sunset, photorealistic",
		AspectRatio: "16:9",
	})
	if err != nil {
		t.Fatalf("image generation failed: %v", err)
	}

	if len(result.Image) == 0 {
		t.Error("expected non-empty image bytes in result")
	}
	t.Logf("Generated image: %d bytes (%s)", len(result.Image), result.MimeType)
}
