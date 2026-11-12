package ai

import (
	"context"
	"errors"
	"testing"
	"time"

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

// core1-C02: generate-text.ts:1718 loops while shouldStartNextStep(), with
// no step ceiling, so StopWhen decides past 1000 steps.
func TestR91genGenerateNoStepCeiling(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: "c", ToolName: "t", Input: `{}`}},
			ToolCalls:    []types.ToolCall{{ID: "c", ToolName: "t", Arguments: map[string]interface{}{}, RawArguments: `{}`}},
			FinishReason: types.FinishReasonToolCalls,
		}, nil
	}}
	tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{tool}, StopWhen: []StopCondition{IsStepCount(1005)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1005 {
		t.Fatalf("steps = %d, want 1005", len(res.Steps))
	}
}

// core1-D01 (gen half): generate-text.ts:1808 passes the final step's
// response to parseCompleteOutput, so NoObjectGeneratedError.Response is the
// model response (id, model id, headers).
func TestR91genOutputParseErrorResponse(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:          []types.ContentPart{types.TextContent{Text: "not json"}},
			FinishReason:     types.FinishReasonStop,
			Usage:            types.Usage{InputTokens: r91genInt64(3)},
			ResponseMetadata: &types.ResponseMetadata{ID: "resp-1", ModelID: "m-1"},
		}, nil
	}}
	type obj struct {
		Name string `json:"name"`
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Output: ObjectOutput[obj](ObjectOutputOptions{Schema: SchemaFor[obj]()})})
	var noObj *NoObjectGeneratedError
	if !errors.As(err, &noObj) {
		t.Fatalf("err = %v, want NoObjectGeneratedError", err)
	}
	if noObj.Response == nil || noObj.Response.ID != "resp-1" || noObj.Response.ModelID != "m-1" {
		t.Fatalf("Response = %+v, want id resp-1 / model m-1", noObj.Response)
	}
	if noObj.Usage == nil || noObj.Usage.InputTokens == nil || *noObj.Usage.InputTokens != 3 {
		t.Fatalf("Usage = %+v", noObj.Usage)
	}
}

func r91genInt64(v int64) *int64 { return &v }

// r91genTimeoutTool blocks until its context ends and fails with the cause.
func r91genTimeoutTool() types.Tool {
	return types.Tool{Name: "slow", Parameters: map[string]interface{}{"type": "object"}, Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}}
}

// core1-A01 (gen tool sites): a configured tool timeout is the TS
// mergeAbortSignals(abortSignal, toolMs) reason even when it is zero
// (AbortSignal.timeout(0)), so the tool error is built with
// NewToolTimeoutError, which marks the duration as set.
func TestR91genZeroToolTimeoutMarkedSet(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: "c", ToolName: "slow", Input: `{}`}},
			ToolCalls:    []types.ToolCall{{ID: "c", ToolName: "slow", Arguments: map[string]interface{}{}, RawArguments: `{}`}},
			FinishReason: types.FinishReasonToolCalls,
		}, nil
	}}
	zero := time.Duration(0)
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{r91genTimeoutTool()}, Timeout: &TimeoutConfig{ToolMs: &zero}})
	if err != nil {
		t.Fatal(err)
	}
	var toolErr error
	for _, part := range res.Steps[0].Content {
		if e, ok := part.(types.ToolErrorContent); ok {
			toolErr = e.Err
		}
	}
	var te *TimeoutError
	if !errors.As(toolErr, &te) {
		t.Fatalf("tool error = %v, want *TimeoutError", toolErr)
	}
	if te.Reason != TimeoutReasonTool || !te.timeoutSet {
		t.Fatalf("tool timeout error = %+v, want a tool timeout with its duration set", te)
	}
}
