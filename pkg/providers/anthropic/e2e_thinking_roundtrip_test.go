package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// A turn with thinking, redacted thinking, text and a tool call must be
// replayed to the API in the order the model produced it (the API rejects an
// assistant turn whose thinking blocks do not come first), on both the
// generate and the stream path. Redacted thinking data and signatures must
// survive the round trip.
const thinkingTurnWantAssistant = `[
	{"type":"thinking","thinking":"First thoughts.","signature":"SIG_A"},
	{"type":"redacted_thinking","data":"REDACTED_B"},
	{"type":"text","text":"Let me check."},
	{"type":"tool_use","id":"toolu_01","name":"weather","input":{"city":"SF"}}
]`

func thinkingTurnTool() types.Tool {
	return types.Tool{
		Name:        "weather",
		Description: "Get the weather",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"temperature": 72}, nil
		},
	}
}

func secondRequestAssistantContent(t *testing.T, bodies []map[string]interface{}) interface{} {
	t.Helper()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	messages, _ := bodies[1]["messages"].([]interface{})
	if len(messages) < 2 {
		t.Fatalf("messages = %#v", bodies[1]["messages"])
	}
	m, _ := messages[1].(map[string]interface{})
	if m["role"] != "assistant" {
		t.Fatalf("messages[1] = %#v, want assistant", messages[1])
	}
	return m["content"]
}

func TestThinkingTurnReplayOrderGenerate(t *testing.T) {
	responses := []string{
		`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[
			{"type":"thinking","thinking":"First thoughts.","signature":"SIG_A"},
			{"type":"redacted_thinking","data":"REDACTED_B"},
			{"type":"text","text":"Let me check."},
			{"type":"tool_use","id":"toolu_01","name":"weather","input":{"city":"SF"}}
		],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`,
		`{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"72."}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":8}}`,
	}
	var mu sync.Mutex
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		idx := len(bodies)
		bodies = append(bodies, body)
		mu.Unlock()
		if idx >= len(responses) {
			http.Error(w, "unexpected", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responses[idx])
	}))
	defer srv.Close()

	model, err := anthropic.New(anthropic.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-opus-5-5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: model, Prompt: "weather?", Tools: []types.Tool{thinkingTurnTool()},
		StopWhen: []ai.StopCondition{ai.IsStepCount(2)},
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	assertJSON(t, "assistant content", secondRequestAssistantContent(t, bodies), thinkingTurnWantAssistant)
}
