package groq

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

func TestGroqProviderSurface(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if CreateGroq(Config{APIKey: "k"}).Name() != "groq" || p.Name() != "groq" {
		t.Fatalf("provider name mismatch")
	}
	if p.Client() == nil {
		t.Fatal("expected client")
	}
	lm, err := p.LanguageModel("")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if lm.ModelID() != "mixtral-8x7b-32768" {
		t.Fatalf("default model mismatch: %s", lm.ModelID())
	}
	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("expected unsupported embedding")
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("expected unsupported image")
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

func TestGroqLanguageModelDoGenerateAndConvertUsage(t *testing.T) {
	var seenPath string
	var seenBody map[string]interface{}
	p := newGroqProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"X-Request-Id": []string{"req1"}},
			Body: io.NopCloser(strings.NewReader(`{
				"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"hello","reasoning":"think","tool_calls":[{"id":"tc1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}}],
				"usage":{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18,"prompt_tokens_details":{"cached_tokens":2,"text_tokens":7,"image_tokens":1},"completion_tokens_details":{"reasoning_tokens":3}}
			}`)),
		}, nil
	})
	m := NewLanguageModel(p, "llama-3.3-70b-versatile")
	if m.SpecificationVersion() != "v3" || m.Provider() != "groq" || m.ModelID() != "llama-3.3-70b-versatile" {
		t.Fatalf("metadata mismatch")
	}
	if !m.SupportsTools() || !m.SupportsStructuredOutput() || m.SupportsImageInput() {
		t.Fatalf("capabilities mismatch")
	}

	topP := 0.9
	temp := 0.2
	maxTokens := 123
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:        types.Prompt{Text: "hi", System: "sys"},
		TopP:          &topP,
		Temperature:   &temp,
		MaxTokens:     &maxTokens,
		StopSequences: []string{"END"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenBody["model"] != "llama-3.3-70b-versatile" || seenBody["max_tokens"] != float64(123) {
		t.Fatalf("request mismatch: %#v", seenBody)
	}
	if res.Text != "hello" || res.FinishReason != types.FinishReasonToolCalls || len(res.ToolCalls) != 1 || len(res.Content) != 1 {
		t.Fatalf("result mismatch: %#v", res)
	}
	if res.ResponseHeaders["x-request-id"] != "req1" && res.ResponseHeaders["X-Request-Id"] != "req1" {
		t.Fatalf("response headers mismatch: %#v", res.ResponseHeaders)
	}
	if res.Usage.InputDetails == nil || res.Usage.OutputDetails == nil || res.Usage.Raw == nil {
		t.Fatalf("usage details missing: %#v", res.Usage)
	}
}

func TestGroqLanguageModelDoGenerateError(t *testing.T) {
	p := newGroqProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	_, err := NewLanguageModel(p, "m").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err == nil {
		t.Fatal("expected provider error")
	}
}

func TestGroqDoStreamAndErr(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"reasoning":"r1"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":""}]}

data: {"x_groq":{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}},"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	p := newGroqProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"X-Request-Id": []string{"req2"}},
			Body:       io.NopCloser(strings.NewReader(sse)),
		}, nil
	})
	stream, err := NewLanguageModel(p, "m").DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck
	var finish *provider.StreamChunk
	for {
		ch, e := stream.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatalf("stream error = %v", e)
		}
		if ch.Type == provider.ChunkTypeFinish {
			finish = ch
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.Usage == nil {
		t.Fatalf("missing finish with usage: %#v", finish)
	}
}

type groqRoundTripper func(*http.Request) (*http.Response, error)

func (f groqRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newGroqProviderWithTransport(t *testing.T, rt groqRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://groq.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://groq.example",
		Headers: map[string]string{
			"Authorization": "Bearer k",
			"Content-Type":  "application/json",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}
