package bytedance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// newImageTestServer creates a mock server for /images/generations that
// records the last request and serves a fixed JSON response body, mirroring
// the TS test file's createTestServer usage.
func newImageTestServer(t *testing.T, responseBody string) (*httptest.Server, *map[string]interface{}, *http.Request) {
	t.Helper()
	var capturedBody map[string]interface{}
	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		if r.URL.Path != "/api/v3/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	return server, &capturedBody, capturedReq
}

// TestImageModel_ConstructorMetadata mirrors "constructor > should expose
// correct provider and model information".
func TestImageModel_ConstructorMetadata(t *testing.T) {
	prov, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	model, err := prov.ImageModel(string(ModelSeedream50))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	im := model.(*ImageModel)
	if im.Provider() != "bytedance.image" {
		t.Errorf("Provider() = %q, want %q", im.Provider(), "bytedance.image")
	}
	if im.ModelID() != string(ModelSeedream50) {
		t.Errorf("ModelID() = %q, want %q", im.ModelID(), ModelSeedream50)
	}
	if im.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q, want v4", im.SpecificationVersion())
	}
	if got := im.MaxImagesPerCall(); got == nil || *got != 1 {
		t.Errorf("MaxImagesPerCall() = %v, want 1", got)
	}
}

// TestImageModel_TextToImageRequest mirrors "doGenerate > should send a JSON
// text-to-image request to /images/generations".
func TestImageModel_TextToImageRequest(t *testing.T) {
	server, body, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"},{"b64_json":"test5678"}]}`)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newImageModel(prov, string(ModelSeedream50))

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A salamander in a forest pond at dusk surrounded by fireflies",
		Size:   "2048x2048",
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"watermark": false},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	want := map[string]interface{}{
		"model":           string(ModelSeedream50),
		"prompt":          "A salamander in a forest pond at dusk surrounded by fireflies",
		"size":            "2048x2048",
		"watermark":       false,
		"response_format": "b64_json",
	}
	assertJSONEqual(t, *body, want)
}

// TestImageModel_TypedProviderOptions mirrors "doGenerate > should map typed
// provider options to ByteDance request fields".
func TestImageModel_TypedProviderOptions(t *testing.T) {
	server, body, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}]}`)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newImageModel(prov, string(ModelSeedream50))

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "prompt",
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"sequentialImageGeneration": "auto",
				"maxImages":                 4,
				"outputFormat":              "png",
				"optimizePromptMode":        "fast",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	want := map[string]interface{}{
		"model":                               string(ModelSeedream50),
		"prompt":                              "prompt",
		"sequential_image_generation":         "auto",
		"sequential_image_generation_options": map[string]interface{}{"max_images": float64(4)},
		"output_format":                       "png",
		"optimize_prompt_options":             map[string]interface{}{"mode": "fast"},
		"response_format":                     "b64_json",
	}
	assertJSONEqual(t, *body, want)
}

// TestImageModel_ResolutionLevelOverridesSize mirrors "doGenerate > should
// let a resolution-level size override the top-level pixel size".
func TestImageModel_ResolutionLevelOverridesSize(t *testing.T) {
	server, body, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}]}`)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newImageModel(prov, string(ModelSeedream50))

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "prompt",
		Size:   "2048x2048",
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"size": "2K"},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if (*body)["size"] != "2K" {
		t.Errorf("size = %v, want %q", (*body)["size"], "2K")
	}
}

// TestImageModel_PassthroughUnknownOptions mirrors "doGenerate > should pass
// through unknown provider options unchanged".
func TestImageModel_PassthroughUnknownOptions(t *testing.T) {
	server, body, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}]}`)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newImageModel(prov, string(ModelSeedream50))

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "prompt",
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"seed": float64(42)},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if (*body)["seed"] != float64(42) {
		t.Errorf("seed = %v, want 42", (*body)["seed"])
	}
}

// TestImageModel_WarnsForUnsupportedSettings mirrors "doGenerate > should
// warn for unsupported settings (aspectRatio, seed, mask)".
func TestImageModel_WarnsForUnsupportedSettings(t *testing.T) {
	server, _, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}]}`)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newImageModel(prov, string(ModelSeedream50))

	seed := 123
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "prompt",
		AspectRatio: "16:9",
		Seed:        &seed,
		Mask: &provider.ImageFile{
			Type:      "file",
			Data:      []byte{1},
			MediaType: "image/png",
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if len(result.Warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %d: %+v", len(result.Warnings), result.Warnings)
	}
	wantFeatures := []string{"aspectRatio", "seed", "mask"}
	for i, w := range result.Warnings {
		if w.Feature != wantFeatures[i] {
			t.Errorf("warning[%d].Feature = %q, want %q", i, w.Feature, wantFeatures[i])
		}
	}
}

// TestImageModel_PassesHeaders mirrors "doGenerate > should pass headers":
// provider-level and per-request headers must both reach the API call, with
// the per-request header able to add to (not just override) the
// provider-configured ones.
func TestImageModel_PassesHeaders(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"test1234"}]}`))
	}))
	defer server.Close()

	prov, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL + "/api/v3",
		Headers: map[string]string{"Custom-Provider-Header": "provider-header-value"},
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	model := newImageModel(prov, string(ModelSeedream50))

	_, err = model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:  "prompt",
		Headers: map[string]string{"Custom-Request-Header": "request-header-value"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if got := gotHeaders.Get("Custom-Provider-Header"); got != "provider-header-value" {
		t.Errorf("Custom-Provider-Header = %q, want %q", got, "provider-header-value")
	}
	if got := gotHeaders.Get("Custom-Request-Header"); got != "request-header-value" {
		t.Errorf("Custom-Request-Header = %q, want %q", got, "request-header-value")
	}
	if got := gotHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

// TestImageModel_APIError mirrors "doGenerate > should handle API errors".
func TestImageModel_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid prompt content","code":"InvalidParameter"}}`))
	}))
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newImageModel(prov, string(ModelSeedream50))

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "prompt"})
	if err == nil {
		t.Fatal("expected an error")
	}
	bdErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *bytedance.Error, got %T: %v", err, err)
	}
	if bdErr.Code != 400 {
		t.Errorf("Code = %d, want 400", bdErr.Code)
	}
	if bdErr.Message != "Invalid prompt content" {
		t.Errorf("Message = %q, want %q", bdErr.Message, "Invalid prompt content")
	}
}

// TestImageModel_ReturnsRawB64Images mirrors "doGenerate > should return the
// raw b64_json content".
func TestImageModel_ReturnsRawB64Images(t *testing.T) {
	server, _, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"},{"b64_json":"test5678"}]}`)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newImageModel(prov, string(ModelSeedream50))

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "prompt"})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	want := []string{"test1234", "test5678"}
	if len(result.Base64Images) != len(want) {
		t.Fatalf("Base64Images = %v, want %v", result.Base64Images, want)
	}
	for i := range want {
		if result.Base64Images[i] != want[i] {
			t.Errorf("Base64Images[%d] = %q, want %q", i, result.Base64Images[i], want[i])
		}
	}
	if result.Base64Image != "test1234" {
		t.Errorf("Base64Image = %q, want %q", result.Base64Image, "test1234")
	}
}

// TestImageModel_ResponseMetadata mirrors "doGenerate > should include
// timestamp, headers and modelId in response".
func TestImageModel_ResponseMetadata(t *testing.T) {
	server, _, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}]}`)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newImageModel(prov, string(ModelSeedream50))

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "prompt"})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if result.Response == nil {
		t.Fatal("expected Response to be set")
	}
	if result.Response.ModelID != string(ModelSeedream50) {
		t.Errorf("Response.ModelID = %q, want %q", result.Response.ModelID, ModelSeedream50)
	}
	if result.Response.Timestamp.IsZero() {
		t.Error("expected a non-zero Response.Timestamp")
	}
}

// TestImageModel_Usage mirrors "doGenerate > usage" describe block.
func TestImageModel_Usage(t *testing.T) {
	t.Run("maps Ark token usage, leaving inputTokens undefined", func(t *testing.T) {
		server, _, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}],"usage":{"generated_images":1,"output_tokens":4096,"total_tokens":4096}}`)
		defer server.Close()
		prov := providerForServer(t, server.URL)
		model := newImageModel(prov, string(ModelSeedream50))

		result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "prompt"})
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if result.Usage.InputTokens != 0 {
			t.Errorf("InputTokens = %d, want 0 (unset)", result.Usage.InputTokens)
		}
		if result.Usage.OutputTokens != 4096 {
			t.Errorf("OutputTokens = %d, want 4096", result.Usage.OutputTokens)
		}
		if result.Usage.TotalTokens != 4096 {
			t.Errorf("TotalTokens = %d, want 4096", result.Usage.TotalTokens)
		}
	})

	t.Run("does not map generated_images into usage", func(t *testing.T) {
		server, _, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"},{"b64_json":"test5678"}],"usage":{"generated_images":2,"output_tokens":8192,"total_tokens":8192}}`)
		defer server.Close()
		prov := providerForServer(t, server.URL)
		model := newImageModel(prov, string(ModelSeedream50))

		result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "prompt"})
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if result.Usage.OutputTokens != 8192 || result.Usage.TotalTokens != 8192 {
			t.Errorf("usage = %+v", result.Usage)
		}
		if len(result.Base64Images) != 2 {
			t.Errorf("expected 2 images, got %d", len(result.Base64Images))
		}
	})

	t.Run("returns zero usage when Ark omits it", func(t *testing.T) {
		server, _, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}]}`)
		defer server.Close()
		prov := providerForServer(t, server.URL)
		model := newImageModel(prov, string(ModelSeedream50))

		result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "prompt"})
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if result.Usage.OutputTokens != 0 || result.Usage.TotalTokens != 0 {
			t.Errorf("usage = %+v, want zero value", result.Usage)
		}
	})

	t.Run("maps null token fields to zero", func(t *testing.T) {
		server, _, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}],"usage":{"generated_images":1,"output_tokens":null,"total_tokens":null}}`)
		defer server.Close()
		prov := providerForServer(t, server.URL)
		model := newImageModel(prov, string(ModelSeedream50))

		result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "prompt"})
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if result.Usage.OutputTokens != 0 || result.Usage.TotalTokens != 0 {
			t.Errorf("usage = %+v, want zero value", result.Usage)
		}
	})
}

// TestImageModel_ImageEditing mirrors the "image editing" describe block.
func TestImageModel_ImageEditing(t *testing.T) {
	t.Run("sends a single input image as the `image` field", func(t *testing.T) {
		server, body, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"},{"b64_json":"test5678"}]}`)
		defer server.Close()
		prov := providerForServer(t, server.URL)
		model := newImageModel(prov, string(ModelSeedream50))

		result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "Change the salamander to a snow weasel",
			Files: []provider.ImageFile{
				{Type: "file", Data: []byte{137, 80, 78, 71}, MediaType: "image/png"},
			},
		})
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}

		want := map[string]interface{}{
			"model":           string(ModelSeedream50),
			"prompt":          "Change the salamander to a snow weasel",
			"image":           "data:image/png;base64,iVBORw==",
			"response_format": "b64_json",
		}
		assertJSONEqual(t, *body, want)
		if len(result.Base64Images) != 2 {
			t.Errorf("expected 2 images, got %d", len(result.Base64Images))
		}
	})

	t.Run("sends multiple input images as an array", func(t *testing.T) {
		server, body, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}]}`)
		defer server.Close()
		prov := providerForServer(t, server.URL)
		model := newImageModel(prov, string(ModelSeedream50))

		_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "Combine these images",
			Files: []provider.ImageFile{
				{Type: "file", Data: []byte{137, 80, 78, 71}, MediaType: "image/png"},
				{Type: "file", Data: []byte{137, 80, 78, 71}, MediaType: "image/png"},
			},
		})
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}

		want := map[string]interface{}{
			"model":  string(ModelSeedream50),
			"prompt": "Combine these images",
			"image": []interface{}{
				"data:image/png;base64,iVBORw==",
				"data:image/png;base64,iVBORw==",
			},
			"response_format": "b64_json",
		}
		assertJSONEqual(t, *body, want)
	})

	t.Run("passes a URL input image through unchanged", func(t *testing.T) {
		server, body, _ := newImageTestServer(t, `{"data":[{"b64_json":"test1234"}]}`)
		defer server.Close()
		prov := providerForServer(t, server.URL)
		model := newImageModel(prov, string(ModelSeedream50))

		_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "Edit this",
			Files: []provider.ImageFile{
				{Type: "url", URL: "https://example.com/input.png"},
			},
		})
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if (*body)["image"] != "https://example.com/input.png" {
			t.Errorf("image = %v, want %q", (*body)["image"], "https://example.com/input.png")
		}
	})
}

// assertJSONEqual compares two decoded-JSON maps for deep equality via
// round-tripping through JSON (avoids float64/int and map-ordering noise).
func assertJSONEqual(t *testing.T, got, want map[string]interface{}) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("failed to marshal got: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("failed to marshal want: %v", err)
	}
	var gotNorm, wantNorm interface{}
	_ = json.Unmarshal(gotJSON, &gotNorm)
	_ = json.Unmarshal(wantJSON, &wantNorm)
	gotStr, _ := json.Marshal(gotNorm)
	wantStr, _ := json.Marshal(wantNorm)
	if string(gotStr) != string(wantStr) {
		t.Errorf("body mismatch:\n got:  %s\n want: %s", gotStr, wantStr)
	}
}
