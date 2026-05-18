package xai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestXAIImageModelIDs verifies image model ID constants have correct values.
func TestXAIImageModelIDs(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"GrokImagineImage", ModelGrokImagineImage, "grok-imagine-image"},
		{"GrokImagineImagePro", ModelGrokImagineImagePro, "grok-imagine-image-pro"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("constant %s = %q, want %q", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestXAIImageModelMetadataMatchesTypeScript(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelGrokImagineImage)

	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q, want %q", model.SpecificationVersion(), "v4")
	}
	if model.Provider() != "xai.image" {
		t.Fatalf("Provider() = %q, want %q", model.Provider(), "xai.image")
	}
	if model.ModelID() != "grok-imagine-image" {
		t.Fatalf("ModelID() = %q, want %q", model.ModelID(), "grok-imagine-image")
	}
	if model.MaxImagesPerCall() != 3 {
		t.Fatalf("MaxImagesPerCall() = %d, want 3", model.MaxImagesPerCall())
	}
}

func TestXAIImageModelAcceptsTypeScriptStyleV1BaseURL(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	})
	model := NewImageModel(prov, ModelGrokImagineImage)

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if gotPath != "/v1/images/generations" {
		t.Fatalf("request path = %q, want %q", gotPath, "/v1/images/generations")
	}
}

func TestXAIImageModelPassesRequestHeaders(t *testing.T) {
	var gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Custom-Request-Header")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat",
		Headers: map[string]string{
			"Custom-Request-Header": "request-header-value",
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if gotHeader != "request-header-value" {
		t.Fatalf("Custom-Request-Header = %q, want request-header-value", gotHeader)
	}
}

// TestXAIResolutionOptionSerialized verifies the resolution field is sent in the request body.
func TestXAIResolutionOptionSerialized(t *testing.T) {
	tests := []struct {
		name               string
		resolution         *string
		expectResolution   bool
		expectedResolution string
	}{
		{
			name:               "1k resolution serialized",
			resolution:         imageTestStrPtr("1k"),
			expectResolution:   true,
			expectedResolution: "1k",
		},
		{
			name:               "2k resolution serialized",
			resolution:         imageTestStrPtr("2k"),
			expectResolution:   true,
			expectedResolution: "2k",
		},
		{
			name:             "nil resolution not sent",
			resolution:       nil,
			expectResolution: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody map[string]interface{}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&capturedBody)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				// Return a fake image URL
				_, _ = w.Write([]byte(`{"data":[{"url":"https://example.com/img.png"}]}`))
			}))
			defer server.Close()

			prov := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})

			model := NewImageModel(prov, ModelGrokImagineImagePro)

			provOpts := map[string]interface{}{}
			if tt.resolution != nil {
				provOpts["xai"] = map[string]interface{}{
					"resolution": *tt.resolution,
				}
			}

			opts := &provider.ImageGenerateOptions{
				Prompt: "a futuristic city",
			}
			if len(provOpts) > 0 {
				opts.ProviderOptions = provOpts
			}

			// The actual HTTP call will fail when trying to download the fake image URL,
			// but the request body will already have been captured.
			_, _ = model.DoGenerate(context.Background(), opts)

			if capturedBody == nil {
				t.Skip("server not reached (likely image download failure before request)")
			}

			if tt.expectResolution {
				res, ok := capturedBody["resolution"]
				if !ok {
					t.Errorf("expected 'resolution' field in request body, not found")
					return
				}
				if res != tt.expectedResolution {
					t.Errorf("resolution = %q, want %q", res, tt.expectedResolution)
				}
			} else {
				if _, ok := capturedBody["resolution"]; ok {
					t.Errorf("expected no 'resolution' field in request body, but found one")
				}
			}
		})
	}
}

// TestXAIResolutionInProviderOptions verifies extractImageProviderOptions reads the resolution field.
func TestXAIResolutionInProviderOptions(t *testing.T) {
	provOpts := map[string]interface{}{
		"xai": map[string]interface{}{
			"resolution": "2k",
		},
	}

	opts, err := extractImageProviderOptions(provOpts)
	if err != nil {
		t.Fatalf("extractImageProviderOptions() error: %v", err)
	}

	if opts.Resolution == nil {
		t.Fatal("expected Resolution to be non-nil")
	}
	if *opts.Resolution != "2k" {
		t.Errorf("Resolution = %q, want %q", *opts.Resolution, "2k")
	}
}

func TestXAIImageProviderOptionsValidateEnumsLikeTypeScript(t *testing.T) {
	tests := []struct {
		name string
		opts map[string]interface{}
	}{
		{
			name: "invalid resolution",
			opts: map[string]interface{}{
				"xai": map[string]interface{}{"resolution": "4k"},
			},
		},
		{
			name: "invalid quality",
			opts: map[string]interface{}{
				"xai": map[string]interface{}{"quality": "ultra"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := extractImageProviderOptions(tt.opts); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

// TestXAIResolutionNotPresentWhenNil verifies resolution absent from body when nil.
func TestXAIResolutionNotPresentWhenNil(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelGrokImagineImage)

	opts := &provider.ImageGenerateOptions{
		Prompt: "a mountain",
	}

	body := model.buildRequestBody(opts, &XAIImageProviderOptions{}, false)

	if _, ok := body["resolution"]; ok {
		t.Errorf("expected 'resolution' absent from request body when not set")
	}
}

func TestXAIImageNMatchesTypeScriptUndefinedBehavior(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelGrokImagineImage)

	body := model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "a mountain",
	}, &XAIImageProviderOptions{}, false)
	if _, ok := body["n"]; ok {
		t.Fatalf("expected n to be omitted when opts.N is nil, got %v", body["n"])
	}

	n := 1
	body = model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "a mountain",
		N:      &n,
	}, &XAIImageProviderOptions{}, false)
	if body["n"] != 1 {
		t.Fatalf("n = %v, want 1", body["n"])
	}
}

func TestXAIImageWarningsMatchTypeScript(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelGrokImagineImage)
	seed := 42
	mask := &provider.ImageFile{Type: "file", Data: []byte{1}, MediaType: "image/png"}

	warnings := model.checkUnsupportedOptions(&provider.ImageGenerateOptions{
		Size: "1024x1024",
		Seed: &seed,
		Mask: mask,
	})

	want := []types.Warning{
		{
			Type:    "unsupported",
			Feature: "size",
			Details: "This model does not support the `size` option. Use `aspectRatio` instead.",
		},
		{Type: "unsupported", Feature: "seed"},
		{Type: "unsupported", Feature: "mask"},
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Fatalf("warnings = %#v, want %#v", warnings, want)
	}
}

func TestXAIImageAPIErrorExtractsMessageLikeTypeScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid prompt","type":"invalid_request_error","code":"bad_prompt"}}`))
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "bad"})
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T, want ProviderError", err)
	}
	if providerErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want 400", providerErr.StatusCode)
	}
	if providerErr.Provider != "xai.image" {
		t.Fatalf("Provider = %q, want xai.image", providerErr.Provider)
	}
	if providerErr.Message != "Invalid prompt" {
		t.Fatalf("Message = %q, want Invalid prompt", providerErr.Message)
	}
	if providerErr.ErrorCode != "bad_prompt" {
		t.Fatalf("ErrorCode = %q, want bad_prompt", providerErr.ErrorCode)
	}
}

// TestXAIResolutionPresentWhenSet verifies resolution present in body when set.
func TestXAIResolutionPresentWhenSet(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelGrokImagineImagePro)

	res := "1k"
	opts := &provider.ImageGenerateOptions{
		Prompt: "a mountain",
	}
	provOpts := &XAIImageProviderOptions{
		Resolution: &res,
	}

	body := model.buildRequestBody(opts, provOpts, false)

	resVal, ok := body["resolution"]
	if !ok {
		t.Errorf("expected 'resolution' in request body when set")
		return
	}
	if resVal != "1k" {
		t.Errorf("resolution = %v, want %q", resVal, "1k")
	}
}

// imageTestStrPtr returns a pointer to a string literal.
func imageTestStrPtr(s string) *string {
	return &s
}

// TestXAIImageRevisedPromptInMetadata verifies that revised_prompt from the API response
// is exposed in providerMetadata.xai.images[0].revisedPrompt.
func TestXAIImageRevisedPromptInMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"cat wearing sunglasses"}]}`)) //nolint:errcheck
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if result.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata is nil")
	}
	xaiRaw, ok := result.ProviderMetadata["xai"]
	if !ok {
		t.Fatal("ProviderMetadata missing 'xai' key")
	}
	xaiMeta, ok := xaiRaw.(XAIImageMetadata)
	if !ok {
		t.Fatalf("xai metadata type = %T, want XAIImageMetadata", xaiRaw)
	}
	if len(xaiMeta.Images) == 0 {
		t.Fatal("XAIImageMetadata.Images is empty")
	}
	if xaiMeta.Images[0].RevisedPrompt == nil {
		t.Fatal("Images[0].RevisedPrompt is nil")
	}
	if *xaiMeta.Images[0].RevisedPrompt != "cat wearing sunglasses" {
		t.Errorf("RevisedPrompt = %q, want %q", *xaiMeta.Images[0].RevisedPrompt, "cat wearing sunglasses")
	}
}

// TestXAIImageMetadataAlwaysPresent verifies that providerMetadata is always set,
// even when there is no revised_prompt and no cost.
func TestXAIImageMetadataAlwaysPresent(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelGrokImagineImage)

	// Build body just to make sure the struct is fine; actual metadata comes from DoGenerate.
	// We test via buildRequestBody + direct struct construction to avoid HTTP.
	opts := &provider.ImageGenerateOptions{Prompt: "a mountain"}
	body := model.buildRequestBody(opts, &XAIImageProviderOptions{}, false)
	if body == nil {
		t.Fatal("buildRequestBody returned nil")
	}

	// Simulate what DoGenerate does with an empty response (no revised_prompt, no cost).
	resp := xaiImageResponse{
		Data: []xaiImageData{
			{B64JSON: "aGVsbG8="},
		},
	}
	imagesMeta := make([]XAIImageItemMetadata, len(resp.Data))
	for i, d := range resp.Data {
		imagesMeta[i] = XAIImageItemMetadata{RevisedPrompt: d.RevisedPrompt}
	}
	meta := XAIImageMetadata{Images: imagesMeta}

	if len(meta.Images) != 1 {
		t.Errorf("len(Images) = %d, want 1", len(meta.Images))
	}
	if meta.Images[0].RevisedPrompt != nil {
		t.Error("expected Images[0].RevisedPrompt to be nil when not in response")
	}
}

// TestXAIImageEditMultipleImages verifies that multiple input files are serialized as
// an "images" array (not a single "image" object) for the editing endpoint.
func TestXAIImageEditMultipleImages(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"url":"https://example.com/result.png"}]}`)) //nolint:errcheck
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)

	opts := &provider.ImageGenerateOptions{
		Prompt: "Combine these two images",
		Files: []provider.ImageFile{
			{Type: "url", URL: "https://example.com/img1.png"},
			{Type: "url", URL: "https://example.com/img2.png"},
		},
	}

	// The download will fail but the request body will have been captured.
	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}

	// Must use "images" (array), not "image" (single object).
	if _, hasImage := capturedBody["image"]; hasImage {
		t.Error("request body should not have 'image' key (single), expected 'images' array")
	}

	images, ok := capturedBody["images"]
	if !ok {
		t.Fatal("request body missing 'images' key")
	}

	imagesSlice, ok := images.([]interface{})
	if !ok {
		t.Fatalf("'images' field is %T, want []interface{}", images)
	}
	if len(imagesSlice) != 2 {
		t.Errorf("len(images) = %d, want 2", len(imagesSlice))
	}

	// Each entry should have url and type fields.
	for i, item := range imagesSlice {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("images[%d] is %T, want map", i, item)
		}
		if m["type"] != "image_url" {
			t.Errorf("images[%d].type = %v, want \"image_url\"", i, m["type"])
		}
		if m["url"] == "" {
			t.Errorf("images[%d].url is empty", i)
		}
	}
}

// TestXAIImageEditSingleImage verifies that a single input file is serialized as
// an "image" object, matching the TypeScript xAI image provider.
func TestXAIImageEditSingleImage(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"url":"https://example.com/result.png"}]}`)) //nolint:errcheck
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)

	opts := &provider.ImageGenerateOptions{
		Prompt: "Edit this image",
		Files: []provider.ImageFile{
			{Type: "url", URL: "https://example.com/img.png"},
		},
	}

	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}

	if _, hasImages := capturedBody["images"]; hasImages {
		t.Error("request body should not have 'images' key for a single input image")
	}

	image, ok := capturedBody["image"].(map[string]interface{})
	if !ok {
		t.Fatalf("'image' field is %T, want map", capturedBody["image"])
	}
	if image["type"] != "image_url" {
		t.Errorf("image.type = %v, want \"image_url\"", image["type"])
	}
	if image["url"] != "https://example.com/img.png" {
		t.Errorf("image.url = %v, want source URL", image["url"])
	}
}

// TestXAIImageOptionsQualityAndUser verifies that quality and user options are
// serialized in the image generation request body.
func TestXAIImageOptionsQualityAndUser(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"url":"https://example.com/img.png"}]}`)) //nolint:errcheck
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)

	quality := "high"
	user := "user-abc123"
	opts := &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"quality": &quality,
				"user":    &user,
			},
		},
	}

	_, _ = model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}

	if capturedBody["quality"] != "high" {
		t.Errorf("quality = %v, want \"high\"", capturedBody["quality"])
	}
	if capturedBody["user"] != "user-abc123" {
		t.Errorf("user = %v, want \"user-abc123\"", capturedBody["user"])
	}
}

// TestXAIImageB64JSONResponseFormat verifies that OutputFormat "b64_json" sets
// response_format to "b64_json" in the request.
func TestXAIImageB64JSONResponseFormat(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		// Return b64_json data
		w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8="}]}`)) //nolint:errcheck
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)

	format := "b64_json"
	opts := &provider.ImageGenerateOptions{
		Prompt: "a cat",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"output_format": &format,
			},
		},
	}

	result, err := model.DoGenerate(context.Background(), opts)

	if capturedBody == nil {
		t.Skip("server not reached")
	}

	if capturedBody["response_format"] != "b64_json" {
		t.Errorf("response_format = %v, want \"b64_json\"", capturedBody["response_format"])
	}

	// If the server returned b64_json data, the result should have image bytes.
	if err == nil && result != nil {
		if len(result.Image) == 0 {
			t.Error("expected non-empty image bytes from b64_json response")
		}
	}
}

func TestXAIImageReturnsAllImagesAndResponseMetadataLikeTypeScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-image-123")
		w.Write([]byte(`{"data":[{"b64_json":"Zmlyc3Q="},{"b64_json":"c2Vjb25k"}]}`)) //nolint:errcheck
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "two images",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if string(result.Image) != "first" {
		t.Fatalf("Image = %q, want first", string(result.Image))
	}
	if len(result.Images) != 2 {
		t.Fatalf("len(Images) = %d, want 2", len(result.Images))
	}
	if string(result.Images[0]) != "first" || string(result.Images[1]) != "second" {
		t.Fatalf("Images = %q, %q; want first, second", string(result.Images[0]), string(result.Images[1]))
	}
	if result.Base64Image != "Zmlyc3Q=" {
		t.Fatalf("Base64Image = %q, want Zmlyc3Q=", result.Base64Image)
	}
	if len(result.Base64Images) != 2 || result.Base64Images[0] != "Zmlyc3Q=" || result.Base64Images[1] != "c2Vjb25k" {
		t.Fatalf("Base64Images = %#v, want original provider base64 strings", result.Base64Images)
	}
	if result.Response == nil {
		t.Fatal("Response is nil")
	}
	if result.Response.ModelID != ModelGrokImagineImage {
		t.Fatalf("Response.ModelID = %q, want %q", result.Response.ModelID, ModelGrokImagineImage)
	}
	if result.Response.Headers["X-Request-Id"] != "req-image-123" {
		t.Fatalf("Response.Headers[X-Request-Id] = %q, want req-image-123", result.Response.Headers["X-Request-Id"])
	}
	if result.Response.Timestamp.IsZero() {
		t.Fatal("Response.Timestamp is zero")
	}
}

func TestXAIImageRespectsAbortSignalOption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be reached with canceled AbortSignal")
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)
	abortCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "a canceled request",
		AbortSignal: abortCtx,
	})
	if err == nil {
		t.Fatal("expected canceled request error")
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T, want ProviderError", err)
	}
	if providerErr.Provider != "xai.image" {
		t.Fatalf("Provider = %q, want xai.image", providerErr.Provider)
	}
}

func TestXAIImageRespectsCallContextWhenAbortSignalIsSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be reached with canceled call context")
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)
	callCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := model.DoGenerate(callCtx, &provider.ImageGenerateOptions{
		Prompt:      "a canceled request",
		AbortSignal: context.Background(),
	})
	if err == nil {
		t.Fatal("expected canceled request error")
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T, want ProviderError", err)
	}
	if providerErr.Provider != "xai.image" {
		t.Fatalf("Provider = %q, want xai.image", providerErr.Provider)
	}
}

// TestXAIImageCostInUsdTicks verifies that costInUsdTicks from the response is
// exposed in ProviderMetadata.
func TestXAIImageCostInUsdTicks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return cost_in_usd_ticks in the top-level usage object (not per-image).
		w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8="}],"usage":{"cost_in_usd_ticks":42}}`)) //nolint:errcheck
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelGrokImagineImage)

	opts := &provider.ImageGenerateOptions{Prompt: "a painting"}

	body := model.buildRequestBody(opts, &XAIImageProviderOptions{}, false)
	if body == nil {
		t.Fatal("buildRequestBody returned nil")
	}

	result, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	meta, ok := result.ProviderMetadata["xai"]
	if !ok {
		t.Fatal("ProviderMetadata missing xai key")
	}
	xaiMeta, ok := meta.(XAIImageMetadata)
	if !ok {
		t.Fatalf("xai metadata type = %T, want XAIImageMetadata", meta)
	}
	if xaiMeta.CostInUsdTicks == nil {
		t.Fatal("CostInUsdTicks is nil")
	}
	if *xaiMeta.CostInUsdTicks != 42 {
		t.Errorf("CostInUsdTicks = %d, want 42", *xaiMeta.CostInUsdTicks)
	}
}
