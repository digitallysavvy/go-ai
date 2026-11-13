package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func builtinReadTool() types.Tool {
	return types.Tool{Name: "read", Parameters: map[string]interface{}{
		"type":       "object",
		"required":   []interface{}{"file_path"},
		"properties": map[string]interface{}{"file_path": map[string]interface{}{"type": "string"}},
	}}
}

func runDynamicRead(t *testing.T, filtering *BuiltinToolFiltering, active map[string]types.Tool, tools map[string]types.Tool, input string) []provider.StreamChunk {
	t.Helper()
	mock := newMockHarness(mockHarnessOptions{
		builtinTools: map[string]BuiltinTool{"read": {Tool: builtinReadTool()}},
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "read", Input: input, ProviderExecuted: true, Dynamic: true},
				&ToolResultPart{ToolCallID: "c1", ToolName: "read", Result: "MCP result", Dynamic: true},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: active, BuiltinToolFiltering: filtering,
	})
	return drainRunPrompt(t, out)
}

// Ports TS #22263: a provider-executed dynamic call that shares a name with a
// filtered-out builtin skips the builtin's schema.
func TestRunPrompt_DynamicCallWithFilteredBuiltinNameSkipsBuiltinSchema(t *testing.T) {
	builtins := map[string]types.Tool{"read": builtinReadTool()}
	for name, filtering := range map[string]*BuiltinToolFiltering{
		"deny":  {Mode: BuiltinToolFilteringDeny, ToolNames: []string{"read"}},
		"allow": {Mode: BuiltinToolFilteringAllow, ToolNames: []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			chunks := runDynamicRead(t, filtering, map[string]types.Tool{}, builtins, `{"query":"hello"}`)
			calls := chunksOfType(chunks, provider.ChunkTypeToolCall)
			if len(calls) != 1 || calls[0].ToolCall.Invalid || !calls[0].ToolCall.Dynamic {
				t.Fatalf("tool-call chunks = %+v, want one valid dynamic call", calls)
			}
			if got := calls[0].ToolCall.Arguments["query"]; got != "hello" {
				t.Fatalf("arguments = %+v", calls[0].ToolCall.Arguments)
			}
			if n := len(chunksOfType(chunks, provider.ChunkTypeToolResult)); n != 1 {
				t.Fatalf("tool-result chunks = %d, want 1", n)
			}
		})
	}
}

// An enabled (active) builtin keeps its schema validation.
func TestRunPrompt_DynamicCallWithActiveBuiltinStillValidated(t *testing.T) {
	builtins := map[string]types.Tool{"read": builtinReadTool()}
	chunks := runDynamicRead(t, nil, map[string]types.Tool{}, builtins, `{"query":"hello"}`)
	calls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(calls) != 1 || !calls[0].ToolCall.Invalid {
		t.Fatalf("tool-call chunks = %+v, want one invalid call", calls)
	}
}

// An active host tool owning the same name keeps its schema.
func TestRunPrompt_DynamicCallWithActiveHostToolKeepsSchema(t *testing.T) {
	hostRead := types.Tool{Name: "read", Parameters: map[string]interface{}{
		"type":       "object",
		"required":   []interface{}{"query"},
		"properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
	}}
	tools := map[string]types.Tool{"read": hostRead}
	chunks := runDynamicRead(t, &BuiltinToolFiltering{Mode: BuiltinToolFilteringDeny, ToolNames: []string{"read"}}, tools, tools, `{"file_path":"example.txt"}`)
	calls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(calls) != 1 || !calls[0].ToolCall.Invalid {
		t.Fatalf("tool-call chunks = %+v, want one invalid call", calls)
	}
}
