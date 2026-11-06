package openresponses

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS seeds the function-call accumulator from output_item.added's own
// `arguments`; argument deltas append to it, and an output_item.done without
// arguments falls back to the accumulator.
func TestStreamFunctionCallSeedsAccumulatorFromAddedItem(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"city\":"}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"\"Paris\"}"}`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":""}}`,
		`data: {"type":"response.completed","response":{"status":"completed"}}`,
	}, "\n\n") + "\n\n"
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(body)), nil, "open-responses")
	for {
		chunk, err := s.Next()
		if err != nil {
			t.Fatalf("no tool-call chunk before end: %v", err)
		}
		if chunk.Type != provider.ChunkTypeToolCall {
			continue
		}
		if chunk.ToolCall.RawArguments != `{"city":"Paris"}` {
			t.Fatalf("arguments = %q", chunk.ToolCall.RawArguments)
		}
		return
	}
}

// TS enqueues {type:'raw', rawValue} before processing each parsed event when
// includeRawChunks is set (the [DONE] marker is not an event).
func TestStreamIncludeRawChunks(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","item":{"type":"message","id":"m1","role":"assistant","content":[]}}`,
		`data: {"type":"response.output_text.delta","item_id":"m1","delta":"hi"}`,
		`data: not-json`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(body)), nil, "open-responses")
	s.includeRawChunks = true
	var got []string
	for {
		chunk, err := s.Next()
		if err != nil {
			break
		}
		got = append(got, string(chunk.Type))
	}
	want := []string{"raw", "text-start", "raw", "text", "raw", "error", "finish"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("chunks = %v, want %v", got, want)
	}
}
