package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// TS OpenAICompatibleChatLanguageModel: `toolCallId: toolCall.id || generateId()`.
func TestCompatChatToolCallWithoutIDGetsGeneratedID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"type":"function","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	fw, _ := fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
	tg, _ := together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
	gm, _ := gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
	za, _ := zai.New(zai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
	for name, m := range map[string]provider.LanguageModel{"fireworks": fw, "together": tg, "gmicloud": gm, "zai": za} {
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID == "" {
			t.Fatalf("%s: tool calls = %#v", name, res.ToolCalls)
		}
	}
}
