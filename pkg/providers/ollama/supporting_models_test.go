package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestOllamaEmbeddingModelMetadataAndDoEmbed(t *testing.T) {
	var seenBody map[string]interface{}
	p := newOllamaProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"X-Request-Id": []string{"req1"}},
			Body:       io.NopCloser(strings.NewReader(`{"object":"list","data":[{"index":0,"embedding":[1,2]}],"usage":{"prompt_tokens":3,"total_tokens":4}}`)),
		}, nil
	})
	m := NewEmbeddingModel(p, "nomic-embed")
	if m.SpecificationVersion() != "v3" || m.Provider() != "ollama" || m.ModelID() != "nomic-embed" {
		t.Fatalf("metadata mismatch: spec=%s provider=%s model=%s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}
	if m.MaxEmbeddingsPerCall() != 1 || !m.SupportsParallelCalls() {
		t.Fatalf("limits mismatch: max=%d parallel=%v", m.MaxEmbeddingsPerCall(), m.SupportsParallelCalls())
	}
	out, err := m.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{Headers: map[string]string{"X-Test": "1"}})
	if err != nil {
		t.Fatalf("DoEmbed error = %v", err)
	}
	if seenBody["model"] != "nomic-embed" {
		t.Fatalf("request body mismatch: %#v", seenBody)
	}
	if len(out.Embedding) != 2 || out.Usage.Tokens != 3 || out.Usage.InputTokens != 3 || out.Usage.TotalTokens != 4 {
		t.Fatalf("result mismatch: %#v", out)
	}
}

func TestOllamaEmbeddingModelErrorAndOptsHeaders(t *testing.T) {
	p := newOllamaProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	if _, err := NewEmbeddingModel(p, "m").DoEmbedMany(context.Background(), []string{"a"}, nil); err == nil {
		t.Fatal("expected provider error")
	}
	if h := optsHeaders(nil); h != nil {
		t.Fatalf("optsHeaders(nil) = %#v", h)
	}
}

func TestOllamaLanguageModelBuildConvertAndUsage(t *testing.T) {
	m := NewLanguageModel(New(Config{BaseURL: "http://localhost:11434"}), "llama3")
	temp := float64(0.2)
	maxTokens := 12
	topP := float64(0.9)
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}},
			System:   "sys",
		},
		MaxTokens:   &maxTokens,
		Temperature: &temp,
		TopP:        &topP,
		ResponseFormat: &provider.ResponseFormat{
			Type: "json_object",
		},
		StopSequences: []string{"stop"},
	}, true)
	if body["model"] != "llama3" || body["stream"] != true {
		t.Fatalf("body mismatch: %#v", body)
	}
	if _, ok := body["messages"].([]map[string]interface{}); !ok {
		t.Fatalf("messages type mismatch: %#v", body["messages"])
	}
	if body["max_tokens"] != 12 || body["temperature"] != temp || body["top_p"] != topP {
		t.Fatalf("options mismatch: %#v", body)
	}
	if _, ok := body["response_format"].(map[string]interface{}); !ok {
		t.Fatalf("response_format mismatch: %#v", body["response_format"])
	}

	resp := ollamaResponse{
		Choices: []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				FinishReason: "stop",
				Message: struct {
					Role      string `json:"role"`
					Content   string `json:"content"`
					ToolCalls []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				}{
					Content: "done",
					ToolCalls: []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					}{
						{
							ID:   "tc1",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      "lookup",
								Arguments: `{"x":1}`,
							},
						},
					},
				},
			},
		},
		Usage: ollamaUsage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9},
	}
	out := m.convertResponse(resp)
	if out.Text != "done" || len(out.ToolCalls) != 1 || out.ToolCalls[0].ToolName != "lookup" {
		t.Fatalf("convert mismatch: %#v", out)
	}

	cached := 2
	reason := 1
	usage := convertOllamaUsage(ollamaUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		PromptTokensDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
		}{CachedTokens: &cached},
		CompletionTokensDetails: &struct {
			ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
			AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
			RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
		}{ReasoningTokens: &reason},
	})
	if usage.InputDetails == nil || usage.OutputDetails == nil || usage.Raw == nil {
		t.Fatalf("usage details missing: %#v", usage)
	}
}

func TestOllamaLanguageModelDoGenerateError(t *testing.T) {
	p := newOllamaProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	if _, err := NewLanguageModel(p, "llama3").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}}); err == nil {
		t.Fatal("expected provider error")
	}
}

type ollamaRoundTripper func(*http.Request) (*http.Response, error)

func (f ollamaRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newOllamaProviderWithTransport(t *testing.T, rt ollamaRoundTripper) *Provider {
	t.Helper()
	p := New(Config{BaseURL: "https://ollama.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://ollama.example",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}
