package cerebras

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS "maps Cerebras provider options to request fields": request.body is the
// transformed body, so the reported request (RawRequest, and the stream's
// RequestBody) carries the per-call fields sent on the wire.
func TestR81integCerebrasReportedRequestHasExtras(t *testing.T) {
	var captured map[string]interface{}
	stream := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = nil
		_ = json.NewDecoder(r.Body).Decode(&captured)
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("gpt-oss-120b")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{"cerebras": map[string]interface{}{
			"parallelToolCalls": false, "topLogprobs": 2, "logitBias": map[string]interface{}{"42": 1},
			"serviceTier": "priority", "reasoningFormat": "parsed", "promptCacheKey": "cache-key",
		}},
	}
	asJSON := func(v interface{}) map[string]interface{} {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	result, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := asJSON(result.RawRequest); !reflect.DeepEqual(got, captured) {
		t.Fatalf("RawRequest = %v, want the wire body %v", got, captured)
	}
	if captured["prompt_cache_key"] != "cache-key" || captured["parallel_tool_calls"] != false {
		t.Fatalf("wire body lacks the extras: %v", captured)
	}

	stream = true
	st, err := model.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	rb, ok := st.(provider.StreamRequestBody)
	if !ok {
		t.Fatalf("stream %T has no RequestBody", st)
	}
	if got := asJSON(rb.RequestBody()); !reflect.DeepEqual(got, captured) {
		t.Fatalf("stream RequestBody = %v, want the wire body %v", got, captured)
	}
}
