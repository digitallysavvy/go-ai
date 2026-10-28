package bedrock

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r56: TS `'redactedContent' in part.reasoningContent` keeps an empty
// redactedContent as a reasoning part (generate).
func TestR56BedrockGenerateEmptyRedactedContentIsReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"reasoningContent":{"redactedContent":""}},{"text":"hi"}]}},"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2,"totalTokens":3}}`))
	}))
	defer server.Close()
	m := newHTTPTestBedrockModel(t, server, "us.openai.gpt-5.6-luna")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 2 {
		t.Fatalf("content = %#v, want reasoning then text", res.Content)
	}
	r, ok := res.Content[0].(types.ReasoningContent)
	if !ok {
		t.Fatalf("content[0] = %#v, want reasoning", res.Content[0])
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(r.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if v, ok := meta["amazonBedrock"]["redactedContent"]; !ok || v != "" {
		t.Fatalf("metadata = %s, want amazonBedrock.redactedContent \"\"", r.ProviderMetadata)
	}
}

// r56: the TS schema requires toolUse.name to be a string, and
// `name ?? tool-${id}` keeps an empty one.
func TestR56BedrockGenerateEmptyToolNameIsKept(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"toolUse":{"toolUseId":"t1","name":"","input":{}}}]}},"stopReason":"tool_use","usage":{"inputTokens":1,"outputTokens":2,"totalTokens":3}}`))
	}))
	defer server.Close()
	m := newHTTPTestBedrockModel(t, server, "amazon.nova-pro-v1:0")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "t1" || res.ToolCalls[0].ToolName != "" {
		t.Fatalf("tool calls = %#v, want t1 with an empty name", res.ToolCalls)
	}
}
