package middleware

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r98genProtoMetadata = `{"p":{"__proto__":{"x":1},"constructor":{"prototype":{"y":2}},"sig":"keep"}}`

// TS simulateStreamingMiddleware enqueues tool-call / tool-result parts with
// their providerMetadata as is (no re-parse): __proto__ keys survive.
func TestR98genSimulateStreamingMetadataKeepsProtoKeys(t *testing.T) {
	var want map[string]interface{}
	if err := json.Unmarshal([]byte(r98genProtoMetadata), &want); err != nil {
		t.Fatal(err)
	}
	s := &simulatedStream{result: &types.GenerateResult{Content: []types.ContentPart{
		types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: `{}`, ProviderMetadata: json.RawMessage(r98genProtoMetadata)},
		types.ToolResultContent{ToolCallID: "c1", ToolName: "t", ProviderExecuted: true, ProviderMetadata: json.RawMessage(r98genProtoMetadata)},
	}}}
	s.buildChunks()
	var calls, results int
	for _, c := range s.chunks {
		if c.ToolCall != nil {
			calls++
			if !reflect.DeepEqual(c.ToolCall.ProviderMetadata, want) {
				t.Errorf("tool call metadata = %#v", c.ToolCall.ProviderMetadata)
			}
		}
		if c.ToolResult != nil {
			results++
			if !reflect.DeepEqual(c.ToolResult.ProviderMetadata, want) {
				t.Errorf("tool result metadata = %#v", c.ToolResult.ProviderMetadata)
			}
		}
	}
	if calls != 1 || results != 1 {
		t.Fatalf("calls %d results %d", calls, results)
	}
}
