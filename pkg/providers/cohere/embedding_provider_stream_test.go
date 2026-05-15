package cohere

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

func TestCohereProviderSurfaceAndEmbeddingFactory(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if CreateCohere(Config{APIKey: "k"}).Name() != "cohere" || p.Name() != "cohere" {
		t.Fatalf("provider name mismatch")
	}
	if p.Client() == nil {
		t.Fatal("expected client")
	}
	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("expected empty model error")
	}
	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("expected empty embedding model error")
	}
	if _, err := p.EmbeddingModelWithOptions("", DefaultEmbeddingOptions()); err == nil {
		t.Fatal("expected empty embedding model error")
	}
	rm, err := p.RerankingModel("rerank-v3.5")
	if err != nil || rm.Provider() != "cohere" {
		t.Fatalf("reranking model error=%v model=%#v", err, rm)
	}
}

func TestCohereEmbeddingModelDoEmbedMany(t *testing.T) {
	var seenPath string
	var seenHeader string
	var seenBody map[string]interface{}
	p := newCohereProviderWithTransportForEmbed(t, func(r *http.Request) (*http.Response, error) {
		seenPath = r.URL.Path
		seenHeader = r.Header.Get("X-Test")
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"X-Request-Id": []string{"req1"}},
			Body:       io.NopCloser(strings.NewReader(`{"embeddings":[[1,2],[3,4]],"meta":{"billed_units":{"input_tokens":7}}}`)),
		}, nil
	})
	d := Dimension1024
	model := NewEmbeddingModel(p, "embed-v4", EmbeddingOptions{OutputDimension: &d, InputType: InputTypeSearchDocument})
	if model.SpecificationVersion() != "v3" || model.Provider() != "cohere" || model.ModelID() != "embed-v4" {
		t.Fatalf("metadata mismatch")
	}
	if model.MaxEmbeddingsPerCall() != 96 || !model.SupportsParallelCalls() {
		t.Fatalf("limits mismatch")
	}
	out, err := model.DoEmbedMany(context.Background(), []string{"a", "b"}, &provider.EmbedModelOptions{Headers: map[string]string{"X-Test": "yes"}})
	if err != nil {
		t.Fatalf("DoEmbedMany error = %v", err)
	}
	if seenPath != "/v1/embed" || seenHeader != "yes" {
		t.Fatalf("request mismatch path=%q header=%q", seenPath, seenHeader)
	}
	if seenBody["model"] != "embed-v4" {
		t.Fatalf("request body mismatch: %#v", seenBody)
	}
	if len(out.Embeddings) != 2 || out.Usage.InputTokens != 7 || out.Responses[0].Headers["X-Request-Id"][0] != "req1" {
		t.Fatalf("result mismatch: %#v", out)
	}
}

func TestCohereEmbeddingModelErrorAndHelpers(t *testing.T) {
	p := newCohereProviderWithTransportForEmbed(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	model := NewEmbeddingModel(p, "embed-v4")
	if _, err := model.DoEmbed(context.Background(), "x", nil); err == nil {
		t.Fatal("expected provider error")
	}
	if h := optsHeaders(nil); h != nil {
		t.Fatalf("optsHeaders(nil) = %#v", h)
	}
}

func TestCohereDoStreamAndStreamHelpers(t *testing.T) {
	sse := `data: {"type":"content-start","index":0,"delta":{"message":{"content":{"type":"thinking"}}}}

data: {"type":"content-delta","index":0,"delta":{"message":{"content":{"thinking":"hmm"}}}}

data: {"type":"content-end","index":0,"delta":{}}

data: {"type":"content-start","index":1,"delta":{"message":{"content":{"type":"text"}}}}

data: {"type":"content-delta","index":1,"delta":{"message":{"content":{"text":"hello"}}}}

data: {"type":"tool-call-start","index":0,"delta":{"message":{"tool_calls":{"id":"tc1","function":{"name":"lookup","arguments":"{\"q\":"}}}}}

data: {"type":"tool-call-delta","index":0,"delta":{"message":{"tool_calls":{"function":{"arguments":"\"x\"}"}}}}

data: {"type":"tool-call-end","index":0,"delta":{}}

data: {"type":"message-end","delta":{"finish_reason":"TOOL_CALL","usage":{"tokens":{"input_tokens":3,"output_tokens":5}}}}

data: [DONE]

`
	p := newCohereProviderWithTransportForEmbed(t, func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(sse)),
		}, nil
	})
	lm := NewLanguageModel(p, "command-r-plus")
	if lm.SpecificationVersion() != "v3" || lm.Provider() != "cohere" || lm.ModelID() != "command-r-plus" {
		t.Fatalf("metadata mismatch")
	}
	if !lm.SupportsTools() || lm.SupportsStructuredOutput() || !lm.SupportsImageInput() {
		t.Fatalf("capability mismatch")
	}
	stream, err := lm.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoStream error = %v", err)
	}
	defer stream.Close() //nolint:errcheck
	gotTool := false
	gotFinish := false
	for {
		ch, e := stream.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatalf("stream error = %v", e)
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

type cohereEmbedRoundTripper func(*http.Request) (*http.Response, error)

func (f cohereEmbedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newCohereProviderWithTransportForEmbed(t *testing.T, rt cohereEmbedRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://cohere.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://cohere.example",
		Headers: map[string]string{
			"Authorization": "Bearer k",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}
