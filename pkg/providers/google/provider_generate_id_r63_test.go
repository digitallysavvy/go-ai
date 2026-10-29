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
// generateId`), so an id-less function call takes its ID from it.
func TestR63ProviderGenerateIDReachesChatAndInteractions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/interactions" {
			_, _ = fmt.Fprint(w, `{"id":"v1","status":"requires_action","steps":[{"type":"function_call","name":"lookup","arguments":{}}]}`)
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
		if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "provider-id" {
			t.Fatalf("%s: tool calls = %+v, want one with ID provider-id", name, res.ToolCalls)
		}
	}
}
