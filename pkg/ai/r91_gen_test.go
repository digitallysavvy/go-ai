package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 91 gen tests (TS 2ebc78b654).

// r91genMCPDynamicResult is a provider response holding a provider-executed
// MCP call of a tool that is not in the tool set (OpenAI mcp.*), its
// provider result, and the final text.
func r91genMCPDynamicResult() *types.GenerateResult {
	return &types.GenerateResult{
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "mcp_1", ToolName: "mcp.web_search_exa", Input: `{"query":"x"}`, ProviderExecuted: true, Dynamic: true},
			types.ToolResultContent{ToolCallID: "mcp_1", ToolName: "mcp.web_search_exa", Result: "found", ProviderExecuted: true, Dynamic: true},
			types.TextContent{Text: "done"},
		},
		ToolCalls:    []types.ToolCall{{ID: "mcp_1", ToolName: "mcp.web_search_exa", RawArguments: `{"query":"x"}`, Arguments: map[string]interface{}{"query": "x"}, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true}},
		FinishReason: types.FinishReasonStop,
	}
}

func r91genNoToolError(t *testing.T, content []types.ContentPart, msgs []types.Message) {
	t.Helper()
	for _, m := range msgs {
		if m.Role == types.RoleTool {
			t.Errorf("response messages hold a role:tool message %+v; TS sends none for a provider-executed call", m)
		}
	}
	for _, part := range content {
		if r, ok := part.(types.ToolResultContent); ok && r.Error != "" {
			t.Errorf("content holds a client tool error %q", r.Error)
		}
	}
}

// core1-C01: generate-text.ts:1526 runs only the client tool calls
// (!providerExecuted), and execute-tool-call.ts:89 gives a non-executable
// tool no output, so a provider-executed dynamic call (OpenAI MCP) gets no
// client "tool not found" error and no role:tool message; otherwise the next
// turn sends a function_call_output that OpenAI rejects.
func TestR91genProviderDynamicCallNotToolNotFound(t *testing.T) {
	local := types.Tool{Name: "local", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	t.Run("generateText", func(t *testing.T) {
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return r91genMCPDynamicResult(), nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{local}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Steps) != 1 {
			t.Fatalf("steps = %d, want 1", len(res.Steps))
		}
		r91genNoToolError(t, res.Content, res.ResponseMessages)
		for _, tr := range res.ToolResults {
			if tr.Error != nil {
				t.Errorf("tool result error %v", tr.Error)
			}
		}
	})
	t.Run("streamText", func(t *testing.T) {
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "mcp_1", ToolName: "mcp.web_search_exa", RawArguments: `{"query":"x"}`, Arguments: map[string]interface{}{"query": "x"}, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true}},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "mcp_1", ToolName: "mcp.web_search_exa", Result: "found", ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true}},
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}}
		res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{local}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
		steps := res.Steps()
		if len(steps) != 1 {
			t.Fatalf("steps = %d, want 1", len(steps))
		}
		r91genNoToolError(t, steps[0].Content, res.ResponseMessages())
	})
}
