package ai

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Owner decision 2026-10-28 (match TS): a tool approval that cannot be
// resolved (TS resolveToolApproval throws, here because the approval
// function's tool context fails its schema) and a tool context that fails its
// schema before execution (TS executeToolCall validates it outside its try)
// fail the call: GenerateText returns the error and StreamText errors the
// stream. Ordinary tool execution errors still reach the model as tool
// errors.

func failureToolCallModel(calls ...types.ToolCall) *testutil.MockLanguageModel {
	step := 0
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			step++
			if step == 1 {
				return &types.GenerateResult{ToolCalls: calls, FinishReason: types.FinishReasonToolCalls}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			step++
			if step == 1 {
				chunks := []provider.StreamChunk{}
				for i := range calls {
					c := calls[i]
					chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &c})
				}
				return testutil.NewMockTextStream(append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls})), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

func strictContextTool(name string, executed *atomic.Int32) types.Tool {
	return types.Tool{
		Name:          name,
		Parameters:    map[string]interface{}{"type": "object"},
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []string{"tenant"}}),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed.Add(1)
			return "ok", nil
		},
	}
}

func plainTool(name string, executed *atomic.Int32, err error) types.Tool {
	return types.Tool{
		Name:       name,
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed.Add(1)
			return "ok", err
		},
	}
}

func isToolContextValidationError(err error) bool {
	var v *providererrors.ValidationError
	if !errors.As(err, &v) || v.Context == nil || v.Context.Field != "tool context" {
		return false
	}
	// The caller gets the validation error itself, not the internal wrapper.
	var wrapped *toolCallFailureError
	return !errors.As(err, &wrapped)
}

func runTextCall(t *testing.T, stream bool, opts GenerateTextOptions) (*GenerateTextResult, error) {
	t.Helper()
	if !stream {
		return GenerateText(context.Background(), opts)
	}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: opts.Model, Prompt: opts.Prompt, Messages: opts.Messages, Tools: opts.Tools,
		ToolsContext: opts.ToolsContext, ToolApproval: opts.ToolApproval, StopWhen: opts.StopWhen,
	})
	if err != nil {
		return nil, err
	}
	if _, err := r.ReadAll(); err != nil {
		return nil, err
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	return &GenerateTextResult{Text: r.Text(), ToolResults: r.ToolResults()}, nil
}

func TestToolContextValidationFailsTheCall(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var executed atomic.Int32
		_, err := runTextCall(t, stream, GenerateTextOptions{
			Model:        failureToolCallModel(types.ToolCall{ID: "c1", ToolName: "strict", Arguments: map[string]interface{}{}}),
			Prompt:       "go",
			Tools:        []types.Tool{strictContextTool("strict", &executed)},
			ToolsContext: map[string]interface{}{"strict": map[string]interface{}{}},
			StopWhen:     []StopCondition{StepCountIs(3)},
		})
		if !isToolContextValidationError(err) {
			t.Errorf("stream=%v: err = %T %v, want the tool context ValidationError", stream, err, err)
		}
		if executed.Load() != 0 {
			t.Errorf("stream=%v: tool executed %d times", stream, executed.Load())
		}
	}
}

func TestToolApprovalResolutionErrorFailsTheCallBeforeAnyTool(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var executed, approvalCalls atomic.Int32
		_, err := runTextCall(t, stream, GenerateTextOptions{
			Model: failureToolCallModel(
				types.ToolCall{ID: "c1", ToolName: "plain", Arguments: map[string]interface{}{}},
				types.ToolCall{ID: "c2", ToolName: "strict", Arguments: map[string]interface{}{}},
			),
			Prompt:       "go",
			Tools:        []types.Tool{plainTool("plain", &executed, nil), strictContextTool("strict", &executed)},
			ToolsContext: map[string]interface{}{"strict": map[string]interface{}{}},
			ToolApproval: map[string]types.ToolApprovalValue{
				"strict": types.SingleToolApprovalFunc(func(map[string]interface{}, types.SingleToolApprovalOptions) types.ToolApprovalResult {
					approvalCalls.Add(1)
					return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
				}),
			},
			StopWhen: []StopCondition{StepCountIs(3)},
		})
		if !isToolContextValidationError(err) {
			t.Errorf("stream=%v: err = %T %v, want the tool context ValidationError", stream, err, err)
		}
		if approvalCalls.Load() != 0 {
			t.Errorf("stream=%v: approval function ran %d times", stream, approvalCalls.Load())
		}
		// TS resolves every approval before executing any tool (generate)
		// and errors the stream at the tool-call chunk (stream): no tool runs.
		if executed.Load() != 0 {
			t.Errorf("stream=%v: %d tools executed before the failing approval", stream, executed.Load())
		}
	}
}

func TestToolExecutionErrorStillReachesTheModel(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var executed atomic.Int32
		result, err := runTextCall(t, stream, GenerateTextOptions{
			Model:    failureToolCallModel(types.ToolCall{ID: "c1", ToolName: "plain", Arguments: map[string]interface{}{}}),
			Prompt:   "go",
			Tools:    []types.Tool{plainTool("plain", &executed, errors.New("boom"))},
			StopWhen: []StopCondition{StepCountIs(3)},
		})
		if err != nil {
			t.Fatalf("stream=%v: err = %v, want a tool-error result", stream, err)
		}
		if result.Text != "done" || executed.Load() != 1 {
			t.Errorf("stream=%v: text %q, executed %d", stream, result.Text, executed.Load())
		}
	}
}

// An approved continuation runs its tool through the same path: a tool
// context that fails its schema fails the call.
func TestResumedApprovedToolContextValidationFailsTheCall(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var executed atomic.Int32
		tool := strictContextTool("tool1", &executed)
		tool.Parameters = valueStringSchema()
		tool.ToolApproval = types.ToolApprovalStatusUserApproval
		_, err := runTextCall(t, stream, GenerateTextOptions{
			Model:        newRecordingModel("Hello"),
			Tools:        []types.Tool{tool},
			ToolsContext: map[string]interface{}{"tool1": map[string]interface{}{}},
			Messages:     approvalHistory("tool1", map[string]interface{}{"value": "value"}, "", true, ""),
			StopWhen:     []StopCondition{StepCountIs(3)},
		})
		if !isToolContextValidationError(err) {
			t.Errorf("stream=%v: err = %T %v, want the tool context ValidationError", stream, err, err)
		}
		if executed.Load() != 0 {
			t.Errorf("stream=%v: tool executed", stream)
		}
	}
}

// GenerateText resolves each call's approval once, before any tool runs, and
// executes with that decision (the approval function is not called again).
func TestGenerateTextResolvesEachApprovalOnce(t *testing.T) {
	var executed, approvalCalls atomic.Int32
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: failureToolCallModel(
			types.ToolCall{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}},
			types.ToolCall{ID: "c2", ToolName: "b", Arguments: map[string]interface{}{}},
		),
		Prompt: "go",
		Tools:  []types.Tool{plainTool("a", &executed, nil), plainTool("b", &executed, nil)},
		ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
			approvalCalls.Add(1)
			if o.ToolCall.ToolName == "b" {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
		}),
		StopWhen: []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if approvalCalls.Load() != 2 || executed.Load() != 1 {
		t.Fatalf("approval calls %d (want 2), executed %d (want 1)", approvalCalls.Load(), executed.Load())
	}
	ids := map[string]bool{}
	for _, part := range result.Content {
		if req, ok := part.(types.ToolApprovalRequestContent); ok {
			ids[req.ApprovalID] = true
		}
	}
	// Both calls carry an approval request (TS: an automatic approval is a
	// request plus its response), each with its own id.
	if len(ids) != 2 {
		t.Fatalf("approval requests %v, want two", ids)
	}
}
