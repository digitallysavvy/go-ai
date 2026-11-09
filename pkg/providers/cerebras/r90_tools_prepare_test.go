package cerebras

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS c9c6402cbd openai-compatible "prepareRequest should apply
// transformRequestBody", through Cerebras: PrepareRequest gives the body
// DoGenerate sends (max_tokens renamed, providerOptions.cerebras fields as
// snake_case) and makes no request.
func TestR90toolsCerebrasPrepareRequestMatchesWire(t *testing.T) {
	var sent map[string]interface{}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &sent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"llama3.1-8b","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("llama3.1-8b")
	if err != nil {
		t.Fatal(err)
	}
	maxTokens := 10
	opts := &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hello"},
		MaxTokens:       &maxTokens,
		ProviderOptions: map[string]interface{}{"cerebras": map[string]interface{}{"parallelToolCalls": false}},
	}
	body, _, err := m.(*LanguageModel).PrepareRequest(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("PrepareRequest made %d requests", calls)
	}
	if _, err := m.DoGenerate(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	var prepared map[string]interface{}
	raw, _ := json.Marshal(body)
	_ = json.Unmarshal(raw, &prepared)
	if !reflect.DeepEqual(prepared, sent) {
		t.Fatalf("prepared %v\nsent     %v", prepared, sent)
	}
	if _, ok := prepared["max_completion_tokens"]; !ok || prepared["parallel_tool_calls"] != false {
		t.Fatalf("transform not applied: %v", prepared)
	}
}
