package xai

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR96provbStreamMetadataJSONStringify: xAI stream metadata built from
// provider values is written like JSON.stringify: <, > and & stay literal
// (main ruling 4, write side).
func TestR96provbStreamMetadataJSONStringify(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.reasoning_summary_part.added","item_id":"<a&b>","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":""}}

`)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeReasoningStart {
			if !strings.Contains(string(c.ProviderMetadata), `"itemId":"<a&b>"`) {
				t.Fatalf("metadata = %s", c.ProviderMetadata)
			}
			return
		}
	}
	t.Fatal("no reasoning-start chunk")
}

// TestR96provbFunctionCallDoneItemVerbatim: TS emits the done item's
// call_id and name verbatim (required strings), so a present "" is not
// replaced by the values of the added item.
func TestR96provbFunctionCallDoneItemVerbatim(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"c1","name":"weather","arguments":"","status":"in_progress"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"","name":"","arguments":"{}","status":"completed"}}

`)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			if c.ToolCall.ID != "" || c.ToolCall.ToolName != "" || c.ToolCall.RawArguments != "{}" {
				t.Fatalf("tool call = %+v, want the done item's empty call_id and name", c.ToolCall)
			}
			return
		}
	}
	t.Fatal("no tool-call chunk")
}
