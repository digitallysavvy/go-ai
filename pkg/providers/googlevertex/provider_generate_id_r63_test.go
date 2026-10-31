package googlevertex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r63: TS createVertex({generateId}) hands the generator to the chat and
// Interactions models (`generateId: options.generateId ?? generateId`).
func TestR63VertexGenerateIDReachesChatAndInteractions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/interactions") {
			_, _ = fmt.Fprint(w, `{"id":"v1","status":"requires_action","steps":[{"type":"function_call","name":"lookup","arguments":{}}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{}}}]},"finishReason":"STOP"}]}`)
	}))
	defer server.Close()

	p, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "tok", BaseURL: server.URL, GenerateID: func() string { return "provider-id" }})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := p.LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	interactions, err := p.Interactions("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	for name, model := range map[string]provider.LanguageModel{"chat": chat, "interactions": interactions} {
		res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "provider-id" {
			t.Fatalf("%s: tool calls = %+v, want one with ID provider-id", name, res.ToolCalls)
		}
	}
}
