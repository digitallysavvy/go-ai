package perplexity

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestPerplexityProvider_FactoriesAndUnsupported(t *testing.T) {
	t.Parallel()

	p := CreatePerplexity(Config{APIKey: "k", BaseURL: "https://example.test"})
	if p.Name() != "perplexity" {
		t.Fatalf("Name() = %q, want perplexity", p.Name())
	}
	if p.Client() == nil {
		t.Fatal("Client() returned nil")
	}

	modelAny, err := p.LanguageModel("")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if modelAny.ModelID() != "llama-3.1-sonar-small-128k-online" {
		t.Fatalf("default model ID = %q", modelAny.ModelID())
	}

	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("EmbeddingModel expected unsupported error")
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("ImageModel expected unsupported error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel expected unsupported error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel expected unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel expected unsupported error")
	}
}

func TestPerplexityLanguageModel_MetadataAndErrorWrap(t *testing.T) {
	t.Parallel()

	m := NewLanguageModel(New(Config{APIKey: "k"}), "sonar")
	if m.SpecificationVersion() != "v3" {
		t.Fatalf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.Provider() != "perplexity" || m.ModelID() != "sonar" {
		t.Fatalf("provider/model mismatch: %s/%s", m.Provider(), m.ModelID())
	}
	if m.SupportsTools() || m.SupportsStructuredOutput() || m.SupportsImageInput() {
		t.Fatal("unexpected capabilities for Perplexity model")
	}

	err := m.handleError(errors.New("boom"))
	if err == nil || !strings.Contains(err.Error(), "perplexity provider error") {
		t.Fatalf("handleError mismatch: %v", err)
	}
}

func TestPerplexityDoStreamAndUsageDetails(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	m := NewLanguageModel(p, "sonar")
	reasoning := types.ReasoningHigh
	stream, err := m.DoStream(t.Context(), &provider.GenerateOptions{Reasoning: &reasoning})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}

	// first emitted chunk may be stream-start (warnings wrapper), then text
	var sawText bool
	for {
		ch, err := stream.Next()
		if err != nil {
			break
		}
		if ch.Type == provider.ChunkTypeText && ch.Text == "Hello" {
			sawText = true
		}
	}
	_ = stream.Close()
	if !sawText {
		t.Fatal("expected text chunk from stream")
	}
}

func TestPerplexityUsageAndStreamCtorHelpers(t *testing.T) {
	t.Parallel()

	usage := convertPerplexityUsage(perplexityUsage{
		PromptTokens:     12,
		CompletionTokens: 8,
		TotalTokens:      20,
		PromptTokensDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
			AudioTokens  *int `json:"audio_tokens,omitempty"`
			TextTokens   *int `json:"text_tokens,omitempty"`
			ImageTokens  *int `json:"image_tokens,omitempty"`
		}{CachedTokens: intPtr(3), TextTokens: intPtr(9), ImageTokens: intPtr(3)},
		CompletionTokensDetails: &struct {
			ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
			AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
			RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
		}{ReasoningTokens: intPtr(2)},
	})
	if usage.InputDetails == nil || usage.OutputDetails == nil {
		t.Fatalf("expected usage details, got %+v", usage)
	}

	stream := newPerplexityStream(io.NopCloser(strings.NewReader("")))
	if stream == nil || stream.OpenAICompatStream == nil {
		t.Fatal("newPerplexityStream() returned nil")
	}
}

func intPtr(v int) *int { return &v }
