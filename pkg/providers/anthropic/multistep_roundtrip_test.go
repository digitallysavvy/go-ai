package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// TestMultiStepToolUseRoundTrip drives generate → tool call → tool result →
// second request through GenerateText against an httptest server and checks
// that the second request body has the shape produced by the TS
// convertToAnthropicPrompt: the assistant turn carries a tool_use block and
// the tool result is sent in a "user" message as a tool_result block (never
// with role "tool").
func TestMultiStepToolUseRoundTrip(t *testing.T) {
	responses := []string{
		`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5",
		  "content":[
		    {"type":"text","text":"Let me check."},
		    {"type":"tool_use","id":"toolu_01","name":"weather","input":{"city":"SF"}}
		  ],
		  "stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`,
		`{"id":"msg_2","type":"message","role":"assistant","model":"claude-sonnet-4-5",
		  "content":[{"type":"text","text":"It is 72 degrees in SF."}],
		  "stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":8}}`,
	}

	var mu sync.Mutex
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		idx := len(bodies)
		bodies = append(bodies, body)
		mu.Unlock()
		if idx >= len(responses) {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responses[idx])
	}))
	defer srv.Close()

	p := anthropic.New(anthropic.Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		System: "You are helpful.",
		Prompt: "What is the weather in SF?",
		Tools: []types.Tool{{
			Name:        "weather",
			Description: "Get the weather",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
			},
			Execute: func(ctx context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"temperature": 72}, nil
			},
		}},
		StopWhen: []ai.StopCondition{ai.IsStepCount(2)},
	})
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if result.Text != "It is 72 degrees in SF." {
		t.Errorf("final text = %q", result.Text)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}

	second := bodies[1]
	wantSystem := `[{"type":"text","text":"You are helpful."}]`
	wantMessages := `[
		{"role":"user","content":[{"type":"text","text":"What is the weather in SF?"}]},
		{"role":"assistant","content":[
			{"type":"text","text":"Let me check."},
			{"type":"tool_use","id":"toolu_01","name":"weather","input":{"city":"SF"}}
		]},
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"toolu_01","content":"{\"temperature\":72}"}
		]}
	]`
	assertJSON(t, "system", second["system"], wantSystem)
	assertJSON(t, "messages", second["messages"], wantMessages)
}

func assertJSON(t *testing.T, label string, got interface{}, want string) {
	t.Helper()
	var wantV interface{}
	if err := json.Unmarshal([]byte(want), &wantV); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wantV) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(wantV, "", "  ")
		t.Errorf("%s mismatch\n got: %s\nwant: %s", label, gotJSON, wantJSON)
	}
}
