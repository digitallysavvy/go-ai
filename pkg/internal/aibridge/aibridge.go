// Package aibridge exposes unexported pkg/ai helpers to other packages of this
// module (the legacy agent loop in pkg/agent) without adding them to the
// public pkg/ai API. pkg/ai sets each function in its init; any package that
// imports pkg/ai can call them.
package aibridge

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

var (
	// ProviderToolResultsFromContent returns the provider-executed results a
	// model response carries (its tool-result and tool-error parts) as
	// ToolResults, in content order, skipping calls executed already has a
	// result for (TS StepResult.toolResults lists them next to the locally
	// executed results).
	ProviderToolResultsFromContent func(content []types.ContentPart, executed []types.ToolResult) []types.ToolResult

	// MessagesForModel removes the approval parts TS
	// convertToLanguageModelPrompt keeps out of a model call's prompt (a
	// provider-executed call's approval response is kept).
	MessagesForModel func(messages []types.Message) []types.Message
)
