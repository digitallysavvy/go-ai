package cerebras

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS transformCerebrasRequestBody returns {...restArgs, ...extras}: the
// body keeps its key order and the providerOptions.cerebras fields follow
// it in the transform's order (parallel_tool_calls before logit_bias). The
// Go transport re-encoded the body from a map, sorting every key, so
// logit_bias went before messages (review round 97).
func TestR97toolsTransportKeepsBodyKeyOrder(t *testing.T) {
	var sent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sent = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("gpt-oss-120b")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{"cerebras": map[string]interface{}{
			"logitBias": map[string]interface{}{"50256": -100}, "parallelToolCalls": false,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	messages, model_ := strings.Index(sent, `"messages"`), strings.Index(sent, `"model"`)
	parallel, bias := strings.Index(sent, `"parallel_tool_calls":false`), strings.Index(sent, `"logit_bias":{"50256":-100}`)
	if messages < 0 || model_ < 0 || parallel < messages || parallel < model_ || bias < parallel {
		t.Fatalf("body key order = %s", sent)
	}
}
