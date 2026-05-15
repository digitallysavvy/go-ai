package deepseek

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

func TestDeepseekProviderSurface(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if CreateDeepSeek(Config{APIKey: "k"}).Name() != "deepseek" || p.Name() != "deepseek" {
		t.Fatalf("provider name mismatch")
	}
	if p.Client() == nil {
		t.Fatal("expected client")
	}
	lm, err := p.LanguageModel("")
	if err != nil || lm.ModelID() != "deepseek-chat" {
		t.Fatalf("language model mismatch model=%#v err=%v", lm, err)
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

func TestDeepseekDoGenerateAndStream(t *testing.T) {
	var seenBody map[string]interface{}
	p := newDeepseekProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		if r.Header.Get("Accept") == "text/event-stream" {
			sse := `data: {"choices":[{"delta":{"reasoning_content":"think"},"finish_reason":""}]}

data: {"choices":[{"delta":{"content":"hello"},"finish_reason":""}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(sse))}, nil
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"X-Request-Id": []string{"req1"}},
			Body: io.NopCloser(strings.NewReader(`{
				"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok","reasoning_content":"r","tool_calls":[{"id":"tc1","type":"function","function":{"name":"lookup","arguments":"{\"x\":1}"}}]}}],
				"usage":{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":3}}
			}`)),
		}, nil
	})

	m := NewLanguageModel(p, "deepseek-v4")
	if m.SpecificationVersion() != "v3" || m.Provider() != "deepseek" || m.ModelID() != "deepseek-v4" {
		t.Fatalf("metadata mismatch")
	}
	if !m.SupportsTools() || !m.SupportsStructuredOutput() || m.SupportsImageInput() {
		t.Fatalf("capability mismatch")
	}

	reason := types.ReasoningLow
	out, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &reason})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if out.Text != "ok" || out.FinishReason != types.FinishReasonStop || len(out.ToolCalls) != 1 || len(out.Content) != 1 {
		t.Fatalf("generate result mismatch: %#v", out)
	}
	if seenBody["thinking"] == nil {
		t.Fatalf("expected thinking field in body: %#v", seenBody)
	}

	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck
	gotFinish := false
	gotTool := false
	for {
		ch, e := stream.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatalf("stream err = %v", e)
		}
		if ch.Type == provider.ChunkTypeToolCall {
			gotTool = true
		}
		if ch.Type == provider.ChunkTypeFinish && ch.FinishReason == types.FinishReasonToolCalls {
			gotFinish = true
		}
	}
	if !gotTool || !gotFinish {
		t.Fatalf("expected tool and finish chunks")
	}
}

func TestDeepseekHelpersAndErrorPath(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-v4")
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "plain"},
				types.ReasoningContent{Text: "reason"},
			},
		},
	}
	converted := m.toDeepSeekMessages(msgs)
	if converted[0]["reasoning_content"] != "reason" {
		t.Fatalf("reasoning_content mapping mismatch: %#v", converted[0])
	}

	p := newDeepseekProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	if _, err := NewLanguageModel(p, "m").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}}); err == nil {
		t.Fatal("expected provider error")
	}
}

type deepseekRoundTripper func(*http.Request) (*http.Response, error)

func (f deepseekRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newDeepseekProviderWithTransport(t *testing.T, rt deepseekRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://deepseek.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://deepseek.example",
		Headers: map[string]string{
			"Authorization": "Bearer k",
			"Content-Type":  "application/json",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}
