package ai

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/aibridge"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Helpers the legacy agent loop (pkg/agent) and pkg/workflow share with
// GenerateText and StreamText. They are reached through pkg/internal/aibridge
// so they stay out of the public API.
func init() {
	aibridge.ProviderToolResultsFromContent = providerToolResultsFromContent
	aibridge.ProviderErrorResultsAsToolErrors = providerErrorResultsAsToolErrors
	aibridge.SuccessfulToolResults = successfulToolResults
	aibridge.MessagesForModel = messagesForModel
	aibridge.ValidateToolResultParts = validateToolResultPartsOf
	aibridge.ResolveContentFiles = resolveContentFiles
	aibridge.CheckToolChoiceViolation = checkToolChoiceViolation
	aibridge.EmptyProviderResults = emptyProviderResults
	aibridge.WorkflowBlockedToolStep = func(continueOnMissingProviderResult bool, reason types.FinishReason, content []types.ContentPart) bool {
		return workflowBlockedToolStep(&InternalOptions{ContinueOnMissingProviderResult: continueOnMissingProviderResult}, reason, content)
	}
	aibridge.WorkflowBlockedResponse = workflowBlockedResponse
	aibridge.InvokeToolInputCallbacks = invokeToolInputCallbacks
	aibridge.MarkDynamicToolResults = markDynamicToolResults
	aibridge.InvalidToolCallInput = invalidToolCallInput
	aibridge.ValidateToolContext = validateToolContext
	aibridge.ResolveToolApproval = resolveToolApproval
	aibridge.WrapGatewayError = wrapGatewayError
	aibridge.SetWorkflowStreamHooks = setWorkflowStreamHooks
	aibridge.StreamTextHasOutputSpec = func(result interface{}) bool {
		r, ok := result.(*StreamTextResult)
		return ok && r != nil && r.hasOutputSpec
	}
}
