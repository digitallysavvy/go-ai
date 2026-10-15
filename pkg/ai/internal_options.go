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
	answered := func(id string) bool {
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
			}
		}
		return false
	}
	var results []types.ToolResult
	for _, call := range missingProviderResultCalls(calls, byName, answered) {
		results = append(results, emptyProviderResult(call, provider, model))
	}
	return results
}
