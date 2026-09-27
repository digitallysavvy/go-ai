package codemode

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// normalizeApprovalResolution converts a resolution value recorded for an
// approval-kind PendingInterruption back into an ApprovalDecision. It
// accepts an ApprovalDecision/*ApprovalDecision (the common case: a value
// this package itself produced) or a map[string]interface{} with an
// "approved" bool and optional "reason" string (a value that round-tripped
// through JSON, e.g. after persistence). Mirrors TypeScript's
// normalizeApprovalResolution (code-mode/src/approval.ts).
func normalizeApprovalResolution(resolution interface{}) (ApprovalDecision, error) {
	switch v := resolution.(type) {
	case ApprovalDecision:
		return v, nil
	case *ApprovalDecision:
		if v == nil {
			break
		}
		return *v, nil
	case map[string]interface{}:
		approved, ok := v["approved"].(bool)
		if !ok {
			break
		}
		reason, _ := v["reason"].(string)
		return ApprovalDecision{Approved: approved, Reason: reason}, nil
	}
	return ApprovalDecision{}, NewProtocolError(
		"Code mode approval resolution must be a boolean approval decision.",
		map[string]interface{}{"resolution": resolution},
	)
}

// IsCodeModeApprovalInterrupt reports whether value is a valid Interrupt
// (see IsCodeModeInterrupt) whose Payload.Kind() == ToolApprovalKind.
// Mirrors TypeScript's experimental_isCodeModeApprovalInterrupt.
func IsCodeModeApprovalInterrupt(value interface{}, security ...ContinuationSecurityOptions) bool {
	interrupt, ok := asInterrupt(value, resolveSecurityArg(security))
	return ok && interrupt.Payload.Kind() == ToolApprovalKind
}

// ContinueCodeModeApproval resolves a pending ApprovalInterrupt with an
// approve/deny decision and resumes execution, forcing Options.Approval.Mode
// to ApprovalModeInterrupt for the resumed call (so any further approval
// required downstream also interrupts, rather than blocking on a callback).
// Mirrors TypeScript's experimental_continueCodeModeApproval.
func ContinueCodeModeApproval(ctx context.Context, interrupt ApprovalInterrupt, response ApprovalResponse, tools ToolSet, options *Options, toolExecutionOptions *types.ToolExecutionOptions) (interface{}, error) {
	if response.ApprovalID != interrupt.InterruptID {
		return nil, NewProtocolError(
			"Approval response "+response.ApprovalID+" does not match pending code-mode approval "+interrupt.InterruptID+".",
			nil,
		)
	}

	resolvedOptions := Options{}
	if options != nil {
		resolvedOptions = *options
	}
	approvalOptions := ApprovalOptions{}
	if resolvedOptions.Approval != nil {
		approvalOptions = *resolvedOptions.Approval
	}
	approvalOptions.Mode = ApprovalModeInterrupt
	resolvedOptions.Approval = &approvalOptions

	decision := ApprovalDecision{Approved: response.Approved, Reason: response.Reason}
	return ContinueCodeModeInterrupt(ctx, interrupt, decision, tools, &resolvedOptions, toolExecutionOptions)
}

// ToCodeModeApprovalMessages renders interrupt as the assistant tool-call +
// tool-approval-request message pair a conversation would show while the
// approval is pending. Mirrors TypeScript's
// experimental_toCodeModeApprovalMessages.
func ToCodeModeApprovalMessages(interrupt ApprovalInterrupt) []types.Message {
	return []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{
					ToolCallID: interrupt.ToolCallID,
					ToolName:   interrupt.ToolName,
					Arguments:  toArgumentsMap(interrupt.Input),
				},
				types.ToolApprovalRequestContent{
					ApprovalID: interrupt.InterruptID,
					ToolCallID: interrupt.ToolCallID,
				},
			},
		},
	}
}

func toArgumentsMap(input interface{}) map[string]interface{} {
	m, _ := input.(map[string]interface{})
	return m
}

// GetCodeModeApprovalResponse scans messages (most recent first) for a
// tool-role message containing a tool-approval-response content part whose
// ApprovalID matches interrupt.InterruptID, returning it as an
// ApprovalResponse. Returns nil if none is found. Mirrors TypeScript's
// experimental_getCodeModeApprovalResponse.
func GetCodeModeApprovalResponse(messages []types.Message, interrupt ApprovalInterrupt) *ApprovalResponse {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != types.RoleTool {
			continue
		}
		for _, part := range message.Content {
			response, ok := part.(types.ToolApprovalResponseContent)
			if !ok || response.ApprovalID != interrupt.InterruptID {
				continue
			}
			return &ApprovalResponse{
				ApprovalID: response.ApprovalID,
				Approved:   response.Approved,
				Reason:     response.Reason,
			}
		}
	}
	return nil
}
