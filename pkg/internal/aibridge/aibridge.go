// Package aibridge exposes unexported pkg/ai helpers to other packages of this
// module (the legacy agent loop in pkg/agent and pkg/workflow) without adding
// them to the public pkg/ai API. pkg/ai sets each function in its init; any
// package that imports pkg/ai can call them.
package aibridge

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

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

	// ValidateToolResultParts runs the "Tool call <id> not found." check of TS
	// convertLanguageModelContent; tools is the step's tool set.
	ValidateToolResultParts func(content []types.ContentPart, calls []types.ToolCall, tools []types.Tool) error

	// ResolveContentFiles downloads every URL-backed file and reasoning-file
	// part of a model response (TS convertLanguageModelContent
	// resolveGeneratedFileData). The input slice is not modified.
	ResolveContentFiles func(ctx context.Context, content []types.ContentPart) ([]types.ContentPart, error)

	// CheckToolChoiceViolation returns a *ai.ToolChoiceViolationError when
	// choice requires a tool call that toolCalls lacks.
	CheckToolChoiceViolation func(choice types.ToolChoice, toolCalls []types.ToolCall, finishReason types.FinishReason, provider, modelID string, content []types.ContentPart) error

	// EmptyProviderResults answers the provider-executed calls of a step that
	// got no result in content with the empty text result TS WorkflowAgent
	// sends (InternalOptions.ContinueOnMissingProviderResult).
	EmptyProviderResults func(calls []types.ToolCall, tools []types.Tool, content []types.ContentPart, finishReason types.FinishReason, provider, model string) []types.ToolResult

	// WorkflowBlockedToolStep reports whether TS WorkflowAgent leaves a step's
	// tool calls untouched; continueOnMissingProviderResult is the call's
	// InternalOptions.ContinueOnMissingProviderResult.
	WorkflowBlockedToolStep func(continueOnMissingProviderResult bool, reason types.FinishReason, content []types.ContentPart) bool

	// WorkflowBlockedResponse returns the tool calls and content of a blocked
	// workflow step as TS puts them in the assistant message.
	WorkflowBlockedResponse func(calls []types.ToolCall, content []types.ContentPart) ([]types.ToolCall, []types.ContentPart)

	// InvokeToolInputCallbacks runs OnInputStart and then OnInputAvailable
	// for every valid call, in order.
	InvokeToolInputCallbacks func(ctx context.Context, calls []types.ToolCall, tools []types.Tool, messages []types.Message, toolsContext map[string]interface{}) error

	// MarkDynamicToolResults sets Dynamic on every result whose call was a
	// dynamic tool call.
	MarkDynamicToolResults func(results []types.ToolResult, calls []types.ToolCall)

	// InvalidToolCallInput is the input of an invalid tool call as TS reports
	// it in a tool-input-error chunk.
	InvalidToolCallInput func(call types.ToolCall) interface{}

	// ValidateToolContext is TS resolveToolContext for one tool.
	ValidateToolContext func(tool *types.Tool, toolName string, ctxValue interface{}) (interface{}, error)

	// ResolveToolApproval is TS resolveToolApproval for the legacy agent
	// loop. It fails closed: an unknown approval status or an unsupported
	// configuration value is an error instead of letting the tool run.
	ResolveToolApproval func(ctx context.Context, call types.ToolCall, tools []types.Tool, messages []types.Message, runtimeCtx interface{}, toolsCtx map[string]interface{}, toolApproval interface{}) (types.ToolApprovalResult, error)

	// WrapGatewayError is TS wrapGatewayError (an unauthenticated AI Gateway
	// error becomes one that says how to authenticate).
	WrapGatewayError func(err error) error
)
