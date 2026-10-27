package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// preResolvedApproval is the approval decision StreamText made for a tool call
// while the step's stream was still running. TS execute-tools-from-stream
// resolves approvals on the tool-call part itself and enqueues the
// tool-approval-request / -response / tool-output-denied parts right after
// it, so executeTools must reuse this decision (and approval ID) instead of
// resolving again after the stream ends.
type preResolvedApproval struct {
	approval types.ToolApprovalResult
	id       string
	err      error
}

// eagerApprovalChunks resolves the approval for call and returns the chunks TS
// enqueues immediately after the tool-call part, plus the content parts to
// record in the same position. ok is false when the call takes no part in
// approval handling here (invalid call, unknown tool) or approval resolution
// failed (the error reaches the model as a tool error from executeTools).
func eagerApprovalChunks(
	ctx context.Context,
	call types.ToolCall,
	preRefinement *types.ToolCall,
	tools []types.Tool,
	messages []types.Message,
	runtimeCtx interface{},
	toolsCtx map[string]interface{},
	toolApproval interface{},
	secret []byte,
) (pre preResolvedApproval, chunks []provider.StreamChunk, parts []types.ContentPart, handled bool) {
	if call.Invalid {
		return pre, nil, nil, false
	}
	tool := findToolForCall(call, tools)
	if tool == nil {
		return pre, nil, nil, false
	}
	if toolsCtx == nil {
		toolsCtx = map[string]interface{}{}
	}
	approval, err := resolveToolApproval(ctx, call, tools, messages, runtimeCtx, toolsCtx, toolApproval)
	pre = preResolvedApproval{approval: approval, err: err}
	switch approval.Status {
	case types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied:
		pre.id = newCallID()
	}
	if err != nil {
		return pre, nil, nil, true
	}
	if pre.id == "" {
		return pre, nil, nil, true
	}
	skeleton := []types.ToolResult{{
		ToolCallID:       call.ID,
		ToolName:         call.ToolName,
		Title:            call.Title,
		Input:            call.Arguments,
		ApprovalStatus:   approval.Status,
		ApprovalID:       pre.id,
		ApprovalReason:   approval.Reason,
		ProviderExecuted: tool.ProviderExecuted,
		// TS approval parts carry toolCall.providerExecuted.
		ProviderExecutedSet: call.ProviderExecutedSet,
		ProviderMetadata:    mergeProviderMetadataMaps(call.ProviderMetadata, tool.ProviderMetadata),
		ToolMetadata:        call.ToolMetadata,
		Dynamic:             call.Dynamic,
	}}
	if approval.Status == types.ToolApprovalStatusDenied {
		reason := ""
		if approval.Reason != nil {
			reason = *approval.Reason
		}
		skeleton[0].Result = types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: reason}
	}
	attachToolApprovalSignatures(skeleton, secret)
	tr := skeleton[0]

	request := toolApprovalRequestFromToolResult(tr)
	if preRefinement != nil {
		if input, ok := inputSchemaInputs([]types.ToolCall{*preRefinement}, []types.ToolCall{call})[call.ID]; ok {
			request.InputSchemaInput = input
		}
	}
	chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &request})
	parts = append(parts, request)
	if approval.Status == types.ToolApprovalStatusUserApproval {
		return pre, chunks, parts, true
	}
	response := toolApprovalResponseFromToolResult(tr)
	chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolApprovalResponse, ToolApprovalResponse: &response})
	parts = append(parts, response)
	if approval.Status == types.ToolApprovalStatusDenied {
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolOutputDenied, ToolResult: &tr})
	}
	return pre, chunks, parts, true
}

// withoutInlineApprovalParts drops the approval request/response parts whose
// tool call already had them recorded in stream position.
func withoutInlineApprovalParts(parts []types.ContentPart, inline map[string]preResolvedApproval) []types.ContentPart {
	if len(inline) == 0 {
		return parts
	}
	out := parts[:0:0]
	for _, part := range parts {
		switch p := part.(type) {
		case types.ToolApprovalRequestContent:
			if pre, ok := inline[normalizeApprovalRequest(p).ToolCallID]; ok && pre.err == nil && pre.id != "" {
				continue
			}
		case types.ToolApprovalResponseContent:
			id := p.ToolCallID
			if id == "" {
				id = p.ToolCall.ID
			}
			if pre, ok := inline[id]; ok && pre.err == nil && pre.id != "" {
				continue
			}
		}
		out = append(out, part)
	}
	return out
}
