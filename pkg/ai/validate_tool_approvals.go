package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// InvalidToolInputError reports tool input that does not satisfy the tool's
// input schema. Mirrors TS InvalidToolInputError.
type InvalidToolInputError struct {
	ToolName  string
	ToolInput string
	Cause     error
}

func (e *InvalidToolInputError) Error() string {
	cause := "unknown error"
	if e.Cause != nil {
		cause = e.Cause.Error()
	}
	return fmt.Sprintf("Invalid input for tool %s: %s", e.ToolName, cause)
}

func (e *InvalidToolInputError) Unwrap() error { return e.Cause }

// IsInvalidToolInputError reports whether err is an InvalidToolInputError.
func IsInvalidToolInputError(err error) bool {
	var target *InvalidToolInputError
	return errors.As(err, &target)
}

// InvalidToolApproval is an approved tool approval whose input failed
// revalidation. It is reported to the model as a tool error and never
// executed.
type InvalidToolApproval struct {
	CollectedToolApproval
	Error *InvalidToolInputError
}

// ValidateApprovedToolApprovalsOptions configures ValidateApprovedToolApprovals.
type ValidateApprovedToolApprovalsOptions struct {
	ApprovedToolApprovals []CollectedToolApproval
	Tools                 []types.Tool
	ToolApproval          types.ToolApprovalConfig
	Messages              []types.Message
	ToolsContext          map[string]interface{}
	RuntimeContext        interface{}
	// ToolApprovalSecret enables HMAC signature verification when non-nil.
	ToolApprovalSecret []byte
	RefineToolInput    map[string]ToolInputRefiner
}

// ValidateApprovedToolApprovalsResult splits approvals into those that may be
// executed, those the current approval policy denies, and those whose input
// is no longer valid.
type ValidateApprovedToolApprovalsResult struct {
	ApprovedToolApprovals []CollectedToolApproval
	DeniedToolApprovals   []CollectedToolApproval
	InvalidToolApprovals  []InvalidToolApproval
}

// ValidateApprovedToolApprovals re-validates approved tool approvals that were
// reconstructed from client-supplied message history before they are
// executed: it verifies the HMAC signature (when a secret is configured),
// re-validates the input against the tool's current input schema and input
// refinement, and re-resolves the server-side approval policy. Mirrors TS
// validateApprovedToolApprovals.
//
// A missing or invalid signature returns *InvalidToolApprovalSignatureError.
func ValidateApprovedToolApprovals(ctx context.Context, opts ValidateApprovedToolApprovalsOptions) (ValidateApprovedToolApprovalsResult, error) {
	var result ValidateApprovedToolApprovalsResult
	toolsContext := opts.ToolsContext
	if toolsContext == nil {
		toolsContext = map[string]interface{}{}
	}

	for _, approval := range opts.ApprovedToolApprovals {
		request := approval.ApprovalRequest
		call := approval.ToolCall
		tool := findToolForCall(call, opts.Tools)

		if opts.ToolApprovalSecret != nil {
			// TS only special-cases `approvalRequest.signature == null`
			// (absent), which JS can distinguish from an explicit empty
			// string. Go's Signature field is a plain string, so an absent
			// signature and an empty one are the same zero value and cannot
			// be told apart; treating "" as "missing" would also disagree
			// with TS's reported reason for an explicit empty string, which
			// falls through to verification and fails as "invalid
			// signature". Always verifying (rather than special-casing "")
			// matches that observable behavior for every input Go can
			// represent, with no loss of safety: either way the call is
			// rejected.
			valid, err := VerifyToolApprovalSignature(opts.ToolApprovalSecret, request.Signature, request.ApprovalID, call.ID, call.ToolName, approvalSignatureInput(call))
			if err != nil || !valid {
				return ValidateApprovedToolApprovalsResult{}, &InvalidToolApprovalSignatureError{
					ApprovalID: request.ApprovalID,
					ToolCallID: call.ID,
					Reason:     "invalid signature",
				}
			}
		}

		if tool != nil && tool.Execute != nil {
			if validator := toolInputValidator(tool); validator != nil {
				if validationErr := revalidateApprovedInput(ctx, approval, tool, validator, opts); validationErr != nil {
					inputJSON, _ := json.Marshal(call.Arguments)
					result.InvalidToolApprovals = append(result.InvalidToolApprovals, InvalidToolApproval{
						CollectedToolApproval: approval,
						Error: &InvalidToolInputError{
							ToolName:  call.ToolName,
							ToolInput: string(inputJSON),
							Cause:     validationErr,
						},
					})
					continue
				}
			}
		}

		status, err := resolveToolApproval(ctx, call, opts.Tools, opts.Messages, opts.RuntimeContext, toolsContext, opts.ToolApproval)
		if err != nil {
			return ValidateApprovedToolApprovalsResult{}, err
		}
		if status.Status == types.ToolApprovalStatusDenied {
			denied := approval
			denied.ApprovalResponse.Approved = false
			if status.Reason != nil {
				denied.ApprovalResponse.Reason = *status.Reason
			}
			result.DeniedToolApprovals = append(result.DeniedToolApprovals, denied)
			continue
		}
		result.ApprovedToolApprovals = append(result.ApprovedToolApprovals, approval)
	}
	return result, nil
}

// approvalSignatureInput is the input value that was signed when the approval
// request was issued (the tool call arguments).
func approvalSignatureInput(call types.ToolCall) interface{} {
	return call.Arguments
}

// toolInputValidator returns the validator for a tool's input schema, or nil
// when the tool has no validatable schema.
func toolInputValidator(tool *types.Tool) schema.Validator {
	switch s := tool.Parameters.(type) {
	case schema.Schema:
		if s == nil {
			return nil
		}
		return s.Validator()
	case map[string]interface{}:
		if s == nil {
			return nil
		}
		return schema.NewSimpleJSONSchema(s).Validator()
	default:
		return nil
	}
}

func revalidateApprovedInput(ctx context.Context, approval CollectedToolApproval, tool *types.Tool, validator schema.Validator, opts ValidateApprovedToolApprovalsOptions) error {
	call := approval.ToolCall
	approvedInput := call.Arguments
	if approvedInput == nil {
		approvedInput = map[string]interface{}{}
	}
	var value interface{} = approvedInput
	if approval.ApprovalRequest.InputSchemaInput != nil {
		value = approval.ApprovalRequest.InputSchemaInput
	}
	if err := validator.Validate(value); err != nil {
		return err
	}

	revalidated := value
	if refiner := opts.RefineToolInput[call.ToolName]; refiner != nil {
		args, err := toArgumentsMap(value)
		if err != nil {
			return err
		}
		refineCall := call
		refineCall.Arguments = args
		refined, err := refiner(ctx, ToolInputRefinementOptions{
			ToolCall:       refineCall,
			Tool:           tool,
			RuntimeContext: opts.RuntimeContext,
			ToolsContext:   opts.ToolsContext,
		})
		if err != nil {
			return err
		}
		if refined != nil {
			revalidated = refined
		}
	}

	// Revalidation must never change the operation that was approved.
	if !isDeepEqualJSONData(revalidated, approvedInput) {
		return errors.New("Approved tool input does not match the validated schema output.")
	}
	return nil
}

func toArgumentsMap(value interface{}) (map[string]interface{}, error) {
	if m, ok := value.(map[string]interface{}); ok {
		return m, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// isDeepEqualJSONData compares two JSON-like values structurally (key order
// and Go numeric types do not matter). Mirrors TS isDeepEqualData for parsed
// JSON data.
func isDeepEqualJSONData(a, b interface{}) bool {
	ca, errA := canonicalJSON(a)
	cb, errB := canonicalJSON(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ca, cb)
}
