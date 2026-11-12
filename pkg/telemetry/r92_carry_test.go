package telemetry

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// GetBaseAttributes leaves out credential headers whatever their case.
func TestR92carryGetBaseAttributesSkipsCredentialHeadersCaseInsensitively(t *testing.T) {
	attrs := GetBaseAttributes("p", "m", nil, map[string]string{
		"authorization": "Bearer s1", "AUTHORIZATION": "Bearer s2", "X-Api-Key": "s3", "Api-Key": "s4", "x-trace": "t",
	})
	for _, kv := range attrs {
		switch string(kv.Key) {
		case "ai.model.provider", "ai.model.id", "ai.request.headers.x-trace":
		default:
			t.Fatalf("unexpected attribute %s = %s", kv.Key, kv.Value.String())
		}
	}
	if len(attrs) != 3 {
		t.Fatalf("attrs = %v", attrs)
	}
}

// TS records toolCall.input as parsed from the model text, in its key order
// (ai.response.toolCalls, gen_ai tool_call arguments, gen_ai.tool.call.arguments).
func TestR92carryToolCallInputKeepsModelKeyOrder(t *testing.T) {
	tc := types.ToolCall{ID: "c1", ToolName: "w", RawArguments: `{"zeta":1,"alpha":{"y":2,"x":3}}`,
		Arguments: map[string]interface{}{"zeta": 1.0, "alpha": map[string]interface{}{"y": 2.0, "x": 3.0}}}
	got, ok := legacyToolCallsJSON([]types.ToolCall{tc})
	if want := `[{"toolCallId":"c1","toolName":"w","input":{"zeta":1,"alpha":{"y":2,"x":3}}}]`; !ok || got != want {
		t.Fatalf("ai.response.toolCalls = %s\nwant %s", got, want)
	}
	b, err := jsonstr.Marshal(FormatOutputMessages(FormatOutputMessagesOptions{ToolCalls: []types.ToolCall{tc}, FinishReason: "tool-calls"}))
	if err != nil || !strings.Contains(string(b), `"arguments":{"zeta":1,"alpha":{"y":2,"x":3}}`) {
		t.Fatalf("gen_ai.output.messages = %s", b)
	}
	args, ok := toolCallArgumentsJSON(types.ToolCallContent{ToolCallID: "c1", ToolName: "w", Input: tc.RawArguments, Arguments: tc.Arguments})
	if !ok || args != `{"zeta":1,"alpha":{"y":2,"x":3}}` {
		t.Fatalf("gen_ai.tool.call.arguments = %s", args)
	}
	// Refined values win; the raw text only gives the order.
	refined := types.ToolCall{ID: "c2", ToolName: "w", RawArguments: `{"b":"raw","a":1}`, Arguments: map[string]interface{}{"a": 1.0, "b": "refined"}}
	if got, _ := legacyToolCallsJSON([]types.ToolCall{refined}); !strings.Contains(got, `"input":{"b":"refined","a":1}`) {
		t.Fatalf("refined = %s", got)
	}
	// An input that is not JSON stays the raw text.
	if got, _ := legacyToolCallsJSON([]types.ToolCall{{ID: "c3", ToolName: "w", RawArguments: "{bad"}}); !strings.Contains(got, `"input":"{bad"`) {
		t.Fatalf("invalid = %s", got)
	}
}
