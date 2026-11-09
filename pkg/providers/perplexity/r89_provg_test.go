package perplexity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS perplexity-prepare-tools.ts: description: tool.description, so a tool
// without a description sends no description key (Go "" = unset).
func TestR89provgPerplexityToolWithoutDescription(t *testing.T) {
	tools, _ := preparePerplexityTools([]types.Tool{
		{Type: types.ToolTypeFunction, Name: "a", Parameters: map[string]interface{}{"type": "object"}},
		{Type: types.ToolTypeFunction, Name: "b", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
	}, types.ToolChoice{})
	if _, ok := tools[0]["description"]; ok {
		t.Fatalf("tool without description sent %#v", tools[0])
	}
	if tools[1]["description"] != "d" {
		t.Fatalf("tool description = %#v", tools[1])
	}
}

// The exported PerplexityEmbeddingModelOptions given as the "perplexity"
// entry is the Go form of the TS options object: it is validated and sent
// (it used to fail with "invalid input: expected object").
func TestR89provgPerplexityTypedEmbeddingOptions(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","model":"pplx-embed-v1-0.6b","data":[{"object":"embedding","index":0,"embedding":"AQI="}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("pplx-embed-v1-0.6b")
	if err != nil {
		t.Fatal(err)
	}
	dims := 128
	typed := PerplexityEmbeddingModelOptions{Dimensions: &dims, EncodingFormat: "base64_binary"}
	for _, v := range []interface{}{typed, &typed} {
		body = nil
		if _, err := m.DoEmbed(context.Background(), "x", &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"perplexity": v}}); err != nil {
			t.Fatalf("typed options rejected: %v", err)
		}
		if body["dimensions"] != float64(128) || body["encoding_format"] != "base64_binary" {
			t.Fatalf("request body = %#v", body)
		}
	}
}

// A 2xx body that fails the TS response schema is the createJsonResponseHandler
// "Invalid JSON response" error, returned as is. Go used to set that error as
// its own cause, so err.Error() recursed until the stack overflowed (a fatal
// crash of the process).
func TestR89provgPerplexityInvalidResponseNoCauseCycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unexpected":true}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("pplx-embed-v1-0.6b")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoEmbedMany(context.Background(), []string{"x"}, nil)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" {
		t.Fatalf("error = %T", err)
	}
	seen := map[error]bool{}
	for e := error(pe); e != nil; e = errors.Unwrap(e) {
		if seen[e] {
			t.Fatal("error cause chain has a cycle")
		}
		seen[e] = true
	}
	_ = err.Error()
}
