package google

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func runInteractionsFrames(t *testing.T, frames ...string) []*provider.StreamChunk {
	t.Helper()
	var b strings.Builder
	for _, f := range frames {
		b.WriteString("data: " + f + "\n\n")
	}
	s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(b.String())), "", nil, nil, nil, 0)
	chunks := drainInteractions(t, s)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError && !strings.Contains(c.Text, "boom") {
			t.Fatalf("unexpected error part: %s", c.Text)
		}
	}
	return chunks
}

const looseCompleted = `{"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed"}}`

// r56: TS falls back to the loose unknownEvent member when a known
// event_type fails its member schema and still dispatches by event_type.
func TestR56InteractionsLooseFallbackDispatchesByEventType(t *testing.T) {
	t.Run("interaction.created", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"interaction.created","interaction":{"id":"v1","model":"gemini-x","status":7}}`,
			looseCompleted)
		var meta *provider.ResponseMetadata
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeResponseMetadata {
				meta = c.ResponseMetadata
			}
		}
		if meta == nil || meta.ID != "v1" || meta.ModelID != "gemini-x" {
			t.Fatalf("response metadata = %#v", meta)
		}
	})

	t.Run("step.start thought summary", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"step.start","index":0,"step":{"type":"thought","summary":[{"type":"text","text":5},{"type":"text","text":"ok"}]}}`,
			`{"event_type":"step.stop","index":0}`,
			looseCompleted)
		var reasoning []string
		var started, ended bool
		for _, c := range chunks {
			switch c.Type {
			case provider.ChunkTypeReasoningStart:
				started = true
			case provider.ChunkTypeReasoning:
				reasoning = append(reasoning, c.Reasoning)
			case provider.ChunkTypeReasoningEnd:
				ended = true
			}
		}
		if !started || !ended || len(reasoning) != 1 || reasoning[0] != "ok" {
			t.Fatalf("started=%v ended=%v reasoning=%q", started, ended, reasoning)
		}
	})

	t.Run("step.delta", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
			`{"event_type":"step.delta","index":0,"event_id":9,"delta":{"type":"text","text":"hi","annotations":"bad"}}`,
			`{"event_type":"step.stop","index":0}`,
			looseCompleted)
		var text string
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeText {
				text += c.Text
			}
		}
		if text != "hi" {
			t.Fatalf("text = %q, want hi", text)
		}
	})

	t.Run("step.stop", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"step.start","index":0,"step":{"type":"function_call","id":"c1","name":"f"}}`,
			`{"event_type":"step.stop","index":0,"event_id":9}`,
			looseCompleted)
		var call *types.ToolCall
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeToolCall {
				call = c.ToolCall
			}
		}
		if call == nil || call.ID != "c1" || call.ToolName != "f" {
			t.Fatalf("tool call = %#v", call)
		}
	})

	for _, et := range []string{"interaction.status_update", "interaction.in_progress", "interaction.requires_action"} {
		t.Run(et, func(t *testing.T) {
			// No completion event: the status the loose event set decides
			// the finish reason.
			chunks := runInteractionsFrames(t,
				`{"event_type":"`+et+`","interaction_id":5,"status":"incomplete"}`)
			var finish *provider.StreamChunk
			for _, c := range chunks {
				if c.Type == provider.ChunkTypeFinish {
					finish = c
				}
			}
			if finish == nil || finish.RawFinishReason != "incomplete" {
				t.Fatalf("finish = %#v, want raw finish reason incomplete", finish)
			}
		})
	}

	t.Run("interaction.completed", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed","usage":{"total_input_tokens":3,"total_output_tokens":"x"},"service_tier":1}}`)
		var finish *provider.StreamChunk
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeFinish {
				finish = c
			}
		}
		if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.RawFinishReason != "completed" {
			t.Fatalf("finish = %#v, want stop/completed", finish)
		}
		if finish.Usage == nil || finish.Usage.InputTokens == nil || *finish.Usage.InputTokens != 3 {
			t.Fatalf("usage = %#v, want 3 input tokens", finish.Usage)
		}
	})

	t.Run("error", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"error","event_id":9,"error":{"code":"c","message":"boom"}}`)
		var sawBoom bool
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeError && c.Text == "boom" {
				sawBoom = true
			}
		}
		if !sawBoom {
			t.Fatal("no boom error part")
		}
	})
}

// r56: TS `name ?? fallback` keeps an empty step name: a function_call step
// named "" is not "unknown", and an MCP step or delta named "" is not
// "mcp_server_tool".
func TestR56InteractionsEmptyStepNameIsKept(t *testing.T) {
	t.Run("stream function_call", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"step.start","index":0,"step":{"type":"function_call","id":"c1","name":""}}`,
			`{"event_type":"step.stop","index":0}`,
			looseCompleted)
		var start, call *types.ToolCall
		for _, c := range chunks {
			switch c.Type {
			case provider.ChunkTypeToolInputStart:
				start = c.ToolCall
			case provider.ChunkTypeToolCall:
				call = c.ToolCall
			}
		}
		if start == nil || start.ToolName != "" || call == nil || call.ToolName != "" {
			t.Fatalf("start = %#v call = %#v, want empty names", start, call)
		}
	})

	t.Run("stream function_call without name", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"step.start","index":0,"step":{"type":"function_call","id":"c1"}}`,
			`{"event_type":"step.stop","index":0}`,
			looseCompleted)
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeToolCall && c.ToolCall.ToolName != "unknown" {
				t.Fatalf("tool name = %q, want unknown", c.ToolCall.ToolName)
			}
		}
	})

	t.Run("stream mcp delta", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"step.start","index":0,"step":{"type":"mcp_server_tool_call","id":"m1","name":"srv"}}`,
			`{"event_type":"step.delta","index":0,"delta":{"type":"mcp_server_tool_call","name":""}}`,
			`{"event_type":"step.stop","index":0}`,
			looseCompleted)
		var call *types.ToolCall
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeToolCall {
				call = c.ToolCall
			}
		}
		if call == nil || call.ToolName != "" {
			t.Fatalf("tool call = %#v, want empty name from the delta", call)
		}
	})

	t.Run("generate mcp", func(t *testing.T) {
		var blocks []interactionsContentBlock
		if err := json.Unmarshal([]byte(`[{"type":"mcp_server_tool_call","id":"m1","name":"","arguments":{}},{"type":"mcp_server_tool_call","id":"m2","arguments":{}}]`), &blocks); err != nil {
			t.Fatal(err)
		}
		m := &InteractionsLanguageModel{}
		_, calls, _ := m.parseOutputs(blocks, "")
		if len(calls) != 2 || calls[0].ToolName != "" || calls[1].ToolName != "mcp_server_tool" {
			t.Fatalf("calls = %#v, want \"\" then mcp_server_tool", calls)
		}
	})
}

// r56: TS reads the service tier from interaction.completed (or the
// response header) only; a service_tier on interaction.created is ignored.
func TestR56InteractionsCreatedServiceTierIgnored(t *testing.T) {
	chunks := runInteractionsFrames(t,
		`{"event_type":"interaction.created","interaction":{"id":"v1","service_tier":"flex"}}`,
		looseCompleted)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish && strings.Contains(string(c.ProviderMetadata), "serviceTier") {
			t.Fatalf("finish metadata = %s, want no serviceTier", c.ProviderMetadata)
		}
	}
}
