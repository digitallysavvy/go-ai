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

// A non-streaming function_call keeps the id the API sent, an empty or
// missing one included, and never takes a generated ID: TS
// parse-google-interactions-outputs.ts sets `toolCallId: call.id`. A bun run
// of TS 3fab50aa9b parseGoogleInteractionsOutputs on these steps gives
// [undefined, "srv-1", ""] and never calls generateId (round 87e; the r62
// coordinator ruling generated IDs instead).
func TestR62InteractionsIDlessFunctionCallUsesGenerator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"id":"v1","status":"requires_action","steps":[
			{"type":"function_call","name":"lookup","arguments":{}},
			{"type":"function_call","id":"srv-1","name":"lookup","arguments":{}},
			{"type":"function_call","id":"","name":"lookup","arguments":{}}
		]}`)
	}))
	defer server.Close()

	n := 0
	cfg := interactionsConfigFromProvider(New(Config{APIKey: "k", BaseURL: server.URL}))
	cfg.GenerateID = func() string { n++; return fmt.Sprintf("gen-%d", n) }
	model := NewInteractionsLanguageModelWithConfig(cfg, ModelGemini25Flash)
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var calls, parts []string
	for _, c := range res.ToolCalls {
		calls = append(calls, fmt.Sprintf("%q", c.ID))
	}
	for _, c := range res.Content {
		if tc, ok := c.(types.ToolCallContent); ok {
			parts = append(parts, fmt.Sprintf("%q", tc.ToolCallID))
		}
	}
	if want := `["" "srv-1" ""]`; fmt.Sprint(calls) != want || fmt.Sprint(parts) != want {
		t.Fatalf("tool call IDs = %v, content IDs = %v, want %s", calls, parts, want)
	}
	if n != 0 {
		t.Fatalf("generator called %d times, want 0", n)
	}
}
