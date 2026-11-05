package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r63: TS createGoogleGenerativeAI({generateId}) hands the generator to the
// chat and Interactions models (`generateId: options.generateId ??
// generateId`). The chat model's id-less function call takes its ID from
// it; an Interactions function_call keeps its (empty) id, as TS
// `toolCallId: call.id`, and an id-less built-in tool call takes a generated
// one (TS `call.id || generateId()`) (round 87e).
func TestR63ProviderGenerateIDReachesChatAndInteractions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/interactions" {
			_, _ = fmt.Fprint(w, `{"id":"v1","status":"requires_action","steps":[{"type":"function_call","name":"lookup","arguments":{}},{"type":"google_search_call","arguments":{}}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{}}}]},"finishReason":"STOP"}]}`)
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL, GenerateID: func() string { return "provider-id" }})
	for name, model := range map[string]provider.LanguageModel{
		"chat":         NewLanguageModel(p, ModelGemini25Flash),
		"interactions": NewInteractionsLanguageModel(p, ModelGemini25Flash),
	} {
		res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := "provider-id"
		if name == "interactions" {
			want = ""
		}
		if len(res.ToolCalls) == 0 || res.ToolCalls[0].ID != want {
			t.Fatalf("%s: tool calls = %+v, want the first with ID %q", name, res.ToolCalls, want)
		}
		if name == "interactions" {
			var builtin []string
			for _, c := range res.Content {
				if tc, ok := c.(types.ToolCallContent); ok && tc.ProviderExecuted {
					builtin = append(builtin, tc.ToolCallID)
				}
			}
			if len(builtin) != 1 || builtin[0] != "provider-id" {
				t.Fatalf("built-in tool call IDs = %v, want [provider-id]", builtin)
			}
		}
	}
}
