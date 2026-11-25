package ai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
