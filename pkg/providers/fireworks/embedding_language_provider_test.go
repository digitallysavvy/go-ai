package fireworks

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

func TestFireworksProviderSurface(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if CreateFireworks(Config{APIKey: "k"}).Name() != "fireworks" || p.Name() != "fireworks" {
		t.Fatalf("provider name mismatch")
	}
	if p.Client() == nil {
		t.Fatal("expected client")
	}
	lm, _ := p.LanguageModel("")
	if lm.ModelID() == "" {
		t.Fatal("expected default LM id")
	}
	em, _ := p.EmbeddingModel("")
	if em.ModelID() == "" {
		t.Fatal("expected default embedding id")
	}
	im, _ := p.ImageModel("")
	if im.ModelID() == "" {
		t.Fatal("expected default image id")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("expected unsupported speech")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("expected unsupported transcription")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("expected unsupported reranking")
	}
}

func TestFireworksEmbeddingModelDoEmbedMany(t *testing.T) {
	var seenBody map[string]interface{}
	p := newFireworksProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"X-Request-Id": []string{"req1"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"index":0,"embedding":[1,2]},{"index":1,"embedding":[3,4]}],"usage":{"prompt_tokens":7,"total_tokens":9}}`)),
		}, nil
	})
	m := NewEmbeddingModel(p, "nomic-embed")
	if m.SpecificationVersion() != "v3" || m.Provider() != "fireworks" || m.ModelID() != "nomic-embed" {
		t.Fatalf("metadata mismatch")
	}
	if m.MaxEmbeddingsPerCall() != 2048 || !m.SupportsParallelCalls() {
		t.Fatalf("limits mismatch")
	}
	out, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, &provider.EmbedModelOptions{Headers: map[string]string{"X-Test": "1"}})
	if err != nil {
		t.Fatalf("DoEmbedMany error = %v", err)
	}
	if seenBody["model"] != "nomic-embed" || len(out.Embeddings) != 2 || out.Usage.Tokens != 7 || out.Usage.InputTokens != 7 || out.Usage.TotalTokens != 9 {
		t.Fatalf("embedding result mismatch body=%#v out=%#v", seenBody, out)
	}
}

func TestFireworksLanguageModelDoGenerateAndDoStream(t *testing.T) {
	p := newFireworksProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := `data: {"choices":[{"delta":{"reasoning_content":"r"},"finish_reason":""}]}

data: {"choices":[{"delta":{"content":"hello"},"finish_reason":""}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(sse))}, nil
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"X-Request-Id": []string{"req1"}},
			Body: io.NopCloser(strings.NewReader(`{
				"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"ok","reasoning_content":"r","tool_calls":[{"id":"tc1","type":"function","function":{"name":"lookup","arguments":"{\"x\":1}"}}]}}],
				"usage":{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":3}}
			}`)),
		}, nil
	})
	m := NewLanguageModel(p, "accounts/fireworks/models/mixtral-8x7b-instruct")
	if m.SpecificationVersion() != "v3" || m.Provider() != "fireworks" {
		t.Fatalf("metadata mismatch")
	}
	if !m.SupportsTools() || !m.SupportsStructuredOutput() || m.SupportsImageInput() {
		t.Fatalf("capability mismatch")
	}
	reason := types.ReasoningHigh
	out, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &reason,
		ProviderOptions: map[string]interface{}{
			"thinking":         map[string]interface{}{"type": "enabled", "budgetTokens": 128},
			"reasoningHistory": "all",
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if out.Text != "ok" || out.FinishReason != types.FinishReasonToolCalls || len(out.ToolCalls) != 1 || len(out.Content) != 1 {
		t.Fatalf("generate result mismatch: %#v", out)
	}
	if out.ResponseHeaders["X-Request-Id"] != "req1" && out.ResponseHeaders["x-request-id"] != "req1" {
		t.Fatalf("response headers mismatch: %#v", out.ResponseHeaders)
	}

	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck
	gotFinish := false
	for {
		ch, e := stream.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatalf("stream err = %v", e)
		}
		if ch.Type == provider.ChunkTypeFinish {
			gotFinish = true
		}
	}
	if !gotFinish {
		t.Fatalf("expected finish chunk")
	}
}

func TestFireworksErrorPathsAndHelpers(t *testing.T) {
	p := newFireworksProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	if _, err := NewEmbeddingModel(p, "m").DoEmbed(context.Background(), "x", nil); err == nil {
		t.Fatal("expected embedding error")
	}
	if _, err := NewLanguageModel(p, "m").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}}); err == nil {
		t.Fatal("expected language error")
	}
	if h := optsEmbedHeaders(nil); h != nil {
		t.Fatalf("optsEmbedHeaders(nil) = %#v", h)
	}
}

type fireworksRoundTripper func(*http.Request) (*http.Response, error)

func (f fireworksRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newFireworksProviderWithTransport(t *testing.T, rt fireworksRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://fireworks.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://fireworks.example",
		Headers: map[string]string{
			"Authorization": "Bearer k",
			"Content-Type":  "application/json",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}
