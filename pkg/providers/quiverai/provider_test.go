package quiverai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestProviderFactories(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.Name() != "quiverai" {
		t.Fatalf("Name = %q", p.Name())
	}
	model, err := p.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel: %v", err)
	}
	if model.Provider() != "quiverai.image" || model.ModelID() != ModelArrow11 {
		t.Fatalf("unexpected model metadata: provider=%q id=%q", model.Provider(), model.ModelID())
	}
	if _, err := p.LanguageModel("x"); err == nil {
		t.Fatal("LanguageModel should be unsupported")
	}
}

func TestImageModelGenerateRequestAndResponse(t *testing.T) {
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/svgs/generations" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Fatalf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"svg-1","created":1700000000,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model := NewImageModel(p, ModelArrow11Max)
	n := 2
	result, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "logo",
		N:      &n,
		Files:  []provider.ImageFile{{Data: []byte("ref"), MediaType: "image/png"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{
				"instructions": "flat",
				"temperature":  0.3,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["model"] != ModelArrow11Max || seenBody["n"] != float64(2) || seenBody["prompt"] != "logo" {
		t.Fatalf("unexpected request body: %#v", seenBody)
	}
	if refs, ok := seenBody["references"].([]interface{}); !ok || len(refs) != 1 {
		t.Fatalf("references missing: %#v", seenBody)
	}
	if string(result.Image) != "<svg/>" || result.MimeType != "image/svg+xml" {
		t.Fatalf("unexpected image result: mime=%q image=%q", result.MimeType, string(result.Image))
	}
	if result.Response == nil || result.Response.ID != "svg-1" || result.Response.ModelID != ModelArrow11Max {
		t.Fatalf("response metadata missing: %#v", result.Response)
	}
	if result.Usage.InputTokens != 1 || result.Usage.OutputTokens != 2 || result.Usage.TotalTokens != 3 {
		t.Fatalf("usage = %#v", result.Usage)
	}
}

func TestImageModelVectorizeRequest(t *testing.T) {
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/svgs/vectorizations" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"svg-2","created":1700000000,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml"}]}`))
	}))
	defer server.Close()

	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow11)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Files: []provider.ImageFile{{URL: "https://example.test/image.png", Type: "url"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{
				"operation":  "vectorize",
				"autoCrop":   true,
				"targetSize": 512,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["auto_crop"] != true || seenBody["target_size"] != float64(512) {
		t.Fatalf("vectorize options missing: %#v", seenBody)
	}
	if _, ok := seenBody["image"].(map[string]interface{}); !ok {
		t.Fatalf("image reference missing: %#v", seenBody)
	}
}

func TestImageModelGenerateRejectsWhitespacePrompt(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow11)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{Prompt: " \t\n "})
	if err == nil {
		t.Fatal("expected whitespace-only prompt to be rejected")
	}
}

func TestImageModelRetryableProviderErrors(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"code":"temporary","message":"try again"}`))
		}))

		model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow11)
		_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{Prompt: "logo"})
		server.Close()
		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		var providerErr *providererrors.ProviderError
		if !errors.As(err, &providerErr) {
			t.Fatalf("status %d: error = %T, want ProviderError", status, err)
		}
		if !providerErr.IsRetryable() {
			t.Fatalf("status %d should be retryable", status)
		}
	}
}
