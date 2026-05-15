package mistral

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

func TestMistralProviderSurface(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if CreateMistral(Config{APIKey: "k"}).Name() != "mistral" || p.Name() != "mistral" {
		t.Fatalf("provider name mismatch")
	}
	if p.Client() == nil {
		t.Fatal("expected client")
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
	em, err := p.EmbeddingModel("")
	if err != nil || em.ModelID() != "mistral-embed" {
		t.Fatalf("embedding model mismatch model=%#v err=%v", em, err)
	}
}

func TestMistralDoGenerateAndDoStream(t *testing.T) {
	var seenPath string
	var seenBody map[string]interface{}
	p := newMistralProviderWithTransportForLM(t, func(r *http.Request) (*http.Response, error) {
		seenPath = r.URL.Path
		if strings.Contains(seenPath, "/chat/completions") {
			_ = json.NewDecoder(r.Body).Decode(&seenBody)
			if r.Header.Get("Accept") == "text/event-stream" {
				sse := `data: {"choices":[{"delta":{"content":"hello"},"finish_reason":""}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(sse))}, nil
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"X-Request-Id": []string{"req1"}},
				Body: io.NopCloser(strings.NewReader(`{
					"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"ok","tool_calls":[{"id":"tc1","type":"function","function":{"name":"lookup","arguments":"{\"x\":1}"}}]}}],
					"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}
				}`)),
			}, nil
		}
		return nil, errors.New("unexpected path")
	})
	lm := NewLanguageModel(p, ModelMistralSmallLatest)
	if lm.SpecificationVersion() != "v3" || lm.Provider() != "mistral" || lm.ModelID() != ModelMistralSmallLatest {
		t.Fatalf("metadata mismatch")
	}
	if !lm.SupportsTools() || !lm.SupportsStructuredOutput() || lm.SupportsImageInput() {
		t.Fatalf("capabilities mismatch")
	}

	out, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenPath != "/v1/chat/completions" || seenBody["model"] != ModelMistralSmallLatest {
		t.Fatalf("request mismatch path=%q body=%#v", seenPath, seenBody)
	}
	if out.Text != "ok" || out.FinishReason != types.FinishReasonToolCalls || len(out.ToolCalls) != 1 {
		t.Fatalf("result mismatch: %#v", out)
	}

	stream, err := lm.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck
	gotText := false
	gotFinish := false
	for {
		ch, e := stream.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatalf("stream err = %v", e)
		}
		if ch.Type == provider.ChunkTypeText && ch.Text == "hello" {
			gotText = true
		}
		if ch.Type == provider.ChunkTypeFinish {
			gotFinish = true
		}
	}
	if !gotText || !gotFinish {
		t.Fatalf("expected text and finish chunks")
	}
}

func TestMistralLanguageHelpers(t *testing.T) {
	if mapMistralFinishReason("model_length") != types.FinishReasonLength {
		t.Fatalf("finish reason mapping mismatch")
	}
	lm := NewLanguageModel(New(Config{APIKey: "k"}), "mistral-large")
	if ws := lm.checkReasoningWarnings(&provider.GenerateOptions{Reasoning: func() *types.ReasoningLevel { v := types.ReasoningMedium; return &v }()}); len(ws) == 0 {
		t.Fatal("expected reasoning warning on unsupported model")
	}
	p := newMistralProviderWithTransportForLM(t, func(_ *http.Request) (*http.Response, error) { return nil, errors.New("boom") })
	if _, err := NewLanguageModel(p, "m").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}}); err == nil {
		t.Fatal("expected provider error")
	}
}

type mistralLMRoundTripper func(*http.Request) (*http.Response, error)

func (f mistralLMRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMistralProviderWithTransportForLM(t *testing.T, rt mistralLMRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://mistral.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://mistral.example",
		Headers: map[string]string{
			"Authorization": "Bearer k",
			"Content-Type":  "application/json",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}
