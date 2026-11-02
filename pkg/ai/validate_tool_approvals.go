package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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
	// TS: `Invalid input for tool ${toolName}: ${getErrorMessage(cause)}`,
	// the cause's toString ("Error: <message>" for a plain error,
	// "AI_TypeValidationError: ..." for a schema failure).
	return fmt.Sprintf("Invalid input for tool %s: %s", e.ToolName, providerutils.ToolErrorText(e.Cause))
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
			// TS reports "missing signature" for an absent (null/undefined)
			// signature. Go's plain string field cannot tell absent from an
			// explicit empty string, so "" is treated as absent: a stripped
			// signature is the realistic case, and an explicit empty string
			// is rejected either way (only the reason text differs).
			if request.Signature == "" {
				return ValidateApprovedToolApprovalsResult{}, &InvalidToolApprovalSignatureError{
					ApprovalID: request.ApprovalID,
					ToolCallID: call.ID,
					Reason:     "missing signature",
				}
			}
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
			// TS revalidates every executable tool (inputSchema is required
			// there): a tool without Parameters accepts any input, but the
			// refiner and the approved-input comparison still run. A tool's
			// own ValidateInput replaces the schema check (TS
			// safeValidateTypes runs the jsonSchema validate function).
			if validationErr := revalidateApprovedInput(ctx, approval, tool, toolInputValidator(tool), opts); validationErr != nil {
				inputJSON, _ := jsonstr.Marshal(call.Arguments)
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
// when the tool has no schema that can validate the decoded input. It is the
// validator the call was parsed with (toolCallInputValidator), so an approved
// input or a UI message part is revalidated the way the call was validated:
// a struct-tag schema cannot validate a decoded map and is skipped there too.
func toolInputValidator(tool *types.Tool) schema.Validator {
	return toolCallInputValidator(tool)
}

func revalidateApprovedInput(ctx context.Context, approval CollectedToolApproval, tool *types.Tool, validator schema.Validator, opts ValidateApprovedToolApprovalsOptions) error {
	call := approval.ToolCall
	approvedInput := call.Arguments
	if approvedInput == nil {
		approvedInput = map[string]interface{}{}
	}
	var value interface{} = approvedInput
	// TS hasOwnProperty(approvalRequest, 'inputSchemaInput'): a null schema
	// input is validated as null.
	if approval.ApprovalRequest.InputSchemaInput != nil || approval.ApprovalRequest.InputSchemaInputSet {
		value = approval.ApprovalRequest.InputSchemaInput
	}
	// TS's safeValidateTypes performs a zod/standard-schema parse, which
	// fills .default() values as part of parsing and returns that defaulted
	// value as validation.value -- the value that then feeds the refiner
	// and the approved-input equality check below. Apply defaults before
	// validating (not after), and use the defaulted value throughout, to
	// match (see validate-tool-approvals.ts's use of `validation.value`).
	if tool.ValidateInput != nil {
		// TS jsonSchema(schema, {validate}): the tool's validator decides, its
		// value feeds the refiner and the equality check, and a failure is a
		// TypeValidationError (as when the call was parsed).
		validated, err := tool.ValidateInput(value)
		if err != nil {
			return approvalInputTypeValidationError(call, value, err)
		}
		value = validated
	} else if validator != nil {
		// TS safeValidateTypes: a TypeValidationError of the value before
		// defaults.
		original := value
		value = applyDefaultsForValidator(value, validator)
		if err := validator.Validate(value); err != nil {
			return approvalInputTypeValidationError(call, original, err)
		}
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
		return errors.New("Approved tool input does not match the validated schema output.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}

// approvalInputTypeValidationError is the TypeValidationError (TS
// safeValidateTypes) of an approved input value that fails revalidation.
func approvalInputTypeValidationError(call types.ToolCall, value interface{}, err error) error {
	args, _ := value.(map[string]interface{})
	if args == nil {
		args = map[string]interface{}{}
	}
	typeCall := call
	typeCall.RawArguments = ""
	if _, ok := value.(map[string]interface{}); !ok {
		if raw, mErr := jsonstr.Marshal(value); mErr == nil {
			typeCall.RawArguments = string(raw)
		}
	}
	return newToolInputTypeValidationError(typeCall, args, err)
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

// ErrorName is the TS error name, used where TS shows error.toString().
func (e *InvalidToolInputError) ErrorName() string { return "AI_InvalidToolInputError" }
