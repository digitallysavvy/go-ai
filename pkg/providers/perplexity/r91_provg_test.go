package perplexity

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// provg2-05: a missing API key is the LoadAPIKeyError itself (TS loadApiKey),
// not a wrapping ProviderError.
func TestR91provgMissingAPIKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("PERPLEXITY_API_KEY", "")
	em, _ := New(Config{}).EmbeddingModel("pplx-embed-v1-0.6b")
	_, err := em.DoEmbed(context.Background(), "hi", nil)
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("error = %T %v", err, err)
	}
}

// provg2-11: replayed tool-call arguments keep the input's key order (TS
// JSON.stringify(part.input ?? {}), convert-to-perplexity-input.ts:143).
func TestR91provgToolCallArgumentsKeyOrder(t *testing.T) {
	part := types.ToolCallContent{ToolCallID: "c", ToolName: "f", Input: `{"b":1,"a":{"z":1,"y":2}}`, Arguments: map[string]interface{}{"b": 1, "a": map[string]interface{}{"z": 1, "y": 2}}}
	if got := perplexityToolCallArguments(part); got != `{"b":1,"a":{"z":1,"y":2}}` {
		t.Fatalf("arguments = %s", got)
	}
	if got := perplexityToolCallArguments(types.ToolCallContent{Input: `null`}); got != `{}` {
		t.Fatalf("null input = %s", got)
	}
}

// Typed-option ruling probe: Go-typed option literals (embedding dimensions
// as a map[string]int, decision warnings from a map[string]string).
func TestR91provgTypedOptionLiterals(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":"AQI="}],"usage":{"prompt_tokens":1}}`))
	}))
	defer srv.Close()
	em, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("pplx-embed-v1-0.6b")
	if _, err := em.DoEmbedMany(context.Background(), []string{"hi"}, &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"perplexity": map[string]int{"dimensions": 256}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"dimensions":256`) {
		t.Fatalf("embedding body = %s", got)
	}
	if _, ok := perplexityThoughtSignature(map[string]interface{}{"perplexity": map[string]string{"thoughtSignature": "s"}}); !ok {
		t.Fatal("typed part thoughtSignature ignored")
	}
}
