package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Mutation survivor (provb fix 4): TS activeOutputItemIds records only
// message and reasoning items. A function call added at an output index
// leaves no active id there, so a text delta at that index keeps its own
// item_id (TS resolveOutputItemId falls back to itemId).
func TestR76Integ_StreamFunctionCallIDIsNotActiveOutputItem(t *testing.T) {
	ev := func(s string) string { return "data: " + s + "\n\n" }
	sse := ev(`{"type":"response.output_item.added","output_index":0,"sequence_number":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"","status":"in_progress"}}`) +
		ev(`{"type":"response.output_text.delta","item_id":"msg_x","output_index":0,"content_index":0,"delta":"hi","sequence_number":2}`) +
		ev(`{"type":"response.completed","sequence_number":3,"response":{"id":"r","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	var ids []string
	for _, c := range r76provbDrain(t, newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)) {
		if c.Type == provider.ChunkTypeText {
			ids = append(ids, c.ID)
		}
	}
	if got := strings.Join(ids, " "); got != "msg_x" {
		t.Errorf("text delta ids = %q, want msg_x", got)
	}
}
