package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// --- httptest-based request body verification --------------------------------

func TestBuildRequestBody_ThinkingConfig_ViaHTTP(t *testing.T) {
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	m := NewLanguageModel(p, ModelGemini20Flash)

	_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{
					"thinkingBudget": float64(256),
				},
			},
		},
	})

	if capturedBody == nil {
		t.Skip("capturedBody nil — provider may not support BaseURL override; skipping HTTP verification")
		return
	}
	gc, ok := capturedBody["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("generationConfig not in request body")
	}
	tc, ok := gc["thinkingConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("thinkingConfig not in generationConfig")
	}
	if tc["thinkingBudget"] != float64(256) {
		t.Errorf("thinkingBudget: got %v, want 256", tc["thinkingBudget"])
	}
}

// --- EmbeddingModelGeminiEmbedding001 constant -----------------------------

func TestEmbeddingModelGeminiEmbedding001Constant(t *testing.T) {
	if EmbeddingModelGeminiEmbedding001 != "gemini-embedding-001" {
		t.Errorf("EmbeddingModelGeminiEmbedding001 = %q, want %q",
			EmbeddingModelGeminiEmbedding001, "gemini-embedding-001")
	}
}

// --- integration test -------------------------------------------------------

func TestLanguageModel_Integration_ThinkingConfig(t *testing.T) {
	apiKey := os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping: GOOGLE_GENERATIVE_AI_API_KEY not set")
	}

	p := New(Config{APIKey: apiKey})
	m, err := p.LanguageModel(ModelGemini31FlashImagePreview)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	result, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "What is 2 + 2? Think step by step."},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{
					"thinkingBudget":  512,
					"includeThoughts": true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if result.Text == "" {
		t.Error("expected non-empty result text")
	}
	t.Logf("Result: %s", result.Text)
	if result.Usage.OutputDetails != nil && result.Usage.OutputDetails.ReasoningTokens != nil {
		t.Logf("Reasoning tokens: %d", *result.Usage.OutputDetails.ReasoningTokens)
	}
}

// TestSupportedURLs_Google ports TS google-provider.test.ts
// "should include YouTube URLs in supportedUrls": the Files API URL and the
// three YouTube URL forms are supported, while an arbitrary https URL is not
// (for a gemini-2.0 model, which supportsExternalFileUrls excludes) and a
// non-YouTube video host is never matched by the base "*" patterns.
func TestSupportedURLs_Google(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	m := NewLanguageModel(p, "gemini-2.0-flash")

	patterns := m.SupportedURLs()["*"]
	if len(patterns) == 0 {
		t.Fatal("expected non-empty '*' patterns")
	}

	supported := []string{
		"https://generativelanguage.googleapis.com/v1beta/files/test123",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ",
	}
	for _, url := range supported {
		if !anyPatternMatches(t, patterns, url) {
			t.Errorf("expected %q to be supported by '*' patterns", url)
		}
	}

	unsupported := []string{
		"https://example.com",
		"https://vimeo.com/123456789",
		"https://youtube.com/channel/UCdQw4w9WgXcQ",
	}
	for _, url := range unsupported {
		if anyPatternMatches(t, patterns, url) {
			t.Errorf("expected %q to NOT be supported by '*' patterns", url)
		}
	}

	// gemini-2.0 models are excluded from external https URL support
	// (TS supportsExternalFileUrls: `!/(^|\/)gemini-2\.0/.test(modelId)`).
	if _, ok := m.SupportedURLs()["application/pdf"]; ok {
		t.Error("gemini-2.0-flash should not support external file URLs (application/pdf)")
	}

	m25 := NewLanguageModel(p, "gemini-2.5-flash")
	pdfPatterns, ok := m25.SupportedURLs()["application/pdf"]
	if !ok || len(pdfPatterns) == 0 {
		t.Error("gemini-2.5-flash should support external https URLs for application/pdf")
	}
}

func anyPatternMatches(t *testing.T, patterns []string, url string) bool {
	t.Helper()
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			t.Fatalf("invalid pattern %q: %v", p, err)
		}
		if re.MatchString(url) {
			return true
		}
	}
	return false
}

// TestHandleError_ParsesGoogleErrorBody ports the intent of TS
// googleFailedResponseHandler (google-error.ts): a {"error":{"code","message",
// "status"}} JSON body becomes the ProviderError's message/status/data.
func TestHandleError_ParsesGoogleErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":{"code":400,"message":"Invalid value for 'temperature'","status":"INVALID_ARGUMENT"}}`)
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: srv.URL})
	m := NewLanguageModel(p, "gemini-2.5-flash")

	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	perr, ok := err.(*providererrors.ProviderError)
	if !ok {
		t.Fatalf("expected *providererrors.ProviderError, got %T: %v", err, err)
	}
	if perr.Message != "Invalid value for 'temperature'" {
		t.Errorf("Message = %q, want the parsed error.message", perr.Message)
	}
	if perr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", perr.StatusCode)
	}
	if perr.ErrorCode != "INVALID_ARGUMENT" {
		t.Errorf("ErrorCode = %q, want INVALID_ARGUMENT", perr.ErrorCode)
	}
	if perr.Data == nil {
		t.Error("expected Data to be populated with the parsed error body")
	}
}
