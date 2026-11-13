package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS output_item.done for apply_patch_call emits the final tool-call only for
// an item that output_item.added announced and whose status is "completed".
func TestResponsesStream_ApplyPatchToolCallRequiresCompletedStatus(t *testing.T) {
	added := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"apply_patch_call","id":"ap_1","call_id":"call_1","status":"in_progress","operation":{"type":"create_file","path":"a.go","diff":""}}}` + "\n\n"
	done := func(status string) string {
		return `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"apply_patch_call","id":"ap_1","call_id":"call_1","status":"` + status + `","operation":{"type":"create_file","path":"a.go","diff":"+x"}}}` + "\n\n"
	}
	toolCalls := func(sse string) int {
		n := 0
		for _, c := range drainResponsesStream(t, sse) {
			if c.Type == provider.ChunkTypeToolCall {
				n++
			}
		}
		return n
	}
	if got := toolCalls(added + done("completed")); got != 1 {
		t.Errorf("completed: %d tool calls, want 1", got)
	}
	if got := toolCalls(added + done("in_progress")); got != 0 {
		t.Errorf("in_progress: %d tool calls, want 0", got)
	}
	if got := toolCalls(done("completed")); got != 0 {
		t.Errorf("never added: %d tool calls, want 0", got)
	}
}
