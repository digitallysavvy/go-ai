package ai

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// IDGenerator returns a stable identifier for tests and integrations that need
// deterministic response or call IDs.
type IDGenerator func() string

// InternalOptions mirrors the TypeScript SDK _internal option. It is intended
// for tests and framework integrations and may change without notice.
type InternalOptions struct {
	GenerateID     IDGenerator
	GenerateCallID IDGenerator

	// ContinueOnMissingProviderResult makes the step loop continue after a
	// provider-executed tool call that got no result in its response. The
	// loop warns and answers the call with an empty text result, as TS
	// WorkflowAgent does (resolveProviderToolResult), instead of ending the
	// run. Calls to deferred-result tools are left pending as usual. Used by
	// WorkflowAgent; plain GenerateText/StreamText stop the loop like TS
	// generateText/streamText.
	ContinueOnMissingProviderResult bool

	// StopOnStreamError makes a step whose stream carried an error part end
	// the run without executing or answering that step's tool calls, like TS
	// WorkflowAgent's hasTerminalError (the loop is done; the error is
	// reported once). Plain StreamText keeps executing tools after an error
	// part, as TS streamText does. Used by WorkflowAgent.
	StopOnStreamError bool
}

// providerResultFinishAllowed reports whether TS WorkflowAgent processes the
// tool calls of a step with this finish reason (tool-calls or stop); other
// finish reasons end the run without answering missing provider results.
func providerResultFinishAllowed(reason types.FinishReason) bool {
	return reason == types.FinishReasonToolCalls || reason == types.FinishReasonStop
}

// missingProviderResultCalls returns the valid provider-executed calls of a step
// that got no result and are not deferred (TS resolveProviderToolResult:
// a result-less call is only left alone for deferred-result provider tools).
func missingProviderResultCalls(calls []types.ToolCall, toolsByName map[string]*types.Tool, hasResult func(id string) bool) []types.ToolCall {
	var missing []types.ToolCall
	for _, call := range calls {
		if call.Invalid || hasResult(call.ID) {
			continue
		}
		tool := toolsByName[call.ToolName]
		if !call.ProviderExecuted && (tool == nil || !tool.ProviderExecuted) {
			continue
		}
		if tool != nil && tool.ProviderExecuted && tool.SupportsDeferredResults {
			continue
		}
		missing = append(missing, call)
	}
	return missing
}

// emptyProviderResult is the synthetic result TS WorkflowAgent sends for a
// provider-executed call the provider never answered: an empty text output.
func emptyProviderResult(call types.ToolCall, provider, model string) types.ToolResult {
	LogWarnings(LogWarningsOptions{
		Warnings: []types.Warning{{
			Type: "other",
			Message: fmt.Sprintf("[WorkflowAgent] Provider-executed tool %q (%s) did not receive a result from the stream. This may indicate a provider issue.",
				call.ToolName, call.ID),
		}},
		Provider: provider,
		Model:    model,
	})
	return types.ToolResult{
		ToolCallID:       call.ID,
		ToolName:         call.ToolName,
		Title:            call.Title,
		Input:            call.Arguments,
		Result:           "",
		ModelOutput:      &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: ""},
		ProviderExecuted: true,
		ProviderMetadata: call.ProviderMetadata,
		ToolMetadata:     call.ToolMetadata,
		Dynamic:          call.Dynamic,
	}
}

func internalGenerateID(internal *InternalOptions) IDGenerator {
	if internal != nil && internal.GenerateID != nil {
		return internal.GenerateID
	}
	return newCallID
}

func internalGenerateCallID(internal *InternalOptions) IDGenerator {
	if internal != nil && internal.GenerateCallID != nil {
		return internal.GenerateCallID
	}
	return newCallID
}

// EmptyProviderResults answers the provider-executed calls of a step that got
// no result in content with the empty text result TS WorkflowAgent sends
// (warning for each). It is the building block of
// InternalOptions.ContinueOnMissingProviderResult for loops outside this
// package.
func EmptyProviderResults(calls []types.ToolCall, tools []types.Tool, content []types.ContentPart, finishReason types.FinishReason, provider, model string) []types.ToolResult {
	if !providerResultFinishAllowed(finishReason) {
		return nil
	}
	byName := make(map[string]*types.Tool, len(tools))
	for i := range tools {
		byName[tools[i].Name] = &tools[i]
	}
	answered := func(id string) bool { return contentAnswersProviderCall(content, id) }
	var results []types.ToolResult
	for _, call := range missingProviderResultCalls(calls, byName, answered) {
		results = append(results, emptyProviderResult(call, provider, model))
	}
	return results
}

// contentAnswersProviderCall reports whether step content already settles a
// provider-executed call: it has a result or error, or the provider asked for
// approval of it. TS WorkflowAgent pauses for a provider tool-approval-request
// (providerApprovalRequests) and never answers that call with an empty result.
func contentAnswersProviderCall(content []types.ContentPart, id string) bool {
	for _, part := range content {
		switch v := part.(type) {
		case types.ToolResultContent:
			if v.ToolCallID == id {
				return true
			}
		case types.ToolErrorContent:
			if v.ToolCallID == id {
				return true
			}
		case types.ToolApprovalRequestContent:
			if v.ToolCallID == id || v.ToolCall.ID == id {
				return true
			}
		case *types.ToolApprovalRequestContent:
			if v != nil && (v.ToolCallID == id || v.ToolCall.ID == id) {
				return true
			}
		}
	}
	return false
}

// workflowBlockedToolStep reports whether TS WorkflowAgent leaves a step's
// tool calls untouched: modelCallIterator processes tools only when the finish
// reason is stop or tool-calls (or length with provider-executed results). On
// any other step the run just ends with the assistant message, which carries
// neither the invalid calls nor any tool-error message.
func workflowBlockedToolStep(internal *InternalOptions, reason types.FinishReason, content []types.ContentPart) bool {
	if internal == nil || !internal.ContinueOnMissingProviderResult || providerResultFinishAllowed(reason) {
		return false
	}
	if reason == types.FinishReasonLength {
		for _, part := range content {
			switch p := part.(type) {
			case types.ToolResultContent:
				if p.ProviderExecuted {
					return false
				}
			case *types.ToolResultContent:
				if p != nil && p.ProviderExecuted {
					return false
				}
			}
		}
	}
	return true
}

// WorkflowBlockedToolStep is workflowBlockedToolStep for loops outside this
// package (pkg/agent).
func WorkflowBlockedToolStep(internal *InternalOptions, reason types.FinishReason, content []types.ContentPart) bool {
	return workflowBlockedToolStep(internal, reason, content)
}

// WorkflowBlockedResponse returns the tool calls and content of a blocked
// workflow step as TS puts them in the assistant message: invalid local calls
// and their tool-error parts are dropped.
func WorkflowBlockedResponse(calls []types.ToolCall, content []types.ContentPart) ([]types.ToolCall, []types.ContentPart) {
	outCalls := make([]types.ToolCall, 0, len(calls))
	for _, c := range calls {
		if c.Invalid && !c.ProviderExecuted {
			continue
		}
		outCalls = append(outCalls, c)
	}
	outContent := make([]types.ContentPart, 0, len(content))
	for _, part := range content {
		switch p := part.(type) {
		case types.ToolErrorContent:
			if !p.ProviderExecuted {
				continue
			}
		case *types.ToolErrorContent:
			if p != nil && !p.ProviderExecuted {
				continue
			}
		}
		outContent = append(outContent, part)
	}
	return outCalls, outContent
}
