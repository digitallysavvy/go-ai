package ai

// Round 76 integrator: change requests wired after the gen/stream merges
// (the InternalOptions executor switches in GenerateText, approval
// revalidation and UI message validation through Tool.ValidateInput).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// GenerateText reads InternalOptions.ClientToolResultsWithoutCallMetadata
// (TS WorkflowAgent executeTool builds client results without
// providerOptions; TS core copies toolCall.providerMetadata): with it, the
// step's tool result and the next prompt's tool-result part carry no
// provider metadata; without it they carry the call's.
func TestR76Integ_GenerateTextClientToolResultsWithoutCallMetadata(t *testing.T) {
	md := map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig"}}
	for _, without := range []bool{false, true} {
		var calls []*provider.GenerateOptions
		m := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true, DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls = append(calls, o)
			if len(calls) == 1 {
				return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}, ProviderMetadata: md}}, FinishReason: types.FinishReasonToolCalls}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}}
		opts := GenerateTextOptions{Model: m, Prompt: "x", StopWhen: []StopCondition{IsStepCount(2)}, Tools: []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}}}
		if without {
			opts.Internal = &InternalOptions{ClientToolResultsWithoutCallMetadata: true}
		}
		res, err := GenerateText(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 || len(res.Steps) != 2 || len(res.Steps[0].ToolResults) != 1 {
			t.Fatalf("without=%v: calls = %d, steps = %d", without, len(calls), len(res.Steps))
		}
		prompt := calls[1].Prompt.Messages
		tr, _ := prompt[len(prompt)-1].Content[0].(types.ToolResultContent)
		got := res.Steps[0].ToolResults[0].ProviderMetadata
		if without && (got != nil || tr.ProviderOptions != nil) {
			t.Errorf("without: result metadata = %v, prompt providerOptions = %v; want none", got, tr.ProviderOptions)
		}
		if !without && (got == nil || tr.ProviderOptions == nil) {
			t.Errorf("default: result metadata = %v, prompt providerOptions = %v; want the call's", got, tr.ProviderOptions)
		}
	}
}

// r76integRejectingTool is a tool whose ValidateInput rejects every input.
func r76integRejectingTool(executed *bool) types.Tool {
	return types.Tool{
		Name: "weather",
		// No Parameters: ValidateInput alone makes the tool validated.
		ValidateInput: func(interface{}) (interface{}, error) {
			return nil, errors.New("city is required")
		},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			*executed = true
			return "rain", nil
		},
	}
}

// TS validateApprovedToolApprovals revalidates an approved input with
// safeValidateTypes({schema: tool.inputSchema}), which runs the jsonSchema
// validate function: an approved call whose tool's ValidateInput rejects is
// invalid (a TypeValidationError cause), and it is not approved.
func TestR76Integ_ApprovalRevalidationUsesValidateInput(t *testing.T) {
	executed := false
	approval := CollectedToolApproval{
		ApprovalRequest:  types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "c1"},
		ApprovalResponse: types.ToolApprovalResponseContent{ApprovalID: "a1", Approved: true},
		ToolCall:         types.ToolCall{ID: "c1", ToolName: "weather", Arguments: map[string]interface{}{"town": "London"}},
	}
	res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
		ApprovedToolApprovals: []CollectedToolApproval{approval},
		Tools:                 []types.Tool{r76integRejectingTool(&executed)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ApprovedToolApprovals) != 0 || len(res.InvalidToolApprovals) != 1 {
		t.Fatalf("approved = %d, invalid = %d; want 0, 1", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
	}
	msg := res.InvalidToolApprovals[0].Error.Error()
	if !strings.Contains(msg, "Invalid input for tool weather") || !strings.Contains(msg, "AI_TypeValidationError: Type validation failed") || !strings.Contains(msg, "Error message: Error: city is required") {
		t.Errorf("error = %q", msg)
	}

	// A ValidateInput that accepts the input as is keeps the approval.
	ok := r76integRejectingTool(&executed)
	ok.ValidateInput = func(v interface{}) (interface{}, error) { return v, nil }
	res, err = ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
		ApprovedToolApprovals: []CollectedToolApproval{approval},
		Tools:                 []types.Tool{ok},
	})
	if err != nil || len(res.ApprovedToolApprovals) != 1 || len(res.InvalidToolApprovals) != 0 {
		t.Fatalf("accepting validator: res = %+v, err = %v", res, err)
	}
	if executed {
		t.Error("validation ran the tool")
	}
}

// TS validateUIMessages validates a static tool part's input with
// safeValidateTypes({schema: tool.inputSchema}): Tool.ValidateInput replaces
// the Parameters check, and its failure is a TypeValidationError whose cause
// shows as getErrorMessage ("Error: <message>").
func TestR76Integ_UIMessageToolInputUsesValidateInput(t *testing.T) {
	executed := false
	msgs := `[{"id":"m1","role":"assistant","parts":[{"type":"tool-weather","toolCallId":"c1","state":"input-available","input":{"town":"London"}}]}]`
	res := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msgs, Tools: []types.Tool{r76integRejectingTool(&executed)}})
	if res.Success || res.Error == nil || !strings.Contains(res.Error.Error(), "Error: city is required") {
		t.Fatalf("res = %+v, want the ValidateInput failure", res)
	}
	accepting := r76integRejectingTool(&executed)
	accepting.Parameters = map[string]interface{}{"type": "object", "required": []interface{}{"city"}}
	accepting.ValidateInput = func(v interface{}) (interface{}, error) { return v, nil }
	if res := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msgs, Tools: []types.Tool{accepting}}); !res.Success {
		t.Fatalf("accepting validator: err = %v (the Parameters schema must not be checked)", res.Error)
	}
}

// Mutation survivor (stream fix 2): TS do-stream-step records 'available'
// only for a call whose tool-call part follows a tool-input-start for its id,
// so under DeferToolInputCallbacks a call streamed without a
// tool-input-start fires no OnInputAvailable; plain StreamText (TS
// streamText) fires it for every valid call.
func TestR76Integ_DeferredToolInputSkipsCallWithoutStart(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		var available int32
		tool := types.Tool{
			Name:       "search",
			Parameters: map[string]interface{}{"type": "object"},
			OnInputAvailable: func(context.Context, types.OnInputAvailableOptions) error {
				available++
				return nil
			},
		}
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "search", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}}
		opts := StreamTextOptions{Model: model, Prompt: "p", Tools: []types.Tool{tool}, OnError: func(context.Context, error) {}}
		if deferred {
			opts.Internal = &InternalOptions{DeferToolInputCallbacks: true}
		}
		r, err := StreamText(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.ConsumeStream()
		want := int32(1)
		if deferred {
			want = 0
		}
		if available != want {
			t.Errorf("deferred=%v: OnInputAvailable ran %d times, want %d", deferred, available, want)
		}
	}
}
