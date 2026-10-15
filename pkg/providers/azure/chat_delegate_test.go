package azure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// azure.chat() is an OpenAIChatLanguageModel in TS: provider options, usage
// request, api-version query and response parsing all come from it.
func TestChatModelDelegatesToOpenAIChat(t *testing.T) {
	var path, query, apiKey string
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query, apiKey = r.URL.Path, r.URL.RawQuery, r.Header.Get("api-key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","model":"gpt","created":1,"choices":[{"message":{"role":"assistant","content":"ok","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"completion_tokens_details":{"accepted_prediction_tokens":1}}}`))
	}))
	defer srv.Close()

	p := mustNewProvider(t, Config{APIKey: "azure-key", BaseURL: srv.URL + "/openai", APIVersion: "2025-01-01-preview", UseDeploymentBasedURLs: true})
	m, err := p.ChatModel("dep-1")
	if err != nil {
		t.Fatal(err)
	}
	if m.Provider() != "azure.chat" || m.ModelID() != "dep-1" {
		t.Fatalf("provider=%q model=%q", m.Provider(), m.ModelID())
	}
	max := 50
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{System: "sys", Text: "hello"},
		MaxTokens: &max,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"user": "u-1", "parallelToolCalls": false,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if apiKey != "azure-key" || path != "/openai/deployments/dep-1/chat/completions" {
		t.Fatalf("path=%q key=%q", path, apiKey)
	}
	if query != "api-version=2025-01-01-preview" {
		t.Fatalf("query=%q", query)
	}
	if body["user"] != "u-1" || body["parallel_tool_calls"] != false {
		t.Fatalf("provider options not forwarded: %v", body)
	}
	msgs, _ := body["messages"].([]interface{})
	if len(msgs) == 0 || msgs[0].(map[string]interface{})["role"] != "system" {
		t.Fatalf("expected system message first: %v", body["messages"])
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "lookup" || res.ToolCalls[0].Arguments["city"] != "Paris" {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 3 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	meta, _ := res.ProviderMetadata["openai"].(map[string]interface{})
	if meta["acceptedPredictionTokens"] != 1 {
		t.Fatalf("metadata = %v", res.ProviderMetadata)
	}
}

func TestChatModelStreamRequestsUsage(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	p := mustNewProvider(t, Config{APIKey: "k", BaseURL: srv.URL + "/openai/v1"})
	m, _ := p.ChatModel("dep-1")
	st, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	var usage *types.Usage
	for {
		c, err := st.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			usage = c.Usage
		}
	}
	if so, _ := body["stream_options"].(map[string]interface{}); so["include_usage"] != true {
		t.Fatalf("stream_options = %v", body["stream_options"])
	}
	if usage == nil || *usage.InputTokens != 4 {
		t.Fatalf("finish usage = %+v", usage)
	}
}

func TestChatModelSystemOnlyPromptDoesNotPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p := mustNewProvider(t, Config{APIKey: "k", BaseURL: srv.URL + "/openai/v1"})
	m, _ := p.ChatModel("d")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{System: "s"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{}}); err != nil {
		t.Fatal(err)
	}
}
