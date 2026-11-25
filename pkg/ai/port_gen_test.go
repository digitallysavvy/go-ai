package ai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func portgenCounterTool(validate func(interface{}) (interface{}, error)) types.Tool {
	return types.Tool{
		Name:          "tool1",
		Parameters:    map[string]interface{}{"type": "object"},
		ValidateInput: validate,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
}

func portgenCounterApproval(secret []byte, schemaInput map[string]interface{}) CollectedToolApproval {
	input := map[string]interface{}{"counter": map[string]interface{}{"value": float64(2)}}
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: input}
	request := types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", ToolCall: call}
	if schemaInput != nil {
		request.InputSchemaInput = schemaInput
		request.InputSchemaInputSet = true
	}
	if secret != nil {
		sig, err := SignToolApproval(secret, "approval-1", "call-1", "tool1", input)
		if err != nil {
			panic(err)
		}
		request.Signature = sig
	}
	return CollectedToolApproval{
		ApprovalRequest:  request,
		ApprovalResponse: types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
		ToolCall:         call,
	}
}

// Port of validate-tool-approvals.test.ts 'should reject signed approvals
// changed by an in-place schema validator' (TS 3c09e0011c).
func TestPortgenApprovalRejectsInPlaceSchemaValidator(t *testing.T) {
	secret := []byte("test-secret")
	approval := portgenCounterApproval(secret, nil)
	original := portgenCounterApproval(secret, nil)
	tool := portgenCounterTool(func(v interface{}) (interface{}, error) {
		counter := v.(map[string]interface{})["counter"].(map[string]interface{})
		counter["value"] = counter["value"].(float64) + 1
		return v, nil
	})
	res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
		ApprovedToolApprovals: []CollectedToolApproval{approval},
		Tools:                 []types.Tool{tool},
		ToolApprovalSecret:    secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ApprovedToolApprovals) != 0 || len(res.InvalidToolApprovals) != 1 {
		t.Fatalf("approved=%d invalid=%d, want 0 and 1", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
	}
	if !strings.Contains(res.InvalidToolApprovals[0].Error.Error(), "does not match the validated schema output") {
		t.Errorf("error = %v", res.InvalidToolApprovals[0].Error)
	}
	if res.InvalidToolApprovals[0].Error.ErrorName() != "AI_InvalidToolInputError" {
		t.Errorf("name = %s", res.InvalidToolApprovals[0].Error.ErrorName())
	}
	if !reflect.DeepEqual(approval, original) {
		t.Errorf("input approval mutated: %+v", approval)
	}
}

// Port of tool-approval-follow-up.test.ts 'should isolate in-place refinement
// during signed approval replay (schema input supplied: %s)' at the
// ValidateApprovedToolApprovals level.
func TestPortgenApprovalIsolatesInPlaceRefinement(t *testing.T) {
	for _, hasSchemaInput := range []bool{false, true} {
		name := "without schema input"
		var schemaInput map[string]interface{}
		if hasSchemaInput {
			name = "with schema input"
			schemaInput = map[string]interface{}{"counter": map[string]interface{}{"value": float64(1)}}
		}
		t.Run(name, func(t *testing.T) {
			secret := []byte("test-secret")
			approval := portgenCounterApproval(secret, schemaInput)
			original := portgenCounterApproval(secret, schemaInput)
			refiners := map[string]ToolInputRefiner{"tool1": func(_ context.Context, o ToolInputRefinementOptions) (map[string]interface{}, error) {
				counter := o.ToolCall.Arguments["counter"].(map[string]interface{})
				counter["value"] = counter["value"].(float64) + 1
				return o.ToolCall.Arguments, nil
			}}
			res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
				ApprovedToolApprovals: []CollectedToolApproval{approval},
				Tools:                 []types.Tool{portgenCounterTool(nil)},
				ToolApprovalSecret:    secret,
				RefineToolInput:       refiners,
			})
			if err != nil {
				t.Fatal(err)
			}
			if hasSchemaInput {
				if len(res.ApprovedToolApprovals) != 1 {
					t.Fatalf("approved=%d invalid=%d", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
				}
				got := res.ApprovedToolApprovals[0].ToolCall.Arguments
				want := map[string]interface{}{"counter": map[string]interface{}{"value": float64(2)}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("approved input = %v, want %v", got, want)
				}
			} else {
				if len(res.ApprovedToolApprovals) != 0 || len(res.InvalidToolApprovals) != 1 {
					t.Fatalf("approved=%d invalid=%d, want 0 and 1", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
				}
				if !strings.Contains(res.InvalidToolApprovals[0].Error.Error(), "does not match the validated schema output") {
					t.Errorf("error = %v", res.InvalidToolApprovals[0].Error)
				}
			}
			if !reflect.DeepEqual(approval, original) {
				t.Errorf("input approval mutated: %+v", approval)
			}
		})
	}
}

// Port of tool-approval-follow-up.test.ts 'should isolate in-place refinement
// during signed approval replay' for generateText (TS 3c09e0011c): the
// history is not mutated and the approved input is what executes.
func TestPortgenGenerateTextIsolatesInPlaceRefinement(t *testing.T) {
	for _, hasSchemaInput := range []bool{false, true} {
		t.Run(map[bool]string{false: "no schema input", true: "schema input"}[hasSchemaInput], func(t *testing.T) {
			secret := []byte("test-secret")
			input := map[string]interface{}{"counter": map[string]interface{}{"value": float64(2)}}
			sig, err := SignToolApproval(secret, "approval-1", "call-1", "tool1", input)
			if err != nil {
				t.Fatal(err)
			}
			request := types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", Signature: sig}
			if hasSchemaInput {
				request.InputSchemaInput = map[string]interface{}{"counter": map[string]interface{}{"value": float64(1)}}
				request.InputSchemaInputSet = true
			}
			messages := []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: input},
					request,
				}},
				{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true}}},
			}
			wantInput := map[string]interface{}{"counter": map[string]interface{}{"value": float64(2)}}
			wantSchemaInput := map[string]interface{}{"counter": map[string]interface{}{"value": float64(1)}}
			var executed []map[string]interface{}
			tool := portgenCounterTool(nil)
			tool.ToolApproval = types.ToolApprovalStatusUserApproval
			tool.Execute = func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				executed = append(executed, in)
				return "recorded", nil
			}
			model := newRecordingModel("OK")
			result, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:                          model,
				Tools:                          []types.Tool{tool},
				Messages:                       messages,
				StopWhen:                       []StopCondition{StepCountIs(3)},
				ExperimentalToolApprovalSecret: secret,
				ExperimentalRefineToolInput: map[string]ToolInputRefiner{"tool1": func(_ context.Context, o ToolInputRefinementOptions) (map[string]interface{}, error) {
					counter := o.ToolCall.Arguments["counter"].(map[string]interface{})
					counter["value"] = counter["value"].(float64) + 1
					return o.ToolCall.Arguments, nil
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			_ = result
			// The refiner also runs when the model's own tool calls are
			// parsed, but this call is a replay: no model tool call exists.
			if hasSchemaInput {
				if len(executed) != 1 || !reflect.DeepEqual(executed[0], wantInput) {
					t.Fatalf("executed = %v, want one call with %v", executed, wantInput)
				}
			} else if len(executed) != 0 {
				t.Fatalf("executed = %v, want none", executed)
			}
			if !reflect.DeepEqual(input, wantInput) {
				t.Errorf("history tool call input mutated: %v", input)
			}
			if hasSchemaInput && !reflect.DeepEqual(request.InputSchemaInput, wantSchemaInput) {
				t.Errorf("history schema input mutated: %v", request.InputSchemaInput)
			}
			tr := singleToolResult(t, model.lastMessageOfFirstPrompt(t))
			if hasSchemaInput {
				if tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "recorded" {
					t.Errorf("output = %+v", tr.Output)
				}
			} else if tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText || !strings.Contains(tr.Output.Value.(string), "does not match the validated schema output") {
				t.Errorf("output = %+v", tr.Output)
			}
		})
	}
}

// portgenFindApprovalRequest returns the first approval request in the
// assistant messages.
func portgenFindApprovalRequest(msgs []types.Message) (types.ToolApprovalRequestContent, bool) {
	for _, msg := range msgs {
		if msg.Role != types.RoleAssistant {
			continue
		}
		for _, part := range msg.Content {
			switch p := part.(type) {
			case types.ToolApprovalRequestContent:
				return p, true
			case *types.ToolApprovalRequestContent:
				return *p, true
			}
		}
	}
	return types.ToolApprovalRequestContent{}, false
}

type portgenApprovalCall struct {
	status   func() types.ToolApprovalStatus
	executed *int
	messages []types.Message
}

func portgenCallApprovalTool(c portgenApprovalCall, secret []byte) (*GenerateTextResult, error) {
	step := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			step++
			if step == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"}}},
				}, nil
			}
			return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop}, nil
		},
	}
	messages := c.messages
	if messages == nil {
		messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}}}
	}
	return GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Tools: []types.Tool{{
			Name:       "tool1",
			Parameters: valueStringSchema(),
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				*c.executed++
				return "result1", nil
			},
		}},
		ToolApproval: map[string]interface{}{"tool1": types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
			return types.ToolApprovalResult{Status: c.status()}
		})},
		ExperimentalToolApprovalSecret: secret,
		StopWhen:                       []StopCondition{StepCountIs(3)},
		Messages:                       messages,
	})
}

// Port of generate-text.test.ts 'should sign the approval request when the
// tool approval status is %s' and 'should not sign the approval request when
// the tool call is denied automatically' (TS 63593a7f6a).
func TestPortgenGenerateTextSignsApprovalsExceptAutomaticDenials(t *testing.T) {
	secret := []byte("test-hmac-secret-do-not-use-in-production")
	for _, status := range []types.ToolApprovalStatus{types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied} {
		t.Run(string(status), func(t *testing.T) {
			executed := 0
			result, err := portgenCallApprovalTool(portgenApprovalCall{status: func() types.ToolApprovalStatus { return status }, executed: &executed}, secret)
			if err != nil {
				t.Fatal(err)
			}
			request, ok := portgenFindApprovalRequest(result.ResponseMessages)
			if !ok {
				t.Fatalf("no approval request in %+v", result.ResponseMessages)
			}
			if status == types.ToolApprovalStatusDenied {
				if request.Signature != "" || !request.IsAutomatic || request.ToolCallID != "call-1" {
					t.Fatalf("request = %+v, want an automatic unsigned request", request)
				}
				return
			}
			want, err := SignToolApproval(secret, request.ApprovalID, "call-1", "tool1", map[string]interface{}{"value": "test"})
			if err != nil {
				t.Fatal(err)
			}
			if request.Signature != want {
				t.Fatalf("signature = %q, want %q", request.Signature, want)
			}
		})
	}
}

// Port of 'should reject an approved response to a tool call that was denied
// automatically' and 'should continue a conversation that ends with an
// automatic denial'.
func TestPortgenGenerateTextAutomaticDenialReplay(t *testing.T) {
	secret := []byte("test-hmac-secret-do-not-use-in-production")
	user := types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}}
	t.Run("approved replay is rejected", func(t *testing.T) {
		executed := 0
		status := types.ToolApprovalStatusDenied
		result, err := portgenCallApprovalTool(portgenApprovalCall{status: func() types.ToolApprovalStatus { return status }, executed: &executed}, secret)
		if err != nil {
			t.Fatal(err)
		}
		request, _ := portgenFindApprovalRequest(result.ResponseMessages)
		// The client omits the history that caused the denial, so the policy
		// no longer denies the call when the approval is checked again.
		status = types.ToolApprovalStatusNotApplicable
		_, err = portgenCallApprovalTool(portgenApprovalCall{
			status:   func() types.ToolApprovalStatus { return status },
			executed: &executed,
			messages: []types.Message{
				user,
				result.ResponseMessages[0],
				{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: request.ApprovalID, Approved: true}}},
			},
		}, secret)
		if err == nil || !IsInvalidToolApprovalSignatureError(err) || !strings.Contains(err.Error(), "missing signature") {
			t.Fatalf("err = %v, want a missing signature error", err)
		}
		if executed != 0 {
			t.Fatalf("executed = %d, want 0", executed)
		}
	})
	t.Run("denied conversation continues", func(t *testing.T) {
		executed := 0
		denied := func() types.ToolApprovalStatus { return types.ToolApprovalStatusDenied }
		result, err := portgenCallApprovalTool(portgenApprovalCall{status: denied, executed: &executed}, secret)
		if err != nil {
			t.Fatal(err)
		}
		continued, err := portgenCallApprovalTool(portgenApprovalCall{
			status:   denied,
			executed: &executed,
			messages: append([]types.Message{user}, result.ResponseMessages[0], result.ResponseMessages[1]),
		}, secret)
		if err != nil {
			t.Fatal(err)
		}
		if continued.Text != "done" || executed != 0 {
			t.Fatalf("text=%q executed=%d", continued.Text, executed)
		}
	})
}

// Port of generate-text.test.ts 'messages array passed to the approval
// function' (TS 0b8cbfadb8): the approval function gets one messages slice
// per step, shared by every tool call of that step and never reused by
// another step, also when PrepareStep returns the same slice every time.
func portgenCollectApprovalMessages(t *testing.T, prepare func(context.Context, PrepareStepOptions) PrepareStepOptions) [][]types.Message {
	t.Helper()
	var collected [][]types.Message
	responses := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			step := responses
			responses++
			if step < 2 {
				calls := make([]types.ToolCall, 0, 2)
				for _, index := range []string{"1", "2"} {
					calls = append(calls, types.ToolCall{ID: "call-" + string(rune('0'+step)) + "-" + index, ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}})
				}
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: calls}, nil
			}
			return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Tools: []types.Tool{{
			Name:       "tool1",
			Parameters: valueStringSchema(),
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "result1", nil
			},
		}},
		ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
			collected = append(collected, o.Messages)
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
		}),
		PrepareStep: prepare,
		StopWhen:    []StopCondition{StepCountIs(3)},
		Prompt:      "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	return collected
}

func portgenSameSlice(a, b []types.Message) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

func TestPortgenApprovalMessagesSliceIsPerStep(t *testing.T) {
	t.Run("same for the calls of a step and new for every step", func(t *testing.T) {
		got := portgenCollectApprovalMessages(t, nil)
		if len(got) != 4 {
			t.Fatalf("approval calls = %d, want 4", len(got))
		}
		if !portgenSameSlice(got[0], got[1]) || !portgenSameSlice(got[2], got[3]) || portgenSameSlice(got[0], got[2]) {
			t.Fatalf("identity: 0=1 %v, 2=3 %v, 0=2 %v", portgenSameSlice(got[0], got[1]), portgenSameSlice(got[2], got[3]), portgenSameSlice(got[0], got[2]))
		}
	})
	t.Run("new for every step when PrepareStep returns the same slice", func(t *testing.T) {
		prepared := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "prepared-input"}}}}
		got := portgenCollectApprovalMessages(t, func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			step.Messages = prepared
			return step
		})
		if len(got) != 4 {
			t.Fatalf("approval calls = %d, want 4", len(got))
		}
		for i, msgs := range got {
			if !reflect.DeepEqual(msgs, prepared) {
				t.Errorf("call %d messages = %+v, want the prepared messages", i, msgs)
			}
			if portgenSameSlice(msgs, prepared) {
				t.Errorf("call %d got the slice PrepareStep returned", i)
			}
		}
		if !portgenSameSlice(got[0], got[1]) || !portgenSameSlice(got[2], got[3]) || portgenSameSlice(got[0], got[2]) {
			t.Fatalf("identity: 0=1 %v, 2=3 %v, 0=2 %v", portgenSameSlice(got[0], got[1]), portgenSameSlice(got[2], got[3]), portgenSameSlice(got[0], got[2]))
		}
	})
}
