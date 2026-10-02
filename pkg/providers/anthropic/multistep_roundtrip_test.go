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

// TestMultiStepStreamingThinkingSignatureRoundTrip covers a coordinator
// follow-up to TS commit a587f554f7 (#21736): Go's streaming
// "signature_delta" handler used to discard the thinking block's signature
// with a bare continue, so a replayed thinking block in the next step's
// request had no signature at all -- which Anthropic rejects outright for
// extended-thinking models. Drives StreamText through a first step that
// streams a signed thinking block plus a tool call, then a second step, and
// asserts the second request's assistant message replays the thinking
// block WITH its signature.
func TestMultiStepStreamingThinkingSignatureRoundTrip(t *testing.T) {
	events := [][]struct {
		event string
		data  string
	}{
		{
			{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-opus-5-5","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me check."}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG_1"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"weather","input":{}}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"SF\"}"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":1}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`},
			{"message_stop", `{"type":"message_stop"}`},
		},
		{
			{"message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"claude-opus-5-5","stop_reason":null,"usage":{"input_tokens":20,"output_tokens":0}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"It is 72 degrees in SF."}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`},
			{"message_stop", `{"type":"message_stop"}`},
		},
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
		if idx >= len(events) {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events[idx] {
			_, _ = io.WriteString(w, "event: "+e.event+"\ndata: "+e.data+"\n\n")
		}
	}))
	defer srv.Close()

	p := anthropic.New(anthropic.Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel("claude-opus-5-5")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
		Model:  model,
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
		StopWhen: []ai.StopCondition{ai.StepCountIs(2)},
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	if err := result.ConsumeStream(); err != nil {
		t.Fatalf("ConsumeStream: %v", err)
	}
	if got := result.Text(); got != "It is 72 degrees in SF." {
		t.Errorf("final text = %q", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}

	second := bodies[1]
	messages, _ := second["messages"].([]interface{})
	if len(messages) < 2 {
		t.Fatalf("messages = %#v, want at least 2", second["messages"])
	}
	assistantMsg, ok := messages[1].(map[string]interface{})
	if !ok || assistantMsg["role"] != "assistant" {
		t.Fatalf("messages[1] = %#v, want the assistant turn", messages[1])
	}
	content, _ := assistantMsg["content"].([]interface{})
	var thinking map[string]interface{}
	for _, part := range content {
		if p, ok := part.(map[string]interface{}); ok && p["type"] == "thinking" {
			thinking = p
			break
		}
	}
	if thinking == nil {
		t.Fatalf("assistant content = %#v, want a thinking block", content)
	}
	if thinking["signature"] != "SIG_1" {
		t.Fatalf("thinking block = %#v, want signature %q", thinking, "SIG_1")
	}
	if thinking["thinking"] != "Let me check." {
		t.Fatalf("thinking block = %#v, want thinking text %q", thinking, "Let me check.")
	}
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
