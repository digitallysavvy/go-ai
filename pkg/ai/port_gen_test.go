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
