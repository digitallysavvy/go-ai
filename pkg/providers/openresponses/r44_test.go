package openresponses

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r44Drain(t *testing.T, events ...string) []*provider.StreamChunk {
	t.Helper()
	sse := ""
	for _, e := range events {
		sse += "data: " + e + "\n\n"
	}
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(sse)), nil, "open-responses")
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil || c == nil {
			return out
		}
		out = append(out, c)
	}
}

// TS keeps the toolCallId / toolName recorded at output_item.added
// (`toolCall?.toolCallId ?? item.call_id`), and
// response.function_call_arguments.done overwrites the accumulated arguments
// even with "".
func TestR44_FunctionCallDoneKeepsAddedIdentityAndEmptyDoneArguments(t *testing.T) {
	chunks := r44Drain(t,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"i1","call_id":"c1","name":"f","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"i1","delta":"{\"a\":1}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"i1","arguments":""}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"i1","call_id":"c2","name":"g","arguments":""}}`,
	)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			if c.ToolCall.ID != "c1" || c.ToolCall.ToolName != "f" || c.ToolCall.RawArguments != "" {
				t.Fatalf("tool call = %#v", c.ToolCall)
			}
			return
		}
	}
	t.Fatal("no tool call")
}

// TS: reasoningContent == null ? null : [...], so an empty content array stays [].
func TestR44_ReasoningEndKeepsEmptyContentArray(t *testing.T) {
	chunks := r44Drain(t,
		`{"type":"response.output_item.done","item":{"type":"reasoning","id":"r1","summary":[],"content":[]}}`,
		`{"type":"response.output_item.done","item":{"type":"reasoning","id":"r2","summary":[]}}`,
	)
	var got []string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeReasoningEnd {
			got = append(got, string(c.ProviderMetadata))
		}
	}
	if len(got) != 2 || !strings.Contains(got[0], `"reasoningContent":[]`) || !strings.Contains(got[1], `"reasoningContent":null`) {
		t.Fatalf("metadata = %v", got)
	}
}

// The custom_tool_call done event keeps the call id and name recorded at
// output_item.added, as for function calls (TS `toolCall?.toolCallId ??
// item.call_id`).
func TestR44_CustomToolCallDoneKeepsAddedIdentity(t *testing.T) {
	chunks := r44Drain(t,
		`{"type":"response.output_item.added","item":{"type":"custom_tool_call","id":"i1","call_id":"c1","name":"f","input":""}}`,
		`{"type":"response.output_item.done","item":{"type":"custom_tool_call","id":"i1","call_id":"c2","name":"g","input":"x"}}`,
	)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			if c.ToolCall.ID != "c1" || c.ToolCall.ToolName != "f" {
				t.Fatalf("tool call = %#v", c.ToolCall)
			}
			return
		}
	}
	t.Fatal("no tool call")
}
