package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A provider tool result flagged isError becomes a tool-error whose error is
// the result value; TS getErrorMessage / toUIMessageChunk write a structured
// value with JSON.stringify, which leaves '<', '>' and '&' literal. The Go
// error text was encoding/json's, which escapes them as < etc.
func TestR96streamProviderToolErrorTextIsJSONStringify(t *testing.T) {
	var mu sync.Mutex
	var errText string
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "p1", ToolName: "web_search", Arguments: map[string]interface{}{}, RawArguments: "{}", ProviderExecuted: true, Dynamic: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "p1", ToolName: "web_search", Result: map[string]interface{}{"message": "a<b & c>d"}, IsError: r90streamTrue()}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage},
		}),
		Prompt:  "x",
		OnError: func(context.Context, error) {},
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil && c.ToolResult.Error != nil {
				mu.Lock()
				errText = c.ToolResult.Error.Error()
				mu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadAll(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := `{"message":"a<b & c>d"}`; errText != want {
		t.Fatalf("tool error text = %q, want %q", errText, want)
	}
}
