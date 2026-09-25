package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// toolApprovalResumeOptions carries the call settings needed to resume tool
// approvals found at the end of the input messages.
type toolApprovalResumeOptions struct {
	messages       []types.Message
	tools          []types.Tool
	toolApproval   types.ToolApprovalConfig
	toolsContext   map[string]interface{}
	runtimeContext interface{}
	secret         []byte
	refine         map[string]ToolInputRefiner
	usage          *types.Usage
	callbacks      toolCallEventCallbacks
	// streaming selects the streamText variant: denied provider-executed
	// approvals are not answered with execution-denied tool results (they are
	// forwarded to the provider as approval responses instead), and stream
	// chunks are produced for every outcome.
	streaming bool
}

// toolApprovalResumeResult is the outcome of resuming approvals before the
// first model call.
type toolApprovalResumeResult struct {
	// responseMessages is the initial response (a single tool message) that
	// is prepended to the call's response messages and appended to the
	// messages sent to the model.
	responseMessages []types.Message
	// chunks are the full-stream parts for the resumed tools (streamText).
	chunks []provider.StreamChunk
}

// resumeToolApprovals ports the pre-step approval handling of TS
// generateText/streamText: collect approval responses from the last tool
// message, re-validate approved ones (signature, input schema, approval
// policy), execute approved local tools, report invalid inputs as tool
// errors, and synthesize execution-denied results for denials.
func resumeToolApprovals(ctx context.Context, opts toolApprovalResumeOptions) (toolApprovalResumeResult, error) {
	var out toolApprovalResumeResult

	collected, err := CollectToolApprovals(opts.messages)
	if err != nil {
		return out, err
	}
	if len(collected.ApprovedToolApprovals) == 0 && len(collected.DeniedToolApprovals) == 0 {
		return out, nil
	}

	localApproved := make([]CollectedToolApproval, 0, len(collected.ApprovedToolApprovals))
	for _, approval := range collected.ApprovedToolApprovals {
		if !approval.ToolCall.ProviderExecuted {
			localApproved = append(localApproved, approval)
		}
	}
	validated, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
		ApprovedToolApprovals: localApproved,
		Tools:                 opts.tools,
		ToolApproval:          opts.toolApproval,
		Messages:              opts.messages,
		ToolsContext:          opts.toolsContext,
		RuntimeContext:        opts.runtimeContext,
		ToolApprovalSecret:    opts.secret,
		RefineToolInput:       opts.refine,
	})
	if err != nil {
		return out, err
	}

	var denied []CollectedToolApproval
	var deniedProviderExecuted []CollectedToolApproval
	for _, approval := range collected.DeniedToolApprovals {
		if opts.streaming && approval.ToolCall.ProviderExecuted {
			deniedProviderExecuted = append(deniedProviderExecuted, approval)
			continue
		}
		denied = append(denied, approval)
	}
	denied = append(denied, validated.DeniedToolApprovals...)
	var deniedWithoutResults []CollectedToolApproval
	for _, approval := range denied {
		if approval.ExistingToolResult == nil {
			deniedWithoutResults = append(deniedWithoutResults, approval)
		}
	}

	if opts.streaming {
		for _, approval := range append(append([]CollectedToolApproval(nil), denied...), deniedProviderExecuted...) {
			call := approval.ToolCall
			out.chunks = append(out.chunks, provider.StreamChunk{
				Type: provider.ChunkTypeToolOutputDenied,
				ToolResult: &types.ToolResult{
					ToolCallID: call.ID,
					ToolName:   call.ToolName,
					Input:      call.Arguments,
					Dynamic:    call.Dynamic,
				},
			})
		}
	}

	var invalidResults []types.ToolResult
	for _, invalid := range validated.InvalidToolApprovals {
		call := invalid.ToolCall
		invalidResults = append(invalidResults, types.ToolResult{
			ToolCallID:   call.ID,
			ToolName:     call.ToolName,
			Title:        call.Title,
			Input:        call.Arguments,
			Error:        invalid.Error,
			ToolMetadata: call.ToolMetadata,
			Dynamic:      call.Dynamic,
		})
	}
	if opts.streaming {
		for i := range invalidResults {
			out.chunks = append(out.chunks, provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &invalidResults[i]})
		}
	}

	// Execute approved local tools. Tools that are missing or have no
	// execute function produce no output (TS executeToolCall returns
	// undefined for them).
	var executable []types.ToolCall
	for _, approval := range validated.ApprovedToolApprovals {
		tool := findToolForCall(approval.ToolCall, opts.tools)
		if tool == nil || tool.Execute == nil || tool.ProviderExecuted {
			continue
		}
		executable = append(executable, approval.ToolCall)
	}
	var executed []types.ToolResult
	if len(executable) > 0 {
		// Approval was already resolved (and re-validated) above, so the
		// executor must not request approval again.
		noApproval := types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		})
		executed, err = executeTools(ctx, executable, opts.tools, opts.runtimeContext, opts.toolsContext, noApproval, opts.usage, opts.callbacks)
		if err != nil {
			return out, err
		}
		if opts.streaming {
			for i := range executed {
				out.chunks = append(out.chunks, provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &executed[i]})
			}
		}
	}

	if len(executed) == 0 && len(invalidResults) == 0 && len(deniedWithoutResults) == 0 {
		return out, nil
	}

	results := make([]types.ToolResult, 0, len(executed)+len(invalidResults)+len(deniedWithoutResults))
	results = append(results, executed...)
	results = append(results, invalidResults...)
	for _, approval := range deniedWithoutResults {
		call := approval.ToolCall
		output := types.ToolResultOutput{
			Type:   types.ToolResultOutputExecutionDenied,
			Reason: approval.ApprovalResponse.Reason,
		}
		if call.ProviderExecuted {
			// Provider-executed tools carry the approval ID so the provider
			// can correlate the denial.
			output.ProviderOptions = map[string]interface{}{
				"openai": map[string]interface{}{"approvalId": approval.ApprovalResponse.ApprovalID},
			}
		}
		results = append(results, types.ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.ToolName,
			Input:      call.Arguments,
			Result:     output,
		})
	}
	out.responseMessages = providerutils.ConvertToResponseMessages(nil, nil, results)
	return out, nil
}

// messagesForModel prepares model messages for a language model call the way
// TS convertToLanguageModelPrompt does for approval parts: tool approval
// requests are removed from assistant messages, tool approval responses are
// removed from tool messages unless they belong to provider-executed tools,
// consecutive tool messages are combined, and empty tool messages dropped.
func messagesForModel(messages []types.Message) []types.Message {
	hasApprovalParts := false
	for _, message := range messages {
		for _, part := range message.Content {
			switch part.(type) {
			case types.ToolApprovalRequestContent, *types.ToolApprovalRequestContent,
				types.ToolApprovalResponseContent, *types.ToolApprovalResponseContent:
				hasApprovalParts = true
			}
		}
	}
	if !hasApprovalParts {
		return messages
	}

	// Approval responses without the providerExecuted flag are still kept
	// when their request references a provider-executed tool call, so
	// hand-built Go histories keep working with provider-side approvals.
	providerExecutedCalls := map[string]bool{}
	approvalToolCall := map[string]string{}
	for _, message := range messages {
		if message.Role != types.RoleAssistant {
			continue
		}
		for _, call := range message.ToolCalls {
			if call.ProviderExecuted {
				providerExecutedCalls[call.ID] = true
			}
		}
		for _, part := range message.Content {
			switch p := part.(type) {
			case types.ToolCallContent:
				if p.ProviderExecuted {
					providerExecutedCalls[p.ToolCallID] = true
				}
			case *types.ToolCallContent:
				if p != nil && p.ProviderExecuted {
					providerExecutedCalls[p.ToolCallID] = true
				}
			case types.ToolApprovalRequestContent:
				approvalToolCall[p.ApprovalID] = normalizeApprovalRequest(p).ToolCallID
				if p.ToolCall.ProviderExecuted {
					providerExecutedCalls[p.ToolCall.ID] = true
				}
			case *types.ToolApprovalRequestContent:
				if p != nil {
					approvalToolCall[p.ApprovalID] = normalizeApprovalRequest(*p).ToolCallID
					if p.ToolCall.ProviderExecuted {
						providerExecutedCalls[p.ToolCall.ID] = true
					}
				}
			}
		}
	}
	keepResponse := func(response types.ToolApprovalResponseContent) (types.ToolApprovalResponseContent, bool) {
		if response.ProviderExecuted {
			return response, true
		}
		if providerExecutedCalls[approvalToolCall[response.ApprovalID]] || response.ToolCall.ProviderExecuted {
			response.ProviderExecuted = true
			return response, true
		}
		return response, false
	}

	out := make([]types.Message, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case types.RoleAssistant:
			filtered := make([]types.ContentPart, 0, len(message.Content))
			for _, part := range message.Content {
				switch part.(type) {
				case types.ToolApprovalRequestContent, *types.ToolApprovalRequestContent:
					continue
				}
				filtered = append(filtered, part)
			}
			message.Content = filtered
			out = append(out, message)
		case types.RoleTool:
			filtered := make([]types.ContentPart, 0, len(message.Content))
			for _, part := range message.Content {
				switch p := part.(type) {
				case types.ToolApprovalResponseContent:
					if kept, ok := keepResponse(p); ok {
						filtered = append(filtered, kept)
					}
					continue
				case *types.ToolApprovalResponseContent:
					if p != nil {
						if kept, ok := keepResponse(*p); ok {
							filtered = append(filtered, kept)
						}
					}
					continue
				}
				filtered = append(filtered, part)
			}
			message.Content = filtered
			if n := len(out); n > 0 && out[n-1].Role == types.RoleTool && len(out[n-1].ProviderOptions) == 0 && len(message.ProviderOptions) == 0 {
				merged := append(append([]types.ContentPart(nil), out[n-1].Content...), message.Content...)
				out[n-1].Content = merged
				continue
			}
			out = append(out, message)
		default:
			out = append(out, message)
		}
	}

	// Drop tool messages that became empty.
	final := out[:0]
	for _, message := range out {
		if message.Role == types.RoleTool && len(message.Content) == 0 {
			continue
		}
		final = append(final, message)
	}
	return final
}

// inputSchemaInputs maps tool call IDs to their pre-refinement input for calls
// whose input refinement changed the input. The approval request carries it
// as inputSchemaInput so revalidation can re-run the refinement (TS
// to-response-messages / to-ui-message-chunk).
func inputSchemaInputs(before, after []types.ToolCall) map[string]interface{} {
	if len(before) == 0 {
		return nil
	}
	original := make(map[string]map[string]interface{}, len(before))
	for _, call := range before {
		original[call.ID] = call.Arguments
	}
	var out map[string]interface{}
	for _, call := range after {
		pre, ok := original[call.ID]
		if !ok || isDeepEqualJSONData(pre, call.Arguments) {
			continue
		}
		if out == nil {
			out = map[string]interface{}{}
		}
		out[call.ID] = pre
	}
	return out
}

// attachInputSchemaInputs sets InputSchemaInput on approval request parts.
func attachInputSchemaInputs(parts []types.ContentPart, inputs map[string]interface{}) []types.ContentPart {
	if len(inputs) == 0 {
		return parts
	}
	for i, part := range parts {
		request, ok := part.(types.ToolApprovalRequestContent)
		if !ok {
			continue
		}
		if input, ok := inputs[normalizeApprovalRequest(request).ToolCallID]; ok {
			request.InputSchemaInput = input
			parts[i] = request
		}
	}
	return parts
}
