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
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestMistralEmbeddingModelMetadataAndLimits(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewEmbeddingModel(p, "mistral-embed")
	if m.SpecificationVersion() != "v3" || m.Provider() != "mistral" || m.ModelID() != "mistral-embed" {
		t.Fatalf("metadata mismatch: spec=%s provider=%s model=%s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}
	if m.MaxEmbeddingsPerCall() != 2048 || !m.SupportsParallelCalls() {
		t.Fatalf("limits mismatch: max=%d parallel=%v", m.MaxEmbeddingsPerCall(), m.SupportsParallelCalls())
	}
}

func TestMistralEmbeddingModelDoEmbedAndDoEmbedMany(t *testing.T) {
	var seenPath string
	var seenAuth string
	var seenHeader string
	var seenBody map[string]interface{}

	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		seenHeader = r.Header.Get("X-Test")
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		if _, ok := seenBody["input"].([]interface{}); ok {
			return mistralJSONResponse(`{"object":"list","data":[{"index":0,"embedding":[1,2]},{"index":1,"embedding":[3,4]}],"usage":{"prompt_tokens":7,"total_tokens":9}}`), nil
		}
		return mistralJSONResponse(`{"object":"list","data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":4}}`), nil
	})
	m := NewEmbeddingModel(p, "mistral-embed")

	one, err := m.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{Headers: map[string]string{"X-Test": "yes"}})
	if err != nil {
		t.Fatalf("DoEmbed error = %v", err)
	}
	if seenPath != "/v1/embeddings" || !strings.HasPrefix(seenAuth, "Bearer k") || seenHeader != "yes" {
		t.Fatalf("request mismatch path=%q auth=%q header=%q", seenPath, seenAuth, seenHeader)
	}
	inputs, ok := seenBody["input"].([]interface{})
	if seenBody["model"] != "mistral-embed" || !ok || len(inputs) != 1 || inputs[0] != "hello" {
		t.Fatalf("request body mismatch: %#v", seenBody)
	}
	if len(one.Embedding) != 2 || one.Usage.InputTokens != 7 || one.Response.Headers["X-Request-Id"] != "req_1" {
		t.Fatalf("DoEmbed result mismatch: %#v", one)
	}

	many, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany error = %v", err)
	}
	if len(many.Embeddings) != 2 || many.Usage.InputTokens != 7 || many.Responses[0].Headers["X-Request-Id"] != "req_1" {
		t.Fatalf("DoEmbedMany result mismatch: %#v", many)
	}
}

func TestMistralEmbeddingModelErrorAndOptsHeaders(t *testing.T) {
	p := newMistralProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	m := NewEmbeddingModel(p, "mistral-embed")
	_, err := m.DoEmbedMany(context.Background(), []string{"a"}, nil)
	if err == nil || !providererrors.IsProviderError(err) {
		t.Fatalf("expected provider error, got %v", err)
	}
	if h := optsHeaders(nil); h != nil {
		t.Fatalf("optsHeaders(nil) = %#v, want nil", h)
	}
}

type mistralRoundTripper func(*http.Request) (*http.Response, error)

func (f mistralRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMistralProviderWithTransport(t *testing.T, rt mistralRoundTripper) *Provider {
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

func mistralJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"req_1"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}
