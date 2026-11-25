package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS 6815153d64: a provider-executed preliminary tool-result is forwarded with
// its preliminary flag and is not recorded as a final result in content.
func TestPortStreamPreliminaryProviderToolResult(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "image_generation", RawArguments: "{}", ProviderExecuted: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "image_generation", Result: map[string]interface{}{"result": "partial-image"}, Preliminary: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "image_generation", Result: map[string]interface{}{"result": "final-image"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var seen []bool
	fs := res.FullStream()
	for {
		c, err := fs.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil {
			seen = append(seen, c.ToolResult.Preliminary)
		}
	}
	if len(seen) != 2 || !seen[0] || seen[1] {
		t.Fatalf("stream tool-result preliminary flags = %v, want [true false]", seen)
	}
	n := 0
	for _, p := range res.Content() {
		if tr, ok := p.(types.ToolResultContent); ok {
			n++
			if tr.Preliminary || tr.Result.(map[string]interface{})["result"] != "final-image" {
				t.Fatalf("content tool-result = %+v, want final-image", tr)
			}
		}
	}
	if n != 1 {
		t.Fatalf("content tool-results = %d, want 1", n)
	}
}
