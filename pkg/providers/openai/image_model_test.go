package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// BUG-T17: models whose IDs begin with recognized prefixes (chatgpt-image, gpt-image-1, etc.)
// manage their own response format and must NOT receive an explicit
// response_format=b64_json in the request body (#12838).
func TestHasDefaultResponseFormat(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		// Models that have a built-in default — must not get response_format override.
		{"chatgpt-image-1", true},
		{"chatgpt-image-latest", true},
		{"gpt-image-1", true},
		{"gpt-image-1-mini", true},
		{"gpt-image-1.5", true},
		{"gpt-image-1.5-turbo", true},
		{"gpt-image-2", true},
		// Classic DALL-E models that need the explicit b64_json format.
		{"dall-e-3", false},
		{"dall-e-2", false},
		{"dall-e-2-hd", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := hasDefaultResponseFormat(tt.modelID)
			if got != tt.expected {
				t.Errorf("hasDefaultResponseFormat(%q) = %v, want %v", tt.modelID, got, tt.expected)
			}
		})
	}
}

type imageDownloadHeaderTransport struct {
	base http.RoundTripper
}

func (t imageDownloadHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("X-Custom-Download-Client", "yes")
	return t.base.RoundTrip(req)
}

func TestImageModel_DoGenerate_WithFilesUsesEditsMultipart(t *testing.T) {
	var requestPath string
	var fields map[string][]string
	var files []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("MultipartReader error = %v", err)
		}
		fields = map[string][]string{}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			if part.FileName() != "" {
				files = append(files, part.FormName())
				continue
			}
			buf := make([]byte, 1024)
			n, _ := part.Read(buf)
			fields[part.FormName()] = append(fields[part.FormName()], string(buf[:n]))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1"})
	model := NewImageModel(p, "gpt-image-1")
	n := 1
	compression := 80

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "edit this",
		Files: []provider.ImageFile{
			{Type: "file", MediaType: "image/png", Data: []byte{137, 80, 78, 71}},
			{Type: "file", MediaType: "image/jpeg", Data: []byte{255, 216, 255, 224}},
		},
		N:    &n,
		Size: "1024x1024",
		ProviderOptions: map[string]interface{}{
			"openai": OpenAIImageProviderOptions{
				Quality:           "high",
				Background:        "transparent",
				OutputFormat:      "webp",
				OutputCompression: &compression,
				InputFidelity:     "high",
				User:              "user-123",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if requestPath != "/v1/images/edits" {
		t.Fatalf("path = %q, want /v1/images/edits", requestPath)
	}
	if string(result.Image) != "hello" {
		t.Fatalf("image = %q, want hello", string(result.Image))
	}
	for key, want := range map[string]string{
		"model":              "gpt-image-1",
		"prompt":             "edit this",
		"n":                  "1",
		"size":               "1024x1024",
		"quality":            "high",
		"background":         "transparent",
		"output_format":      "webp",
		"output_compression": "80",
		"input_fidelity":     "high",
		"user":               "user-123",
	} {
		if got := fields[key][0]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if len(files) != 2 || files[0] != "image[]" || files[1] != "image[]" {
		t.Fatalf("file fields = %#v, want two image[] fields", files)
	}
}

func TestImageModel_DoGenerate_EditURLUsesConfiguredHTTPClient(t *testing.T) {
	var downloadHeader string
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloadHeader = r.Header.Get("X-Custom-Download-Client")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{137, 80, 78, 71})
	}))
	defer imageServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.MultipartReader(); err != nil {
			t.Fatalf("MultipartReader error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer apiServer.Close()

	p := New(Config{
		APIKey:  "test-key",
		BaseURL: apiServer.URL + "/v1",
		HTTPClient: &http.Client{Transport: imageDownloadHeaderTransport{
			base: http.DefaultTransport,
		}},
	})
	model := NewImageModel(p, "gpt-image-1")

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "edit this",
		Files: []provider.ImageFile{{
			Type:      "url",
			URL:       imageServer.URL + "/image.png",
			MediaType: "image/png",
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if downloadHeader != "yes" {
		t.Fatalf("image download did not use configured HTTP client transport; header = %q", downloadHeader)
	}
}

func TestImageModel_DoGenerate_InvalidTypedProviderOptions(t *testing.T) {
	p := New(Config{APIKey: "test-key", BaseURL: "http://127.0.0.1:1/v1"})
	model := NewImageModel(p, "gpt-image-1")
	compression := 101

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat",
		ProviderOptions: map[string]interface{}{
			"openai": OpenAIImageProviderOptions{OutputCompression: &compression},
		},
	})
	if err == nil {
		t.Fatal("DoGenerate should reject invalid outputCompression before making a request")
	}
}

func TestImageModel_DoGenerate_EditWarnings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1"})
	model := NewImageModel(p, "gpt-image-1")
	seed := 42

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "edit",
		Files:       []provider.ImageFile{{Type: "file", MediaType: "image/png", Data: []byte{1}}},
		AspectRatio: "16:9",
		Seed:        &seed,
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %#v, want 2", result.Warnings)
	}
	if result.Warnings[0].Feature != "aspectRatio" || result.Warnings[1].Feature != "seed" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestBuildRequestBody_NoResponseFormatForChatGPTImage verifies that the
// response_format field is absent from the request body for chatgpt-image models.
func TestBuildRequestBody_NoResponseFormatForChatGPTImage(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewImageModel(p, "chatgpt-image-1")

	body := model.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "a cat"})

	if _, ok := body["response_format"]; ok {
		t.Errorf("response_format must not be set for chatgpt-image models, got %v", body["response_format"])
	}
}

// TestBuildRequestBody_ResponseFormatSetForDALLE verifies that dall-e models
// still receive response_format=b64_json.
func TestBuildRequestBody_ResponseFormatSetForDALLE(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewImageModel(p, "dall-e-3")

	body := model.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "a cat"})

	rf, ok := body["response_format"]
	if !ok {
		t.Errorf("response_format must be set for dall-e models")
	}
	if rf != "b64_json" {
		t.Errorf("response_format = %v, want b64_json", rf)
	}
}

func TestImageModelBuildRequestBodyTypedProviderOptions(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewImageModel(p, "gpt-image-1")
	compression := 80

	body := model.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt:  "a cat",
		Quality: "standard",
		ProviderOptions: map[string]interface{}{
			"openai": OpenAIImageProviderOptions{
				Quality:           "high",
				Background:        "transparent",
				Moderation:        "low",
				OutputFormat:      "webp",
				OutputCompression: &compression,
				User:              "user-123",
			},
		},
	})

	if body["quality"] != "high" {
		t.Fatalf("quality = %v, want high", body["quality"])
	}
	if body["background"] != "transparent" {
		t.Fatalf("background = %v, want transparent", body["background"])
	}
	if body["moderation"] != "low" {
		t.Fatalf("moderation = %v, want low", body["moderation"])
	}
	if body["output_format"] != "webp" {
		t.Fatalf("output_format = %v, want webp", body["output_format"])
	}
	if body["output_compression"] != compression {
		t.Fatalf("output_compression = %v, want %d", body["output_compression"], compression)
	}
	if body["user"] != "user-123" {
		t.Fatalf("user = %v, want user-123", body["user"])
	}
}

func TestImageModel_DoGenerate_UsesVersionRelativePath(t *testing.T) {
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1"})
	model := NewImageModel(p, "dall-e-3")

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if requestPath != "/v1/images/generations" {
		t.Fatalf("path = %q, want /v1/images/generations", requestPath)
	}
}
